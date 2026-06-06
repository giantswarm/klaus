package a2a

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"

	"github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/session"
	"github.com/giantswarm/klaus/pkg/telemetry"
)

// Mode selects the underlying Klaus execution strategy for A2A requests.
type Mode string

const (
	// ModeChat wires A2A to a PersistentProcess. The subprocess maintains the
	// session and conversation history across turns.
	ModeChat Mode = "chat"
	// ModeAgent wires A2A to single-shot Process invocations. Each turn
	// resumes the session by passing --resume <sessionID>.
	ModeAgent Mode = "agent"
)

// Executor implements a2asrv.AgentExecutor backed by a Klaus Prompter.
// It is safe for concurrent use from multiple goroutines; requests to the
// same contextID are serialized, requests from different contextIDs run
// in parallel.
type Executor struct {
	prompter claude.Prompter
	store    session.Store
	mode     Mode

	// mu protects contextMu.
	mu sync.Mutex
	// contextMu maps contextID to its per-context lock, preventing concurrent
	// executions within the same conversation. A second request while one is
	// in flight receives a rejected event.
	contextMu map[string]*sync.Mutex
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

// New returns an Executor. prompter may be either a *claude.Process (agent
// mode) or a *claude.PersistentProcess (chat mode). store persists
// contextID→sessionID bindings across turns.
func New(prompter claude.Prompter, store session.Store, mode Mode) *Executor {
	return &Executor{
		prompter:  prompter,
		store:     store,
		mode:      mode,
		contextMu: make(map[string]*sync.Mutex),
	}
}

// Execute implements a2asrv.AgentExecutor. It runs a Klaus prompt for the
// request and writes A2A events to queue:
//
//  1. working — immediately after lock acquired
//  2. working (streaming) — for each assistant text chunk
//  3. artifact with LastChunk=true — full result text when turn completes
//  4. completed (Final=true) — terminal state
//
// If the contextID is already in-flight, a rejected event is written instead.
func (e *Executor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	contextID := reqCtx.ContextID

	_, span := telemetry.Tracer("a2a").Start(ctx, telemetry.SpanA2ATaskReceived,
		trace.WithAttributes(
			attribute.String(telemetry.AttrContextID, contextID),
			attribute.Int(telemetry.AttrMessageLength, len(extractText(reqCtx.Message))),
		))
	span.End()

	// Acquire the per-context lock; reject if already in-flight.
	mu := e.lockForContext(contextID)
	if !mu.TryLock() {
		log.Printf("[a2a] contextID %q already in-flight, rejecting", contextID)
		return queue.Write(ctx, rejectedEvent(reqCtx, "another request for this context is already in-flight"))
	}
	defer func() {
		mu.Unlock()
		e.releaseContext(contextID)
	}()

	// Extract prompt text.
	text := extractText(reqCtx.Message)
	if text == "" {
		return queue.Write(ctx, failedEvent(reqCtx, "message contains no text content"))
	}

	// Intercept TUI-only slash commands.
	if cmd, ok := interceptSlashCommand(text); ok {
		reply := fmt.Sprintf(
			"`%s` is not available in this environment — Claude Code is running headless. "+
				"Model and configuration are controlled via environment variables (CLAUDE_MODEL, etc.).", cmd)
		if err := queue.Write(ctx, artifactEvent(reqCtx, reply, "")); err != nil {
			return fmt.Errorf("write intercept artifact: %w", err)
		}
		return queue.Write(ctx, completedEvent(reqCtx))
	}

	// Emit initial working state.
	if err := queue.Write(ctx, workingEvent(reqCtx, "thinking…")); err != nil {
		return fmt.Errorf("write working: %w", err)
	}

	// Look up or build RunOptions based on mode.
	runOpts, err := e.runOptions(ctx, contextID)
	if err != nil {
		log.Printf("[a2a] session lookup failed for contextID %q: %v", contextID, err)
		// Non-fatal: proceed without resume.
	}

	// Run the prompt.
	ch, err := e.prompter.RunWithOptions(ctx, text, runOpts)
	if err != nil {
		if errors.Is(err, claude.ErrBusy) {
			return queue.Write(ctx, rejectedEvent(reqCtx, "agent subprocess is busy"))
		}
		log.Printf("[a2a] RunWithOptions failed for contextID %q: %v", contextID, err)
		return queue.Write(ctx, failedEvent(reqCtx, "agent error: "+err.Error()))
	}

	// Drain the stream and emit working events for text chunks.
	var lastText string
	for msg := range ch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if msg.Type == claude.MessageTypeStreamEvent || msg.Type == claude.MessageTypeAssistant {
			if msg.Text != "" && msg.Text != lastText {
				lastText = msg.Text
				if writeErr := queue.Write(ctx, workingEvent(reqCtx, claude.Truncate(msg.Text, 200))); writeErr != nil {
					log.Printf("[a2a] write working chunk: %v", writeErr)
				}
			}
		}
	}

	// Collect result and bind session.
	status := e.prompter.Status()
	sessionID := status.SessionID
	result := status.Result
	if result == "" {
		detail := e.prompter.ResultDetail()
		result = detail.ResultText
	}

	if sessionID != "" {
		if bindErr := e.store.BindSession(ctx, contextID, sessionID); bindErr != nil {
			log.Printf("[a2a] BindSession failed for contextID %q: %v", contextID, bindErr)
		}
	}

	if status.Status == claude.ProcessStatusError {
		errMsg := status.ErrorMessage
		if errMsg == "" {
			errMsg = "agent subprocess exited with an error"
		}
		return queue.Write(ctx, failedEvent(reqCtx, errMsg))
	}

	// Write the full result as a final artifact.
	if result == "" && status.Status == claude.ProcessStatusBusy {
		// Mid-retry: the subprocess is recovering. Emit a status hint.
		retryMsg := fmt.Sprintf("reconnecting, attempt %d", status.RetryCount)
		if writeErr := queue.Write(ctx, workingEvent(reqCtx, retryMsg)); writeErr != nil {
			return fmt.Errorf("write retry working: %w", writeErr)
		}
	}

	if result != "" {
		if writeErr := queue.Write(ctx, artifactEvent(reqCtx, result, sessionID)); writeErr != nil {
			return fmt.Errorf("write artifact: %w", writeErr)
		}
	}

	return queue.Write(ctx, completedEvent(reqCtx))
}

// Cancel implements a2asrv.AgentExecutor. It stops the Klaus subprocess and
// writes a canceled terminal event.
func (e *Executor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	if err := e.prompter.Stop(); err != nil {
		log.Printf("[a2a] Cancel: Stop() error for contextID %q: %v", reqCtx.ContextID, err)
	}

	// Give the subprocess up to 30 s to exit gracefully.
	done := e.prompter.Done()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		log.Printf("[a2a] Cancel: subprocess did not stop within 30s for contextID %q", reqCtx.ContextID)
	case <-ctx.Done():
	}

	return queue.Write(ctx, canceledEvent(reqCtx))
}

// runOptions builds per-invocation RunOptions for the given contextID.
// In agent mode it threads the persisted sessionID; in chat mode session
// continuity is owned by the PersistentProcess itself.
func (e *Executor) runOptions(ctx context.Context, contextID string) (*claude.RunOptions, error) {
	if e.mode != ModeAgent {
		return nil, nil
	}

	sessionID, err := e.store.SessionID(ctx, contextID)
	if err != nil {
		return nil, err
	}

	return &claude.RunOptions{
		SessionID: sessionID,
		Resume:    sessionID,
	}, nil
}

// lockForContext returns (and lazily creates) the per-contextID mutex. The
// returned mutex is not held; callers must call TryLock/Lock themselves.
func (e *Executor) lockForContext(contextID string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	mu, ok := e.contextMu[contextID]
	if !ok {
		mu = &sync.Mutex{}
		e.contextMu[contextID] = mu
	}
	return mu
}

// releaseContext removes the per-context mutex once a context is no longer
// in-flight. This prevents the map from growing unboundedly.
func (e *Executor) releaseContext(contextID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.contextMu, contextID)
}

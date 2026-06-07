package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"

	"github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/kagentapi"
	"github.com/giantswarm/klaus/pkg/memory"
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
	kagent   *kagentapi.Client
	mem      memory.Client
	mode     Mode

	// mu protects inFlight.
	mu sync.Mutex
	// inFlight tracks contextIDs that currently have an Execute in progress.
	// A second request for the same contextID while one is in flight receives
	// a rejected event. Using a presence-set avoids the unlock-then-delete
	// race that a per-context mutex map would have.
	inFlight map[string]struct{}
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

// New returns an Executor. prompter may be either a *claude.Process (agent
// mode) or a *claude.PersistentProcess (chat mode). store persists
// contextID→sessionID bindings across turns. kagentClient may be nil; when
// non-nil and enabled, completed turns are pushed to the kagent UI.
// memClient handles semantic memory retrieval and storage; pass memory.NoOp{}
// to disable memory augmentation.
func New(prompter claude.Prompter, store session.Store, mode Mode, kagentClient *kagentapi.Client, memClient memory.Client) *Executor {
	if memClient == nil {
		memClient = memory.NoOp{}
	}
	return &Executor{
		prompter: prompter,
		store:    store,
		kagent:   kagentClient,
		mem:      memClient,
		mode:     mode,
		inFlight: make(map[string]struct{}),
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

	// Acquire the per-context in-flight slot; reject if already running.
	if !e.lockForContext(contextID) {
		log.Printf("[a2a] contextID %q already in-flight, rejecting", contextID)
		return queue.Write(ctx, rejectedEvent(reqCtx, "another request for this context is already in-flight"))
	}
	defer e.releaseContext(contextID)

	// Extract prompt text.
	text := extractText(reqCtx.Message)
	if text == "" {
		return queue.Write(ctx, failedEvent(reqCtx, "message contains no text content"))
	}

	log.Printf("[a2a] turn start contextID=%q prompt=%q", contextID, claude.Truncate(text, 120))

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

	// Retrieve semantic memory and prepend relevant past context to the prompt.
	augmented := e.augmentWithMemory(ctx, contextID, text)

	// Run the prompt.
	ch, err := e.prompter.RunWithOptions(ctx, augmented, runOpts)
	if err != nil {
		if errors.Is(err, claude.ErrBusy) {
			return queue.Write(ctx, rejectedEvent(reqCtx, "agent subprocess is busy"))
		}
		log.Printf("[a2a] RunWithOptions failed for contextID %q: %v", contextID, err)
		return queue.Write(ctx, failedEvent(reqCtx, "agent error: "+err.Error()))
	}

	// Drain the stream, emit working events for text chunks, and collect all
	// messages for result extraction and session ID discovery.
	var lastText string
	var messages []claude.StreamMessage
	var streamSessionID string
drainLoop:
	for {
		select {
		case <-ctx.Done():
			// Stop the subprocess so it doesn't block indefinitely after the
			// caller cancels; without this the stdout goroutine fills the 100-
			// message buffer and cmd.Wait never returns, leaving status Busy.
			_ = e.prompter.Stop()
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				break drainLoop
			}
			messages = append(messages, msg)
			if msg.Type == claude.MessageTypeSystem && msg.SessionID != "" {
				streamSessionID = msg.SessionID
				log.Printf("[a2a] session contextID=%q sessionID=%q", contextID, msg.SessionID)
			}
			if msg.Type == claude.MessageTypeAssistant && msg.Subtype == claude.SubtypeToolUse {
				log.Printf("[a2a] tool_use contextID=%q tool=%q", contextID, msg.ToolName)
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
	}

	// Log the raw result for console visibility (kubectl logs).
	log.Printf("[a2a] turn done contextID=%q messages=%d", contextID, len(messages))

	// Derive the result from the stream only. Status().Result is NOT consulted
	// because RunWithOptions leaves the process Idle; the disk fallback in
	// Status() would serve a stale result from a previous MCP Submit.
	result := claude.CollectResultText(messages)

	if result != "" {
		log.Printf("[a2a] result contextID=%q result=%q", contextID, claude.Truncate(result, 500))
	}

	// Use the sessionID observed in this run's stream. Status().SessionID is
	// NOT consulted because p.sessionID is reset at run start but may still
	// carry a prior value if no system message arrives (e.g. early crash).
	sessionID := streamSessionID

	if sessionID != "" {
		if bindErr := e.store.BindSession(ctx, contextID, sessionID); bindErr != nil {
			log.Printf("[a2a] BindSession failed for contextID %q: %v", contextID, bindErr)
		}
	}

	// Record user prompt and assistant reply as conversation turns. Both
	// writes are async and best-effort so they never block the response path.
	e.recordTurns(contextID, sessionID, text, result)

	// Query status only for error / retry-state detection; Result and SessionID
	// fields are intentionally not used (see result/sessionID derivation above).
	status := e.prompter.Status()

	if status.Status == claude.ProcessStatusError {
		errMsg := status.ErrorMessage
		if errMsg == "" {
			errMsg = "agent subprocess exited with an error"
		}
		return queue.Write(ctx, failedEvent(reqCtx, errMsg))
	}

	// If the subprocess is still Busy after the stream closed and we have no
	// result, it is mid-retry (PersistentProcess recovering from a crash).
	// Fail the turn rather than emitting a terminal completed with no artifact.
	if result == "" && status.Status == claude.ProcessStatusBusy {
		retryMsg := fmt.Sprintf("subprocess is recovering (attempt %d); no result produced", status.RetryCount)
		return queue.Write(ctx, failedEvent(reqCtx, retryMsg))
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
		Resume:      sessionID,
		SaveSession: true,
	}, nil
}

// augmentWithMemory retrieves relevant memory chunks and prepends them to
// the prompt text. Returns text unchanged when memory is unavailable or empty.
func (e *Executor) augmentWithMemory(ctx context.Context, contextID, text string) string {
	chunks, err := e.mem.Retrieve(ctx, contextID, text, 5)
	if err != nil {
		log.Printf("[a2a] memory retrieve failed contextID=%q: %v", contextID, err)
		return text
	}
	if len(chunks) == 0 {
		return text
	}
	var sb strings.Builder
	sb.WriteString("[Relevant memory from previous sessions]\n")
	for _, c := range chunks {
		sb.WriteString("- ")
		sb.WriteString(c.Content)
		sb.WriteByte('\n')
	}
	sb.WriteString("\n")
	sb.WriteString(text)
	return sb.String()
}

// recordTurns persists the user prompt and assistant reply as conversation
// turns in the store, then pushes them to the kagent UI. Both operations are
// async and best-effort: failures are logged only, never returned.
func (e *Executor) recordTurns(contextID, sessionID, userText, assistantText string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if userText != "" {
			userContent, _ := json.Marshal(userText)
			if err := e.store.AppendTurn(ctx, session.Turn{
				ContextID: contextID,
				SessionID: sessionID,
				Role:      "user",
				Content:   userContent,
				TS:        time.Now(),
			}); err != nil {
				log.Printf("[a2a] AppendTurn user failed contextID=%q: %v", contextID, err)
			}
			if e.kagent != nil && sessionID != "" {
				e.kagent.PushEvent(ctx, sessionID, kagentapi.SessionEvent{Role: "user", Content: userText})
			}
			if err := e.mem.Store(ctx, contextID, "user", userText); err != nil {
				log.Printf("[a2a] memory store user failed contextID=%q: %v", contextID, err)
			}
		}

		if assistantText != "" {
			assistantContent, _ := json.Marshal(assistantText)
			if err := e.store.AppendTurn(ctx, session.Turn{
				ContextID: contextID,
				SessionID: sessionID,
				Role:      "assistant",
				Content:   assistantContent,
				TS:        time.Now(),
			}); err != nil {
				log.Printf("[a2a] AppendTurn assistant failed contextID=%q: %v", contextID, err)
			}
			if e.kagent != nil && sessionID != "" {
				e.kagent.PushEvent(ctx, sessionID, kagentapi.SessionEvent{Role: "assistant", Content: assistantText})
			}
			if err := e.mem.Store(ctx, contextID, "assistant", assistantText); err != nil {
				log.Printf("[a2a] memory store assistant failed contextID=%q: %v", contextID, err)
			}
		}
	}()
}

// lockForContext marks contextID as in-flight. Returns true if the slot was
// acquired (caller may proceed), false if contextID was already in-flight
// (caller should reject). The in-flight flag is set atomically under e.mu,
// eliminating the unlock-then-delete race that a per-context mutex map has.
func (e *Executor) lockForContext(contextID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.inFlight[contextID]; ok {
		return false
	}
	e.inFlight[contextID] = struct{}{}
	return true
}

// releaseContext clears the in-flight marker for contextID. This prevents
// the map from growing unboundedly and unblocks the next request.
func (e *Executor) releaseContext(contextID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.inFlight, contextID)
}

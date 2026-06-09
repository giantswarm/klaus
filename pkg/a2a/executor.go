package a2a

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/telemetry"
)

// Mode selects the underlying Klaus execution strategy for A2A requests.
type Mode string

const (
	// ModeChat wires A2A to a PersistentProcess; the subprocess owns session continuity.
	ModeChat Mode = "chat"
	// ModeAgent wires A2A to single-shot Process invocations, resuming via --resume.
	ModeAgent Mode = "agent"
)

// Executor implements a2asrv.AgentExecutor backed by a Klaus Prompter.
// Requests to the same contextID are serialized; different contextIDs run in
// parallel.
type Executor struct {
	prompter claude.Prompter
	mode     Mode

	mu       sync.Mutex
	inFlight map[string]struct{}
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

// New returns an Executor. prompter may be a *claude.Process (agent mode) or a
// *claude.PersistentProcess (chat mode).
func New(prompter claude.Prompter, mode Mode) *Executor {
	return &Executor{
		prompter: prompter,
		mode:     mode,
		inFlight: make(map[string]struct{}),
	}
}

// Execute implements a2asrv.AgentExecutor.
func (e *Executor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		contextID := execCtx.ContextID

		_, span := telemetry.Tracer("a2a").Start(ctx, telemetry.SpanA2ATaskReceived,
			trace.WithAttributes(
				attribute.String(telemetry.AttrContextID, contextID),
				attribute.Int(telemetry.AttrMessageLength, len(extractText(execCtx.Message))),
			))
		span.End()

		if execCtx.StoredTask == nil {
			if !yield(a2asdk.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}

		if !e.lockForContext(contextID) {
			log.Printf("[a2a] contextID %q already in-flight, rejecting", contextID)
			yield(rejectedEvent(execCtx, "another request for this context is already in-flight"), nil)
			return
		}
		var releaseOnce sync.Once
		releaseCtx := func() { releaseOnce.Do(func() { e.releaseContext(contextID) }) }
		defer releaseCtx()

		text := extractText(execCtx.Message)
		if text == "" {
			yield(failedEvent(execCtx, "message contains no text content"), nil)
			return
		}

		log.Printf("[a2a] turn start contextID=%q prompt=%q", contextID, claude.Truncate(text, 120))

		if cmd, ok := interceptSlashCommand(text); ok {
			reply := fmt.Sprintf(
				"`%s` is not available in this environment — Claude Code is running headless. "+
					"Model and configuration are controlled via environment variables (CLAUDE_MODEL, etc.).", cmd)
			if !yield(artifactEvent(execCtx, reply, ""), nil) {
				return
			}
			yield(completedEvent(execCtx), nil)
			return
		}

		if !yield(workingEvent(execCtx, "thinking..."), nil) {
			return
		}

		runOpts, err := e.runOptions(ctx, contextID)
		if err != nil {
			log.Printf("[a2a] session lookup failed for contextID %q: %v", contextID, err)
		}

		ch, err := e.prompter.RunWithOptions(ctx, text, runOpts)
		if err != nil {
			if errors.Is(err, claude.ErrBusy) {
				yield(rejectedEvent(execCtx, "agent subprocess is busy"), nil)
				return
			}
			log.Printf("[a2a] RunWithOptions failed for contextID %q: %v", contextID, err)
			yield(failedEvent(execCtx, "agent error: "+err.Error()), nil)
			return
		}

		var lastText string
		var messages []claude.StreamMessage
		var streamSessionID string
	drainLoop:
		for {
			select {
			case <-ctx.Done():
				releaseCtx()
				_ = e.prompter.Stop()
				return
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
					if !yield(workingEvent(execCtx, "using tool: "+msg.ToolName), nil) {
						releaseCtx()
						_ = e.prompter.Stop()
						return
					}
				}
				if msg.Type == claude.MessageTypeStreamEvent || msg.Type == claude.MessageTypeAssistant {
					if msg.Text != "" && msg.Text != lastText {
						lastText = msg.Text
						if !yield(workingEvent(execCtx, claude.Truncate(msg.Text, 200)), nil) {
							releaseCtx()
							_ = e.prompter.Stop()
							return
						}
					}
				}
			}
		}

		// Stale resume: subprocess exited with no session ID — session file was
		// lost (e.g. pod restart with emptyDir workspace). Retry once with a fresh
		// session seeded from contextID. Skip if ctx is already cancelled (the
		// channel-close and ctx.Done cases can race in the select above).
		if ctx.Err() == nil && runOpts != nil && runOpts.Resume != "" && streamSessionID == "" &&
			e.prompter.Status().Status == claude.ProcessStatusError {
			log.Printf("[a2a] stale session resume contextID=%q, retrying fresh", contextID)
			retryOpts := &claude.RunOptions{
				SaveSession: true,
				SessionID:   sessionIDFromContext(contextID),
			}
			if retryCh, retryErr := e.prompter.RunWithOptions(ctx, text, retryOpts); retryErr == nil {
				messages = messages[:0]
				lastText = ""
			retryDrainLoop:
				for {
					select {
					case <-ctx.Done():
						releaseCtx()
						_ = e.prompter.Stop()
						return
					case msg, ok := <-retryCh:
						if !ok {
							break retryDrainLoop
						}
						messages = append(messages, msg)
						if msg.Type == claude.MessageTypeSystem && msg.SessionID != "" {
							streamSessionID = msg.SessionID
							log.Printf("[a2a] retry session contextID=%q sessionID=%q", contextID, msg.SessionID)
						}
						if msg.Type == claude.MessageTypeAssistant && msg.Subtype == claude.SubtypeToolUse {
							log.Printf("[a2a] tool_use (retry) contextID=%q tool=%q", contextID, msg.ToolName)
							if !yield(workingEvent(execCtx, "using tool: "+msg.ToolName), nil) {
								releaseCtx()
								_ = e.prompter.Stop()
								return
							}
						}
						if msg.Type == claude.MessageTypeStreamEvent || msg.Type == claude.MessageTypeAssistant {
							if msg.Text != "" && msg.Text != lastText {
								lastText = msg.Text
								if !yield(workingEvent(execCtx, claude.Truncate(msg.Text, 200)), nil) {
									releaseCtx()
									_ = e.prompter.Stop()
									return
								}
							}
						}
					}
				}
			} else {
				log.Printf("[a2a] retry RunWithOptions failed contextID=%q: %v", contextID, retryErr)
			}
		}

		log.Printf("[a2a] turn done contextID=%q messages=%d", contextID, len(messages))

		result := claude.CollectResultText(messages)
		if result != "" {
			log.Printf("[a2a] result contextID=%q result=%q", contextID, claude.Truncate(result, 500))
		}

		status := e.prompter.Status()
		if status.Status == claude.ProcessStatusError {
			errMsg := status.ErrorMessage
			if errMsg == "" {
				errMsg = "agent subprocess exited with an error"
			}
			yield(failedEvent(execCtx, errMsg), nil)
			return
		}

		if result == "" && status.Status == claude.ProcessStatusBusy {
			retryMsg := fmt.Sprintf("subprocess is recovering (attempt %d); no result produced", status.RetryCount)
			yield(failedEvent(execCtx, retryMsg), nil)
			return
		}

		if result != "" {
			if !yield(artifactEvent(execCtx, result, streamSessionID), nil) {
				return
			}
		}

		yield(completedEvent(execCtx), nil)
	}
}

// Cancel implements a2asrv.AgentExecutor.
func (e *Executor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		if err := e.prompter.Stop(); err != nil {
			log.Printf("[a2a] Cancel: Stop() error for contextID %q: %v", execCtx.ContextID, err)
		}

		done := e.prompter.Done()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			log.Printf("[a2a] Cancel: subprocess did not stop within 30s for contextID %q", execCtx.ContextID)
		case <-ctx.Done():
		}

		yield(canceledEvent(execCtx), nil)
	}
}

// runOptions builds RunOptions for agent mode (resume by contextID-derived session ID).
// Returns nil in chat mode — the PersistentProcess owns session continuity.
func (e *Executor) runOptions(_ context.Context, contextID string) (*claude.RunOptions, error) {
	if e.mode != ModeAgent {
		return nil, nil
	}
	return &claude.RunOptions{
		SaveSession: true,
		Resume:      sessionIDFromContext(contextID),
	}, nil
}

// sessionIDFromContext derives a Claude session ID from an A2A contextID.
// "ctx-<uuid>" -> "<uuid>"; plain UUIDs are returned as-is.
func sessionIDFromContext(contextID string) string {
	if after, ok := strings.CutPrefix(contextID, "ctx-"); ok {
		return after
	}
	return contextID
}

func (e *Executor) lockForContext(contextID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.inFlight[contextID]; ok {
		return false
	}
	e.inFlight[contextID] = struct{}{}
	return true
}

func (e *Executor) releaseContext(contextID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.inFlight, contextID)
}

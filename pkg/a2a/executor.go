package a2a

import (
	"context"
	"encoding/json"
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
	"github.com/google/uuid"

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
	// a rejected event.
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
// request and yields A2A events in sequence:
//
//  1. working — immediately after lock acquired
//  2. working (streaming) — for each assistant text chunk
//  3. artifact with LastChunk=true — full result text when turn completes
//  4. completed — terminal state
//
// If the contextID is already in-flight, a rejected event is yielded instead.
func (e *Executor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		contextID := execCtx.ContextID

		_, span := telemetry.Tracer("a2a").Start(ctx, telemetry.SpanA2ATaskReceived,
			trace.WithAttributes(
				attribute.String(telemetry.AttrContextID, contextID),
				attribute.Int(telemetry.AttrMessageLength, len(extractText(execCtx.Message))),
			))
		span.End()

		// Acquire the per-context in-flight slot; reject if already running.
		if !e.lockForContext(contextID) {
			log.Printf("[a2a] contextID %q already in-flight, rejecting", contextID)
			yield(rejectedEvent(execCtx, "another request for this context is already in-flight"), nil)
			return
		}
		var releaseOnce sync.Once
		releaseCtx := func() { releaseOnce.Do(func() { e.releaseContext(contextID) }) }
		defer releaseCtx()

		// Inject the authenticated caller's subject into ctx so memory scopes to
		// the user rather than the static KAGENT_MEMORY_USER_ID fallback.
		if authInfo := kagentapi.AuthInfoFromContext(ctx); authInfo.UserSub != "" {
			ctx = memory.WithUserID(ctx, authInfo.UserSub)
		}

		// Extract prompt text.
		text := extractText(execCtx.Message)
		if text == "" {
			yield(failedEvent(execCtx, "message contains no text content"), nil)
			return
		}

		log.Printf("[a2a] turn start contextID=%q prompt=%q", contextID, claude.Truncate(text, 120))

		// Intercept TUI-only slash commands.
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

		// Emit initial working state.
		if !yield(workingEvent(execCtx, "thinking…"), nil) {
			return
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
				yield(rejectedEvent(execCtx, "agent subprocess is busy"), nil)
				return
			}
			log.Printf("[a2a] RunWithOptions failed for contextID %q: %v", contextID, err)
			yield(failedEvent(execCtx, "agent error: "+err.Error()), nil)
			return
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
				// Release before Stop so a concurrent retry can acquire the slot
				// without waiting for the subprocess to drain (up to 30 s).
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

		// Stale resume: subprocess exited with no session ID — the session file was
		// lost (e.g. pod restart with emptyDir workspace). Retry once with a fresh
		// session using the deterministic context-derived ID so the turn succeeds.
		if runOpts != nil && runOpts.Resume != "" && streamSessionID == "" &&
			e.prompter.Status().Status == claude.ProcessStatusError {
			log.Printf("[a2a] stale session resume contextID=%q, retrying fresh", contextID)
			retryOpts := &claude.RunOptions{
				SaveSession: true,
				SessionID:   sessionIDFromContext(contextID),
			}
			if retryCh, retryErr := e.prompter.RunWithOptions(ctx, augmented, retryOpts); retryErr == nil {
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

		// Record user prompt and assistant reply as conversation turns. Both
		// writes are async and best-effort so they never block the response path.
		e.recordTurns(ctx, contextID, sessionID, text, result)

		// Query status only for error / retry-state detection.
		status := e.prompter.Status()

		if status.Status == claude.ProcessStatusError {
			errMsg := status.ErrorMessage
			if errMsg == "" {
				errMsg = "agent subprocess exited with an error"
			}
			yield(failedEvent(execCtx, errMsg), nil)
			return
		}

		// If the subprocess is still Busy after the stream closed and we have no
		// result, it is mid-retry (PersistentProcess recovering from a crash).
		if result == "" && status.Status == claude.ProcessStatusBusy {
			retryMsg := fmt.Sprintf("subprocess is recovering (attempt %d); no result produced", status.RetryCount)
			yield(failedEvent(execCtx, retryMsg), nil)
			return
		}

		if result != "" {
			if !yield(artifactEvent(execCtx, result, sessionID), nil) {
				return
			}
		}

		yield(completedEvent(execCtx), nil)
	}
}

// Cancel implements a2asrv.AgentExecutor. It stops the Klaus subprocess and
// yields a canceled terminal event.
func (e *Executor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		if err := e.prompter.Stop(); err != nil {
			log.Printf("[a2a] Cancel: Stop() error for contextID %q: %v", execCtx.ContextID, err)
		}

		// Give the subprocess up to 30 s to exit gracefully.
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

// runOptions builds per-invocation RunOptions for the given contextID.
// In agent mode it always resumes by the ID derived from contextID; in chat
// mode session continuity is owned by the PersistentProcess itself.
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
// A2A context IDs are typically "ctx-<uuid>"; the UUID part is extracted so
// the Claude session file is named by the same UUID the kagent UI displays.
// If contextID is already a plain UUID (no prefix), it is returned as-is.
func sessionIDFromContext(contextID string) string {
	if after, ok := strings.CutPrefix(contextID, "ctx-"); ok {
		return after
	}
	return contextID
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
//
// parentCtx carries the caller's OIDC AuthInfo (set by extractCallerAuth
// middleware). It is captured before the goroutine so the goroutine can
// re-inject it into the detached timeout context for kagent auth.
func (e *Executor) recordTurns(parentCtx context.Context, contextID, sessionID, userText, assistantText string) {
	authInfo := kagentapi.AuthInfoFromContext(parentCtx)
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), 10*time.Second)
		defer cancel()
		if authInfo.BearerToken != "" {
			ctx = kagentapi.WithAuthInfo(ctx, authInfo)
		}

		if userText != "" {
			userContent, _ := json.Marshal(userText)
			if sessionID != "" {
				if err := e.store.AppendTurn(ctx, session.Turn{
					ContextID: contextID,
					SessionID: sessionID,
					Role:      "user",
					Content:   userContent,
					TS:        time.Now(),
				}); err != nil {
					log.Printf("[a2a] AppendTurn user failed contextID=%q: %v", contextID, err)
				}
			}
			if e.kagent != nil {
				e.kagent.PushEvent(ctx, contextID, kagentapi.NewSessionEvent(newEventID(), "user", userText))
			}
			if err := e.mem.Store(ctx, contextID, "user", userText); err != nil {
				log.Printf("[a2a] memory store user failed contextID=%q: %v", contextID, err)
			}
		}

		if assistantText != "" {
			assistantContent, _ := json.Marshal(assistantText)
			if sessionID != "" {
				if err := e.store.AppendTurn(ctx, session.Turn{
					ContextID: contextID,
					SessionID: sessionID,
					Role:      "assistant",
					Content:   assistantContent,
					TS:        time.Now(),
				}); err != nil {
					log.Printf("[a2a] AppendTurn assistant failed contextID=%q: %v", contextID, err)
				}
			}
			if e.kagent != nil {
				e.kagent.PushEvent(ctx, contextID, kagentapi.NewSessionEvent(newEventID(), "agent", assistantText))
			}
			if err := e.mem.Store(ctx, contextID, "assistant", assistantText); err != nil {
				log.Printf("[a2a] memory store assistant failed contextID=%q: %v", contextID, err)
			}
		}

		// Store the turn as a completed A2A task so the kagent UI can render
		// conversation history on reload.
		if e.kagent != nil && userText != "" && assistantText != "" {
			e.kagent.StoreTask(ctx, newEventID(), contextID, userText, assistantText)
		}
	}()
}

// lockForContext marks contextID as in-flight. Returns true if the slot was
// acquired (caller may proceed), false if contextID was already in-flight.
func (e *Executor) lockForContext(contextID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.inFlight[contextID]; ok {
		return false
	}
	e.inFlight[contextID] = struct{}{}
	return true
}

// releaseContext clears the in-flight marker for contextID.
func (e *Executor) releaseContext(contextID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.inFlight, contextID)
}

func newEventID() string { return uuid.New().String() }

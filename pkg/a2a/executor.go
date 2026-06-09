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
	"github.com/giantswarm/klaus/pkg/kagentapi"
	"github.com/giantswarm/klaus/pkg/memory"
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
	kagent   *kagentapi.Client // nil-safe; no-op when endpoint is unset
	mem      memory.Store      // nil-safe; NoOp when memory is disabled
	memTopK  int

	mu       sync.Mutex
	inFlight map[string]struct{}
	sessions map[string]string // contextID → session ID established in prior turn
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

// New returns an Executor. prompter may be a *claude.Process (agent mode) or a
// *claude.PersistentProcess (chat mode). kagentClient may be nil (no-op).
// memStore may be nil (treated as NoOp). memTopK is the max chunks to retrieve
// per turn; 0 uses the default of 5.
func New(prompter claude.Prompter, mode Mode, kagentClient *kagentapi.Client, memStore memory.Store, memTopK int) *Executor {
	if memStore == nil {
		memStore = memory.NoOp{}
	}
	if memTopK <= 0 {
		memTopK = 5
	}
	return &Executor{
		prompter: prompter,
		mode:     mode,
		kagent:   kagentClient,
		mem:      memStore,
		memTopK:  memTopK,
		inFlight: make(map[string]struct{}),
		sessions: make(map[string]string),
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
			if !yield(artifactEvent(execCtx, reply, "", nil), nil) {
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

		userID := authInfoFromA2AContext(ctx).UserSub
		if userID == "" {
			log.Printf("[a2a] no user identity for contextID %q — memory disabled for this turn", contextID)
		} else if e.mode == ModeAgent {
			if chunks, memErr := e.mem.Retrieve(ctx, userID, text, e.memTopK); memErr != nil {
				log.Printf("[a2a] memory retrieve failed for contextID %q: %v", contextID, memErr)
			} else if len(chunks) > 0 {
				if runOpts == nil {
					runOpts = &claude.RunOptions{}
				}
				runOpts.AppendSystemPrompt = formatMemoryChunks(chunks)
			}
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

		messages, streamSessionID, cancelled := e.drainStream(ctx, execCtx, ch, releaseCtx, yield, "")
		if cancelled {
			return
		}

		// Stale resume: subprocess exited with no session ID — session file was lost
		// (e.g. pod restart with emptyDir workspace). Retry once with a fresh session
		// seeded from contextID. Skip if ctx is already cancelled (the channel-close
		// and ctx.Done cases can race in the drain loop).
		if ctx.Err() == nil && runOpts != nil && runOpts.Resume != "" && streamSessionID == "" &&
			e.prompter.Status().Status == claude.ProcessStatusError {
			log.Printf("[a2a] stale session resume contextID=%q, retrying fresh", contextID)
			retryOpts := &claude.RunOptions{
				SaveSession: true,
				SessionID:   sessionIDFromContext(contextID),
			}
			if retryCh, retryErr := e.prompter.RunWithOptions(ctx, text, retryOpts); retryErr == nil {
				var retryMessages []claude.StreamMessage
				retryMessages, streamSessionID, cancelled = e.drainStream(ctx, execCtx, retryCh, releaseCtx, yield, "(retry) ")
				if cancelled {
					return
				}
				messages = retryMessages
			} else {
				log.Printf("[a2a] retry RunWithOptions failed contextID=%q: %v", contextID, retryErr)
			}
		}

		// Record the session so the next turn can resume without a redundant
		// failed subprocess spawn.
		if streamSessionID != "" {
			e.noteSession(contextID, streamSessionID)
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
			usage := claude.CollectTokenUsage(messages)
			if !yield(artifactEvent(execCtx, result, streamSessionID, usage), nil) {
				return
			}
		}

		e.pushTurnAsync(ctx, execCtx, extractText(execCtx.Message), result, messages)

		yield(completedEvent(execCtx), nil)
	}
}

// drainStream reads from ch until it closes or ctx is cancelled. It yields A2A
// working events for tool-use and streaming text. Returns (messages, sessionID,
// cancelled). cancelled=true means ctx was cancelled mid-drain; the caller should
// return immediately without producing a result event.
//
// In chat mode the shared PersistentProcess is never stopped on context
// cancellation — only the drain is abandoned. In agent mode the single-shot
// subprocess is stopped so it doesn't orphan.
func (e *Executor) drainStream(
	ctx context.Context,
	execCtx *a2asrv.ExecutorContext,
	ch <-chan claude.StreamMessage,
	releaseCtx func(),
	yield func(a2asdk.Event, error) bool,
	logTag string,
) (messages []claude.StreamMessage, sessionID string, cancelled bool) {
	contextID := execCtx.ContextID
	var lastText string

	for {
		select {
		case <-ctx.Done():
			releaseCtx()
			if e.mode == ModeAgent {
				_ = e.prompter.Stop()
			}
			return messages, sessionID, true
		case msg, ok := <-ch:
			if !ok {
				return messages, sessionID, false
			}
			messages = append(messages, msg)
			if msg.Type == claude.MessageTypeSystem && msg.SessionID != "" {
				sessionID = msg.SessionID
				log.Printf("[a2a] %ssession contextID=%q sessionID=%q", logTag, contextID, msg.SessionID)
			}
			if msg.Type == claude.MessageTypeAssistant && msg.Subtype == claude.SubtypeToolUse {
				log.Printf("[a2a] %stool_use contextID=%q tool=%q", logTag, contextID, msg.ToolName)
				if !yield(workingEvent(execCtx, "using tool: "+msg.ToolName), nil) {
					releaseCtx()
					if e.mode == ModeAgent {
						_ = e.prompter.Stop()
					}
					return messages, sessionID, true
				}
			}
			if msg.Type == claude.MessageTypeStreamEvent || msg.Type == claude.MessageTypeAssistant {
				if msg.Text != "" && msg.Text != lastText {
					lastText = msg.Text
					if !yield(workingEvent(execCtx, claude.Truncate(msg.Text, 200)), nil) {
						releaseCtx()
						if e.mode == ModeAgent {
							_ = e.prompter.Stop()
						}
						return messages, sessionID, true
					}
				}
			}
		}
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

// runOptions builds RunOptions for agent mode. Uses --resume when a session has
// been established for this contextID; otherwise uses --session-id with the same
// derived value so the first turn creates the session deterministically, avoiding
// a wasted subprocess spawn from a failed --resume.
// Returns nil in chat mode — the PersistentProcess owns session continuity.
func (e *Executor) runOptions(_ context.Context, contextID string) (*claude.RunOptions, error) {
	if e.mode != ModeAgent {
		return nil, nil
	}
	derivedID := sessionIDFromContext(contextID)
	e.mu.Lock()
	knownSession := e.sessions[contextID]
	e.mu.Unlock()
	if knownSession != "" {
		return &claude.RunOptions{
			SaveSession: true,
			Resume:      knownSession,
		}, nil
	}
	return &claude.RunOptions{
		SaveSession: true,
		SessionID:   derivedID,
	}, nil
}

// noteSession records that a session was established for contextID so subsequent
// turns can resume it directly.
func (e *Executor) noteSession(contextID, sessionID string) {
	e.mu.Lock()
	e.sessions[contextID] = sessionID
	e.mu.Unlock()
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

// pushTurnAsync fires best-effort kagent history + usage push and memory
// recording in a goroutine so the A2A response is not delayed.
// userText and agentText may be empty (no-op for those operations).
func (e *Executor) pushTurnAsync(ctx context.Context, execCtx *a2asrv.ExecutorContext, userText, agentText string, messages []claude.StreamMessage) {
	kagentEnabled := e.kagent != nil && e.kagent.Enabled()
	if !kagentEnabled && userText == "" && agentText == "" {
		return
	}

	contextID := execCtx.ContextID
	taskID := string(execCtx.TaskID)
	auth := authInfoFromA2AContext(ctx)
	agentMeta := adkUsageMetadata(claude.CollectTokenUsage(messages))

	go func() {
		pushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()

		if kagentEnabled && (userText != "" || agentText != "") {
			e.kagent.PushEvent(pushCtx, contextID, kagentapi.NewSessionEvent(taskID+"-user", "user", userText), auth)
			e.kagent.PushEvent(pushCtx, contextID, kagentapi.NewSessionEventWithMetadata(taskID+"-agent", "agent", agentText, agentMeta), auth)
			e.kagent.StoreTask(pushCtx, taskID, contextID, userText, agentText, "completed", agentMeta, auth)
		}

		if userID := auth.UserSub; userID != "" {
			if userText != "" {
				if err := e.mem.Record(pushCtx, userID, "user", userText); err != nil {
					log.Printf("[a2a] memory record user failed contextID=%q: %v", contextID, err)
				}
			}
			if agentText != "" {
				if err := e.mem.Record(pushCtx, userID, "assistant", agentText); err != nil {
					log.Printf("[a2a] memory record assistant failed contextID=%q: %v", contextID, err)
				}
			}
		}
	}()
}

// authInfoFromA2AContext extracts caller identity from the a2asrv CallContext
// stored in ctx by the JSON-RPC handler. Falls back to SA-token-only auth when
// absent (e.g. in tests or non-HTTP paths).
func authInfoFromA2AContext(ctx context.Context) kagentapi.AuthInfo {
	callCtx, ok := a2asrv.CallContextFrom(ctx)
	if !ok {
		return kagentapi.AuthInfo{}
	}
	params := callCtx.ServiceParams()
	var bearer, userSub string
	if vals, ok := params.Get("authorization"); ok && len(vals) > 0 {
		bearer = strings.TrimPrefix(vals[0], "Bearer ")
		bearer = strings.TrimPrefix(bearer, "bearer ")
	}
	if vals, ok := params.Get("x-user-id"); ok && len(vals) > 0 {
		userSub = vals[0]
	}
	return kagentapi.AuthInfo{BearerToken: bearer, UserSub: userSub}
}

// formatMemoryChunks formats retrieved memory chunks as a system-prompt block
// that Claude will see before processing the user turn.
func formatMemoryChunks(chunks []memory.Chunk) string {
	var sb strings.Builder
	sb.WriteString("[Relevant memory from previous sessions]\n")
	for _, c := range chunks {
		sb.WriteString("- ")
		sb.WriteString(c.Content)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// adkUsageMetadata converts a CollectTokenUsage result to the three-field map
// that kagent's UI reads under the "adk_usage_metadata" key.
// Returns nil when usage is nil or both input and output are zero.
func adkUsageMetadata(usage *claude.TokenUsage) map[string]any {
	if usage == nil {
		return nil
	}
	input := usage.InputTokens
	output := usage.OutputTokens
	if input == 0 && output == 0 {
		return nil
	}
	return map[string]any{
		"adk_usage_metadata": map[string]any{
			"totalTokenCount":      input + output,
			"promptTokenCount":     input,
			"candidatesTokenCount": output,
		},
	}
}

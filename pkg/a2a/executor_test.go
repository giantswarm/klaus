package a2a_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/giantswarm/klaus/pkg/a2a"
	"github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/memory"
	"github.com/giantswarm/klaus/pkg/session"
)

// fakePrompter is a minimal claude.Prompter for testing.
type fakePrompter struct {
	runErr    error
	messages  []claude.StreamMessage
	statusVal claude.StatusInfo
	result    string

	// capturedOpts records the RunOptions passed on each call.
	capturedOpts []*claude.RunOptions
}

func (f *fakePrompter) Run(ctx context.Context, prompt string) (<-chan claude.StreamMessage, error) {
	return f.RunWithOptions(ctx, prompt, nil)
}

func (f *fakePrompter) RunWithOptions(_ context.Context, _ string, opts *claude.RunOptions) (<-chan claude.StreamMessage, error) {
	f.capturedOpts = append(f.capturedOpts, opts)
	if f.runErr != nil {
		return nil, f.runErr
	}
	// Use explicit messages when provided; otherwise synthesise from status
	// fields so that sessionID and result flow through the stream (as the real
	// *Process does), not through Status() which the executor no longer reads.
	msgs := f.messages
	if len(msgs) == 0 {
		if f.statusVal.SessionID != "" {
			msgs = append(msgs, claude.StreamMessage{
				Type:      claude.MessageTypeSystem,
				SessionID: f.statusVal.SessionID,
			})
		}
		if f.result != "" {
			msgs = append(msgs, claude.StreamMessage{
				Type:   claude.MessageTypeResult,
				Result: f.result,
			})
		}
	}
	ch := make(chan claude.StreamMessage, len(msgs)+1)
	for _, m := range msgs {
		ch <- m
	}
	close(ch)
	return ch, nil
}

func (f *fakePrompter) RunSyncWithOptions(ctx context.Context, prompt string, opts *claude.RunOptions) (string, []claude.StreamMessage, error) {
	ch, err := f.RunWithOptions(ctx, prompt, opts)
	if err != nil {
		return "", nil, err
	}
	var msgs []claude.StreamMessage
	for m := range ch {
		msgs = append(msgs, m)
	}
	return f.result, msgs, nil
}

func (f *fakePrompter) Submit(_ context.Context, _ string, _ *claude.RunOptions) error { return nil }
func (f *fakePrompter) Status() claude.StatusInfo                                      { return f.statusVal }
func (f *fakePrompter) Stop() error                                                    { return nil }
func (f *fakePrompter) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (f *fakePrompter) ResultDetail() claude.ResultDetailInfo {
	return claude.ResultDetailInfo{ResultText: f.result}
}
func (f *fakePrompter) Messages() claude.MessagesInfo { return claude.MessagesInfo{} }
func (f *fakePrompter) RawMessages(_ int, _ []string) claude.RawMessagesInfo {
	return claude.RawMessagesInfo{}
}
func (f *fakePrompter) OpenAIMessages(_ int) claude.OpenAIMessagesInfo {
	return claude.OpenAIMessagesInfo{}
}
func (f *fakePrompter) MarshalStatus() ([]byte, error) { return json.Marshal(f.statusVal) }

// collectEvents drains the iterator and returns all events. Any iteration error
// is returned immediately alongside the events collected so far.
func collectEvents(ctx context.Context, seq func(yield func(a2asdk.Event, error) bool)) ([]a2asdk.Event, error) {
	var events []a2asdk.Event
	for event, err := range seq {
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
	_ = ctx
	return events, nil
}

// makeExecCtx builds a minimal ExecutorContext for testing.
func makeExecCtx(contextID string, text string) *a2asrv.ExecutorContext {
	var msg *a2asdk.Message
	if text != "" {
		msg = a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart(text))
		msg.ContextID = contextID
	}
	return &a2asrv.ExecutorContext{
		ContextID: contextID,
		TaskID:    "task-123",
		Message:   msg,
	}
}

func TestExecutor_SlashCommandIntercept(t *testing.T) {
	prompter := &fakePrompter{result: "done"}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil, memory.NoOp{})

	execCtx := makeExecCtx("ctx-1", "/clear please clear the context")
	events, err := collectEvents(t.Context(), exec.Execute(t.Context(), execCtx))
	require.NoError(t, err)

	// Should have written an artifact event (the intercept reply) + completed.
	require.GreaterOrEqual(t, len(events), 2)

	// The last event must be a terminal completed state.
	last := events[len(events)-1]
	statusEv, ok := last.(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok, "last event should be TaskStatusUpdateEvent, got %T", last)
	assert.Equal(t, a2asdk.TaskStateCompleted, statusEv.Status.State)
	assert.True(t, statusEv.Status.State.Terminal())

	// The prompter must NOT have been called for intercepted commands.
	require.Empty(t, prompter.capturedOpts, "RunWithOptions must not be called for intercepted slash commands")
}

func TestExecutor_EmptyText(t *testing.T) {
	prompter := &fakePrompter{}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil, memory.NoOp{})

	execCtx := makeExecCtx("ctx-2", "")

	events, err := collectEvents(t.Context(), exec.Execute(t.Context(), execCtx))
	require.NoError(t, err)

	require.Len(t, events, 1)
	statusEv, ok := events[0].(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateFailed, statusEv.Status.State)
	assert.True(t, statusEv.Status.State.Terminal())
}

func TestExecutor_BusyReturnsRejected(t *testing.T) {
	prompter := &fakePrompter{runErr: claude.ErrBusy}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil, memory.NoOp{})

	execCtx := makeExecCtx("ctx-3", "hello")
	events, err := collectEvents(t.Context(), exec.Execute(t.Context(), execCtx))
	require.NoError(t, err)

	// working event + rejected event
	require.GreaterOrEqual(t, len(events), 1)
	last := events[len(events)-1]
	statusEv, ok := last.(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateRejected, statusEv.Status.State)
	assert.True(t, statusEv.Status.State.Terminal())
}

func TestExecutor_SuccessfulTurn(t *testing.T) {
	prompter := &fakePrompter{
		result: "The answer is 42.",
		statusVal: claude.StatusInfo{
			Status:    claude.ProcessStatusCompleted,
			SessionID: "sess-abc",
			Result:    "The answer is 42.",
		},
	}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil, memory.NoOp{})

	execCtx := makeExecCtx("ctx-4", "What is the answer?")
	events, err := collectEvents(t.Context(), exec.Execute(t.Context(), execCtx))
	require.NoError(t, err)

	// Expect: working → artifact → completed.
	require.GreaterOrEqual(t, len(events), 3)

	// The last event must be terminal completed.
	last := events[len(events)-1]
	statusEv, ok := last.(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateCompleted, statusEv.Status.State)
	assert.True(t, statusEv.Status.State.Terminal())

	// Conversation turns are recorded asynchronously; give them time to land.
	require.Eventually(t, func() bool {
		turns, histErr := store.History(t.Context(), "ctx-4")
		return histErr == nil && len(turns) >= 2
	}, 2*time.Second, 5*time.Millisecond)

	turns, err := store.History(t.Context(), "ctx-4")
	require.NoError(t, err)
	require.Len(t, turns, 2)
	assert.Equal(t, "user", turns[0].Role)
	assert.Equal(t, "assistant", turns[1].Role)
}

func TestExecutor_ConcurrentContextRejected(t *testing.T) {
	// Use a prompter that blocks until the context is cancelled.
	blocked := make(chan struct{})
	blocker := &blockingPrompter{
		fakePrompter: &fakePrompter{},
		block:        blocked,
	}

	store := session.NewMemoryStore()
	exec := a2a.New(blocker, store, a2a.ModeChat, nil, memory.NoOp{})

	// First request: holds the lock. Signal when the first event arrives.
	ctx1, cancel1 := context.WithCancel(t.Context())
	execCtx1 := makeExecCtx("ctx-concurrent", "first")
	firstEvent := make(chan struct{})

	doneCh := make(chan error, 1)
	go func() {
		var firstSent bool
		for _, iterErr := range exec.Execute(ctx1, execCtx1) {
			if !firstSent {
				close(firstEvent)
				firstSent = true
			}
			if iterErr != nil {
				doneCh <- iterErr
				return
			}
		}
		doneCh <- nil
	}()

	// Wait until the first (working) event has been yielded — proves lock is held.
	select {
	case <-firstEvent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first event from first request")
	}

	// Second request to the SAME contextID — must be rejected immediately.
	execCtx2 := makeExecCtx("ctx-concurrent", "second")
	events2, err := collectEvents(t.Context(), exec.Execute(t.Context(), execCtx2))
	require.NoError(t, err)

	require.Len(t, events2, 1)
	statusEv, ok := events2[0].(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateRejected, statusEv.Status.State)

	// Unblock the first request.
	cancel1()
	close(blocked)
	<-doneCh
}

func TestExecutor_Cancel(t *testing.T) {
	prompter := &fakePrompter{}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil, memory.NoOp{})

	execCtx := makeExecCtx("ctx-cancel", "")
	events, err := collectEvents(t.Context(), exec.Cancel(t.Context(), execCtx))
	require.NoError(t, err)

	require.Len(t, events, 1)
	statusEv, ok := events[0].(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateCanceled, statusEv.Status.State)
	assert.True(t, statusEv.Status.State.Terminal())
}

const (
	timeout      = 2e9 // 2 seconds
	pollInterval = 5e6 // 5 ms
)

// blockingPrompter wraps fakePrompter with a blocking RunWithOptions.
type blockingPrompter struct {
	*fakePrompter
	block chan struct{}
}

func (b *blockingPrompter) RunWithOptions(ctx context.Context, prompt string, opts *claude.RunOptions) (<-chan claude.StreamMessage, error) {
	ch := make(chan claude.StreamMessage)
	go func() {
		defer close(ch)
		select {
		case <-ctx.Done():
		case <-b.block:
		}
	}()
	return ch, nil
}

func TestExecutor_SubprocessError(t *testing.T) {
	prompter := &fakePrompter{
		statusVal: claude.StatusInfo{
			Status:       claude.ProcessStatusError,
			ErrorMessage: "subprocess exited with code 1",
		},
	}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil, memory.NoOp{})

	events, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-err", "hello")))
	require.NoError(t, err)

	last := events[len(events)-1]
	statusEv, ok := last.(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateFailed, statusEv.Status.State)
	assert.True(t, statusEv.Status.State.Terminal())
}

func TestExecutor_CollectResultTextFallback(t *testing.T) {
	// Status().Result and ResultDetail().ResultText are both empty.
	// The result must come from the MessageTypeResult message in the stream.
	resultMsg := claude.StreamMessage{
		Type:   claude.MessageTypeResult,
		Result: "full response from stream",
	}
	prompter := &fakePrompter{
		messages:  []claude.StreamMessage{resultMsg},
		statusVal: claude.StatusInfo{Status: claude.ProcessStatusIdle},
	}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil, memory.NoOp{})

	events, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-fallback", "hello")))
	require.NoError(t, err)

	var artifactEv *a2asdk.TaskArtifactUpdateEvent
	for _, ev := range events {
		if ae, ok := ev.(*a2asdk.TaskArtifactUpdateEvent); ok {
			artifactEv = ae
			break
		}
	}
	require.NotNil(t, artifactEv, "expected artifact-update event from CollectResultText fallback")
	require.Len(t, artifactEv.Artifact.Parts, 1)
	assert.Equal(t, "full response from stream", artifactEv.Artifact.Parts[0].Text())
	assert.True(t, artifactEv.LastChunk)
}

func TestExecutor_ResumeAlwaysDerivedFromContextID(t *testing.T) {
	// Both turns must use --resume derived from the A2A contextID, regardless
	// of what session ID the subprocess emits. The store is never consulted.
	prompter := &fakePrompter{
		result: "turn result",
		statusVal: claude.StatusInfo{
			Status:    claude.ProcessStatusCompleted,
			SessionID: "subprocess-reported-id",
			Result:    "turn result",
		},
	}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeAgent, nil, memory.NoOp{})

	_, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-derived", "first")))
	require.NoError(t, err)

	// Second turn: subprocess reports a different session ID — must not affect RunOptions.
	prompter.statusVal.SessionID = "different-session-id"
	_, err = collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-derived", "second")))
	require.NoError(t, err)

	require.Len(t, prompter.capturedOpts, 2)
	assert.Equal(t, "derived", prompter.capturedOpts[0].Resume)
	assert.Equal(t, "derived", prompter.capturedOpts[1].Resume)
	assert.Empty(t, prompter.capturedOpts[0].SessionID)
	assert.Empty(t, prompter.capturedOpts[1].SessionID)
}

// TestExecutor_AgentModeResume verifies that the second turn in agent mode
// passes only --resume (not --session-id), which is what the claude CLI requires.
func TestExecutor_AgentModeResume(t *testing.T) {
	const sessionID = "sess-resume-test"
	prompter := &fakePrompter{
		result: "turn result",
		statusVal: claude.StatusInfo{
			Status:    claude.ProcessStatusCompleted,
			SessionID: sessionID,
			Result:    "turn result",
		},
	}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeAgent, nil, memory.NoOp{})

	// First turn — no prior session.
	events1, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-resume", "first")))
	require.NoError(t, err)
	last1 := events1[len(events1)-1].(*a2asdk.TaskStatusUpdateEvent)
	assert.Equal(t, a2asdk.TaskStateCompleted, last1.Status.State)

	// First turn RunOptions: Resume is derived from contextID; SessionID is not set.
	require.Len(t, prompter.capturedOpts, 1)
	opts1 := prompter.capturedOpts[0]
	require.NotNil(t, opts1)
	assert.Equal(t, "resume", opts1.Resume, "first turn derives --resume from contextID")
	assert.Empty(t, opts1.SessionID, "first turn must not set --session-id")
	assert.True(t, opts1.SaveSession, "first turn must set SaveSession so the conversation is persisted")

	// Second turn — session ID is always derived from contextID, not from store.
	events2, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-resume", "second")))
	require.NoError(t, err)
	last2 := events2[len(events2)-1].(*a2asdk.TaskStatusUpdateEvent)
	assert.Equal(t, a2asdk.TaskStateCompleted, last2.Status.State)

	// Second turn RunOptions: same derived Resume, no SessionID.
	require.Len(t, prompter.capturedOpts, 2)
	opts2 := prompter.capturedOpts[1]
	require.NotNil(t, opts2)
	assert.Equal(t, "resume", opts2.Resume, "second turn derives --resume from contextID")
	assert.Empty(t, opts2.SessionID, "second turn must not pass --session-id")
	assert.True(t, opts2.SaveSession, "second turn must set SaveSession")
}

// staleResumePrompter simulates a subprocess that fails on the first (resume)
// call but succeeds on the retry (fresh session). Used to test stale-resume
// detection after pod restart with emptyDir workspace.
type staleResumePrompter struct {
	*fakePrompter
	calls int
}

func (p *staleResumePrompter) RunWithOptions(_ context.Context, _ string, opts *claude.RunOptions) (<-chan claude.StreamMessage, error) {
	p.calls++
	p.capturedOpts = append(p.capturedOpts, opts)
	var msgs []claude.StreamMessage
	if p.calls >= 2 {
		// Retry succeeds: emit session + result.
		msgs = []claude.StreamMessage{
			{Type: claude.MessageTypeSystem, SessionID: "new-session-id"},
			{Type: claude.MessageTypeResult, Result: "retry succeeded"},
		}
	}
	// First call: empty channel — no session ID emitted, simulates failed resume.
	ch := make(chan claude.StreamMessage, len(msgs)+1)
	for _, m := range msgs {
		ch <- m
	}
	close(ch)
	return ch, nil
}

func (p *staleResumePrompter) Status() claude.StatusInfo {
	if p.calls <= 1 {
		// After first (failed) call: subprocess error.
		return claude.StatusInfo{Status: claude.ProcessStatusError, ErrorMessage: "exit status 1"}
	}
	return claude.StatusInfo{Status: claude.ProcessStatusIdle}
}

// TestExecutor_StaleResumeRetry verifies that when --resume fails (session file
// gone after pod restart), the executor retries with a fresh session and the
// turn completes rather than failing.
func TestExecutor_StaleResumeRetry(t *testing.T) {
	inner := &fakePrompter{}
	prompter := &staleResumePrompter{fakePrompter: inner}

	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeAgent, nil, memory.NoOp{})

	events, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-stale", "hello after restart")))
	require.NoError(t, err)

	// Turn must succeed, not fail.
	last := events[len(events)-1]
	statusEv, ok := last.(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateCompleted, statusEv.Status.State, "stale resume should retry and complete")
	assert.True(t, statusEv.Status.State.Terminal())

	// Two RunWithOptions calls: first with Resume derived from contextID, second fresh with SessionID.
	require.Len(t, prompter.capturedOpts, 2, "expected exactly two RunWithOptions calls")
	assert.Equal(t, "stale", prompter.capturedOpts[0].Resume, "first call must derive --resume from contextID")
	assert.Empty(t, prompter.capturedOpts[1].Resume, "retry call must not pass --resume")
	assert.Equal(t, "stale", prompter.capturedOpts[1].SessionID, "retry must seed session ID from contextID")
}

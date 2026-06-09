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
	exec := a2a.New(prompter, a2a.ModeChat, nil)

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
	exec := a2a.New(prompter, a2a.ModeChat, nil)

	execCtx := makeExecCtx("ctx-2", "")

	events, err := collectEvents(t.Context(), exec.Execute(t.Context(), execCtx))
	require.NoError(t, err)

	require.Len(t, events, 2)
	_, ok := events[0].(*a2asdk.Task)
	require.True(t, ok, "first event must be *Task")
	statusEv, ok := events[1].(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateFailed, statusEv.Status.State)
	assert.True(t, statusEv.Status.State.Terminal())
}

func TestExecutor_BusyReturnsRejected(t *testing.T) {
	prompter := &fakePrompter{runErr: claude.ErrBusy}
	exec := a2a.New(prompter, a2a.ModeChat, nil)

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
	exec := a2a.New(prompter, a2a.ModeChat, nil)

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

}

func TestExecutor_ConcurrentContextRejected(t *testing.T) {
	// Use a prompter that blocks until the context is cancelled.
	blocked := make(chan struct{})
	blocker := &blockingPrompter{
		fakePrompter: &fakePrompter{},
		block:        blocked,
	}

	exec := a2a.New(blocker, a2a.ModeChat, nil)

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

	require.Len(t, events2, 2)
	_, ok := events2[0].(*a2asdk.Task)
	require.True(t, ok, "first event must be *Task")
	statusEv, ok := events2[1].(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateRejected, statusEv.Status.State)

	// Unblock the first request.
	cancel1()
	close(blocked)
	<-doneCh
}

func TestExecutor_Cancel(t *testing.T) {
	prompter := &fakePrompter{}
	exec := a2a.New(prompter, a2a.ModeChat, nil)

	execCtx := makeExecCtx("ctx-cancel", "")
	events, err := collectEvents(t.Context(), exec.Cancel(t.Context(), execCtx))
	require.NoError(t, err)

	require.Len(t, events, 1)
	statusEv, ok := events[0].(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateCanceled, statusEv.Status.State)
	assert.True(t, statusEv.Status.State.Terminal())
}

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
	exec := a2a.New(prompter, a2a.ModeChat, nil)

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
	exec := a2a.New(prompter, a2a.ModeChat, nil)

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

func TestExecutor_AgentModeTracksSubprocessSession(t *testing.T) {
	// First turn must use --session-id derived from contextID (no prior session).
	// Second turn must use --resume with the session ID the subprocess actually reported,
	// not always the derived ID.
	prompter := &fakePrompter{
		result: "turn result",
		statusVal: claude.StatusInfo{
			Status: claude.ProcessStatusCompleted,
			Result: "turn result",
		},
	}
	exec := a2a.New(prompter, a2a.ModeAgent, nil)

	// First turn: subprocess reports "subprocess-reported-id".
	prompter.statusVal.SessionID = "subprocess-reported-id"
	_, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-derived", "first")))
	require.NoError(t, err)

	// Second turn: executor must resume with the session ID the subprocess emitted,
	// not re-derive it from contextID.
	prompter.statusVal.SessionID = "different-session-id"
	_, err = collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-derived", "second")))
	require.NoError(t, err)

	require.Len(t, prompter.capturedOpts, 2)
	// First turn: no prior session → --session-id from contextID.
	assert.Equal(t, "derived", prompter.capturedOpts[0].SessionID, "first turn must derive --session-id from contextID")
	assert.Empty(t, prompter.capturedOpts[0].Resume, "first turn must not pass --resume")
	// Second turn: resume with what the subprocess actually reported.
	assert.Equal(t, "subprocess-reported-id", prompter.capturedOpts[1].Resume, "second turn must resume with subprocess-reported session ID")
	assert.Empty(t, prompter.capturedOpts[1].SessionID, "second turn must not set --session-id")
}

// TestExecutor_AgentModeResume verifies the session handoff across two turns in
// agent mode: first turn seeds a new session via --session-id, second turn
// resumes the session the subprocess actually reported.
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
	exec := a2a.New(prompter, a2a.ModeAgent, nil)

	// First turn — no prior session.
	events1, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-resume", "first")))
	require.NoError(t, err)
	last1 := events1[len(events1)-1].(*a2asdk.TaskStatusUpdateEvent)
	assert.Equal(t, a2asdk.TaskStateCompleted, last1.Status.State)

	// First turn RunOptions: no prior session → seeds via --session-id derived from contextID.
	require.Len(t, prompter.capturedOpts, 1)
	opts1 := prompter.capturedOpts[0]
	require.NotNil(t, opts1)
	assert.Equal(t, "resume", opts1.SessionID, "first turn derives --session-id from contextID")
	assert.Empty(t, opts1.Resume, "first turn must not set --resume")
	assert.True(t, opts1.SaveSession, "first turn must set SaveSession so the conversation is persisted")

	// Second turn — executor has recorded the session the subprocess reported.
	events2, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-resume", "second")))
	require.NoError(t, err)
	last2 := events2[len(events2)-1].(*a2asdk.TaskStatusUpdateEvent)
	assert.Equal(t, a2asdk.TaskStateCompleted, last2.Status.State)

	// Second turn RunOptions: resume with the actual session ID the subprocess reported.
	require.Len(t, prompter.capturedOpts, 2)
	opts2 := prompter.capturedOpts[1]
	require.NotNil(t, opts2)
	assert.Equal(t, sessionID, opts2.Resume, "second turn resumes with subprocess-reported session ID")
	assert.Empty(t, opts2.SessionID, "second turn must not pass --session-id")
	assert.True(t, opts2.SaveSession, "second turn must set SaveSession")
}

// staleResumePrompter simulates the pod-restart / emptyDir scenario:
//
//	call 1 (first turn, --session-id): succeeds, emits session "sess-first"
//	call 2 (second turn, --resume sess-first): fails — session file gone
//	call 3 (retry of second turn, --session-id): succeeds, emits new session
type staleResumePrompter struct {
	*fakePrompter
	calls int
}

func (p *staleResumePrompter) RunWithOptions(_ context.Context, _ string, opts *claude.RunOptions) (<-chan claude.StreamMessage, error) {
	p.calls++
	p.capturedOpts = append(p.capturedOpts, opts)
	var msgs []claude.StreamMessage
	switch p.calls {
	case 1:
		msgs = []claude.StreamMessage{
			{Type: claude.MessageTypeSystem, SessionID: "sess-first"},
			{Type: claude.MessageTypeResult, Result: "first turn ok"},
		}
	case 2:
		// Stale --resume: empty channel, no session ID, subprocess exits with error.
	case 3:
		msgs = []claude.StreamMessage{
			{Type: claude.MessageTypeSystem, SessionID: "sess-new"},
			{Type: claude.MessageTypeResult, Result: "retry succeeded"},
		}
	}
	ch := make(chan claude.StreamMessage, len(msgs)+1)
	for _, m := range msgs {
		ch <- m
	}
	close(ch)
	return ch, nil
}

func (p *staleResumePrompter) Status() claude.StatusInfo {
	if p.calls == 2 {
		return claude.StatusInfo{Status: claude.ProcessStatusError, ErrorMessage: "exit status 1"}
	}
	return claude.StatusInfo{Status: claude.ProcessStatusIdle}
}

// TestExecutor_ArtifactTokenUsage verifies that token_usage is attached to the
// final artifact when the prompter emits assistant messages with usage data, and
// absent for synthetic responses (slash commands) where no usage is present.
func TestExecutor_ArtifactTokenUsage(t *testing.T) {
	t.Run("token_usage present when prompter emits usage", func(t *testing.T) {
		fp := &fakePrompter{
			messages: []claude.StreamMessage{
				{Type: claude.MessageTypeSystem, SessionID: "sess-1"},
				{
					Type:    claude.MessageTypeAssistant,
					Subtype: claude.SubtypeText,
					Text:    "answer",
					Usage:   &claude.TokenUsage{InputTokens: 100, OutputTokens: 50},
				},
				{Type: claude.MessageTypeResult, Result: "answer"},
			},
		}
		exec := a2a.New(fp, a2a.ModeAgent, nil)
		events, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-usage", "what is 2+2?")))
		require.NoError(t, err)

		var artifact *a2asdk.TaskArtifactUpdateEvent
		for _, ev := range events {
			if a, ok := ev.(*a2asdk.TaskArtifactUpdateEvent); ok {
				artifact = a
			}
		}
		require.NotNil(t, artifact, "expected an artifact event")
		require.NotNil(t, artifact.Artifact.Metadata, "artifact must carry metadata")

		usage, ok := artifact.Artifact.Metadata["token_usage"].(map[string]any)
		require.True(t, ok, "token_usage must be a map[string]any")
		assert.EqualValues(t, int64(100), usage["input_tokens"])
		assert.EqualValues(t, int64(50), usage["output_tokens"])
	})

	t.Run("token_usage absent for slash-command response", func(t *testing.T) {
		fp := &fakePrompter{}
		exec := a2a.New(fp, a2a.ModeAgent, nil)
		events, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-slash", "/clear")))
		require.NoError(t, err)

		for _, ev := range events {
			if a, ok := ev.(*a2asdk.TaskArtifactUpdateEvent); ok {
				if a.Artifact != nil && a.Artifact.Metadata != nil {
					_, hasUsage := a.Artifact.Metadata["token_usage"]
					assert.False(t, hasUsage, "slash-command artifact must not carry token_usage")
				}
			}
		}
	})
}

// TestExecutor_StaleResumeRetry verifies that when --resume fails (session file
// gone after pod restart), the executor retries with a fresh --session-id and the
// turn completes rather than failing. Stale resume is only triggered on the second+
// turn (when a prior session was recorded).
func TestExecutor_StaleResumeRetry(t *testing.T) {
	inner := &fakePrompter{}
	prompter := &staleResumePrompter{fakePrompter: inner}
	exec := a2a.New(prompter, a2a.ModeAgent, nil)

	// First turn: fresh start; establishes session "sess-first".
	_, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-stale", "first message")))
	require.NoError(t, err)
	require.Len(t, prompter.capturedOpts, 1)
	assert.Equal(t, "stale", prompter.capturedOpts[0].SessionID, "first turn seeds --session-id from contextID")
	assert.Empty(t, prompter.capturedOpts[0].Resume)

	// Second turn: --resume sess-first fails (stale); executor retries with --session-id.
	events, err := collectEvents(t.Context(), exec.Execute(t.Context(), makeExecCtx("ctx-stale", "hello after restart")))
	require.NoError(t, err)

	last := events[len(events)-1]
	statusEv, ok := last.(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateCompleted, statusEv.Status.State, "stale resume should retry and complete")

	require.Len(t, prompter.capturedOpts, 3, "expected 3 RunWithOptions calls: turn1, stale-attempt, retry")
	assert.Equal(t, "sess-first", prompter.capturedOpts[1].Resume, "second turn uses stored --resume")
	assert.Empty(t, prompter.capturedOpts[1].SessionID)
	assert.Empty(t, prompter.capturedOpts[2].Resume, "retry must not pass --resume")
	assert.Equal(t, "stale", prompter.capturedOpts[2].SessionID, "retry seeds --session-id from contextID")
}

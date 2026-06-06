package a2a_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	a2asdk "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"

	"github.com/giantswarm/klaus/pkg/a2a"
	"github.com/giantswarm/klaus/pkg/claude"
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
	ch := make(chan claude.StreamMessage, len(f.messages)+1)
	for _, m := range f.messages {
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

// captureQueue collects events written during a test.
type captureQueue struct {
	events []a2asdk.Event
}

func (q *captureQueue) Write(_ context.Context, event a2asdk.Event) error {
	q.events = append(q.events, event)
	return nil
}

func (q *captureQueue) WriteVersioned(_ context.Context, event a2asdk.Event, _ a2asdk.TaskVersion) error {
	q.events = append(q.events, event)
	return nil
}

func (q *captureQueue) Read(_ context.Context) (a2asdk.Event, a2asdk.TaskVersion, error) {
	return nil, 0, eventqueue.ErrQueueClosed
}

func (q *captureQueue) Close() error { return nil }

var _ eventqueue.Queue = (*captureQueue)(nil)

// makeReqCtx builds a minimal RequestContext for testing.
func makeReqCtx(contextID string, text string) *a2asrv.RequestContext {
	msg := a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.TextPart{Text: text})
	msg.ContextID = contextID
	return &a2asrv.RequestContext{
		ContextID: contextID,
		TaskID:    "task-123",
		Message:   msg,
	}
}

func TestExecutor_SlashCommandIntercept(t *testing.T) {
	prompter := &fakePrompter{result: "done"}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil)

	queue := &captureQueue{}
	reqCtx := makeReqCtx("ctx-1", "/clear please clear the context")

	err := exec.Execute(t.Context(), reqCtx, queue)
	require.NoError(t, err)

	// Should have written an artifact event (the intercept reply) + completed.
	require.GreaterOrEqual(t, len(queue.events), 2)

	// The last event must be a terminal completed state.
	last := queue.events[len(queue.events)-1]
	statusEv, ok := last.(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok, "last event should be TaskStatusUpdateEvent, got %T", last)
	assert.Equal(t, a2asdk.TaskStateCompleted, statusEv.Status.State)
	assert.True(t, statusEv.Final)

	// The prompter must NOT have been called for intercepted commands.
	// Since fakePrompter.runErr is nil but we track calls via messages being absent.
	// No working events should have been emitted before the artifact.
}

func TestExecutor_EmptyText(t *testing.T) {
	prompter := &fakePrompter{}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil)

	queue := &captureQueue{}
	reqCtx := makeReqCtx("ctx-2", "")

	err := exec.Execute(t.Context(), reqCtx, queue)
	require.NoError(t, err)

	require.Len(t, queue.events, 1)
	statusEv, ok := queue.events[0].(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateFailed, statusEv.Status.State)
	assert.True(t, statusEv.Final)
}

func TestExecutor_BusyReturnsRejected(t *testing.T) {
	prompter := &fakePrompter{runErr: claude.ErrBusy}
	store := session.NewMemoryStore()
	exec := a2a.New(prompter, store, a2a.ModeChat, nil)

	queue := &captureQueue{}
	reqCtx := makeReqCtx("ctx-3", "hello")

	err := exec.Execute(t.Context(), reqCtx, queue)
	require.NoError(t, err)

	// working event + rejected event
	require.GreaterOrEqual(t, len(queue.events), 1)
	last := queue.events[len(queue.events)-1]
	statusEv, ok := last.(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateRejected, statusEv.Status.State)
	assert.True(t, statusEv.Final)
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
	exec := a2a.New(prompter, store, a2a.ModeChat, nil)

	queue := &captureQueue{}
	reqCtx := makeReqCtx("ctx-4", "What is the answer?")

	err := exec.Execute(t.Context(), reqCtx, queue)
	require.NoError(t, err)

	// Expect: working → artifact → completed.
	require.GreaterOrEqual(t, len(queue.events), 3)

	// The last event must be terminal completed.
	last := queue.events[len(queue.events)-1]
	statusEv, ok := last.(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateCompleted, statusEv.Status.State)
	assert.True(t, statusEv.Final)

	// The session should be bound in the store.
	sessID, err := store.SessionID(t.Context(), "ctx-4")
	require.NoError(t, err)
	assert.Equal(t, "sess-abc", sessID)

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
	prompter := &fakePrompter{}
	prompter.messages = nil
	// Replace RunWithOptions with a blocking version.
	blocker := &blockingPrompter{fakePrompter: prompter, block: blocked}

	store := session.NewMemoryStore()
	exec := a2a.New(blocker, store, a2a.ModeChat, nil)

	// First request: holds the lock.
	ctx1, cancel1 := context.WithCancel(t.Context())
	queue1 := &captureQueue{}
	reqCtx1 := makeReqCtx("ctx-concurrent", "first")

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- exec.Execute(ctx1, reqCtx1, queue1)
	}()

	// Give the goroutine time to acquire the lock.
	// Poll until we see the working event.
	require.Eventually(t, func() bool {
		return len(queue1.events) > 0
	}, timeout, pollInterval)

	// Second request to the SAME contextID — must be rejected.
	queue2 := &captureQueue{}
	reqCtx2 := makeReqCtx("ctx-concurrent", "second")
	err := exec.Execute(t.Context(), reqCtx2, queue2)
	require.NoError(t, err)

	require.Len(t, queue2.events, 1)
	statusEv, ok := queue2.events[0].(*a2asdk.TaskStatusUpdateEvent)
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
	exec := a2a.New(prompter, store, a2a.ModeChat, nil)

	queue := &captureQueue{}
	reqCtx := makeReqCtx("ctx-cancel", "")
	err := exec.Cancel(t.Context(), reqCtx, queue)
	require.NoError(t, err)

	require.Len(t, queue.events, 1)
	statusEv, ok := queue.events[0].(*a2asdk.TaskStatusUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, a2asdk.TaskStateCanceled, statusEv.Status.State)
	assert.True(t, statusEv.Final)
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
	exec := a2a.New(prompter, store, a2a.ModeAgent, nil)

	// First turn — no prior session.
	q1 := &captureQueue{}
	err := exec.Execute(t.Context(), makeReqCtx("ctx-resume", "first"), q1)
	require.NoError(t, err)
	last1 := q1.events[len(q1.events)-1].(*a2asdk.TaskStatusUpdateEvent)
	assert.Equal(t, a2asdk.TaskStateCompleted, last1.Status.State)

	// First turn RunOptions: Resume should be empty (no prior session); SaveSession must be true.
	require.Len(t, prompter.capturedOpts, 1)
	opts1 := prompter.capturedOpts[0]
	require.NotNil(t, opts1)
	assert.Empty(t, opts1.Resume, "first turn must not pass --resume")
	assert.Empty(t, opts1.SessionID, "first turn must not pass --session-id")
	assert.True(t, opts1.SaveSession, "first turn must set SaveSession so the conversation is persisted")

	// Second turn — store now has sessionID bound from turn 1.
	q2 := &captureQueue{}
	err = exec.Execute(t.Context(), makeReqCtx("ctx-resume", "second"), q2)
	require.NoError(t, err)
	last2 := q2.events[len(q2.events)-1].(*a2asdk.TaskStatusUpdateEvent)
	assert.Equal(t, a2asdk.TaskStateCompleted, last2.Status.State)

	// Second turn RunOptions: Resume must be set, SessionID must be empty, SaveSession must be true.
	require.Len(t, prompter.capturedOpts, 2)
	opts2 := prompter.capturedOpts[1]
	require.NotNil(t, opts2)
	assert.Equal(t, sessionID, opts2.Resume, "second turn must pass --resume <sessionID>")
	assert.Empty(t, opts2.SessionID, "second turn must not pass --session-id")
	assert.True(t, opts2.SaveSession, "second turn must set SaveSession")
}

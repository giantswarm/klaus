package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	claudepkg "github.com/giantswarm/klaus/pkg/claude"

	"github.com/mark3labs/mcp-go/mcp"
)

// mockPrompter is a test double implementing claude.Prompter.
type mockPrompter struct {
	lastPrompt  string
	lastRunOpts *claudepkg.RunOptions

	// result is sent on the channel returned by RunWithOptions.
	result     string
	sessionID  string
	totalCost  float64
	messages   []claudepkg.StreamMessage
	runErr     error
	status     claudepkg.StatusInfo
	stopCalled bool

	// submitCalled tracks whether Submit was called instead of RunWithOptions.
	submitCalled bool
	// resultDetail is returned by ResultDetail.
	resultDetail claudepkg.ResultDetailInfo
	// messagesInfo is returned by Messages.
	messagesInfo claudepkg.MessagesInfo
}

func (m *mockPrompter) Run(ctx context.Context, prompt string) (<-chan claudepkg.StreamMessage, error) {
	return m.RunWithOptions(ctx, prompt, nil)
}

func (m *mockPrompter) RunWithOptions(_ context.Context, prompt string, opts *claudepkg.RunOptions) (<-chan claudepkg.StreamMessage, error) {
	m.lastPrompt = prompt
	m.lastRunOpts = opts
	if m.runErr != nil {
		return nil, m.runErr
	}

	ch := make(chan claudepkg.StreamMessage, 10)
	// Emit configured messages, or a default system + result pair.
	if len(m.messages) > 0 {
		for _, msg := range m.messages {
			ch <- msg
		}
	} else {
		if m.sessionID != "" {
			ch <- claudepkg.StreamMessage{
				Type:      claudepkg.MessageTypeSystem,
				SessionID: m.sessionID,
			}
		}
		ch <- claudepkg.StreamMessage{
			Type:      claudepkg.MessageTypeResult,
			Result:    m.result,
			TotalCost: m.totalCost,
		}
	}
	close(ch)
	return ch, nil
}

func (m *mockPrompter) RunSyncWithOptions(ctx context.Context, prompt string, opts *claudepkg.RunOptions) (string, []claudepkg.StreamMessage, error) {
	ch, err := m.RunWithOptions(ctx, prompt, opts)
	if err != nil {
		return "", nil, err
	}
	var msgs []claudepkg.StreamMessage
	for msg := range ch {
		msgs = append(msgs, msg)
	}
	return m.result, msgs, nil
}

func (m *mockPrompter) Submit(_ context.Context, prompt string, opts *claudepkg.RunOptions) error {
	m.lastPrompt = prompt
	m.lastRunOpts = opts
	m.submitCalled = true
	if m.runErr != nil {
		return m.runErr
	}
	return nil
}

func (m *mockPrompter) Status() claudepkg.StatusInfo { return m.status }

func (m *mockPrompter) ResultDetail() claudepkg.ResultDetailInfo { return m.resultDetail }

func (m *mockPrompter) Stop() error {
	m.stopCalled = true
	return nil
}

func (m *mockPrompter) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (m *mockPrompter) Messages() claudepkg.MessagesInfo { return m.messagesInfo }

func (m *mockPrompter) MarshalStatus() ([]byte, error) {
	return json.Marshal(m.status)
}

// --- Prompt tool tests ---

func TestPromptTool_BlockingBasic(t *testing.T) {
	mock := &mockPrompter{
		result:    "Hello, world!",
		sessionID: "sess-123",
		totalCost: 0.05,
	}

	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message":  "Say hello",
		"blocking": true,
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	if mock.lastPrompt != "Say hello" {
		t.Errorf("expected prompt %q, got %q", "Say hello", mock.lastPrompt)
	}
	if mock.submitCalled {
		t.Error("expected RunWithOptions (not Submit) for blocking mode")
	}

	// Parse the JSON result.
	resp := parsePromptResponse(t, result)
	if resp.Result != "Hello, world!" {
		t.Errorf("expected result %q, got %q", "Hello, world!", resp.Result)
	}
	if resp.SessionID != "sess-123" {
		t.Errorf("expected session_id %q, got %q", "sess-123", resp.SessionID)
	}
	if resp.TotalCost == nil || *resp.TotalCost != 0.05 {
		t.Errorf("expected total_cost_usd 0.05, got %v", resp.TotalCost)
	}
}

func TestPromptTool_BlockingWithTokenUsage(t *testing.T) {
	mock := &mockPrompter{
		result:    "Done!",
		sessionID: "sess-tu",
		messages: []claudepkg.StreamMessage{
			{Type: claudepkg.MessageTypeSystem, SessionID: "sess-tu"},
			{
				Type:    claudepkg.MessageTypeAssistant,
				Subtype: claudepkg.SubtypeText,
				Text:    "thinking...",
				Usage: &claudepkg.TokenUsage{
					InputTokens:              1000,
					OutputTokens:             200,
					CacheCreationInputTokens: 500,
				},
			},
			{
				Type:    claudepkg.MessageTypeAssistant,
				Subtype: claudepkg.SubtypeText,
				Text:    "more thinking...",
				Usage: &claudepkg.TokenUsage{
					InputTokens:          2000,
					OutputTokens:         300,
					CacheReadInputTokens: 800,
				},
			},
			{
				Type:      claudepkg.MessageTypeResult,
				Result:    "Done!",
				TotalCost: 0.15,
			},
		},
	}

	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message":  "Do something",
		"blocking": true,
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	resp := parsePromptResponse(t, result)
	if resp.TotalCost == nil || *resp.TotalCost != 0.15 {
		t.Errorf("expected total_cost_usd 0.15, got %v", resp.TotalCost)
	}
	if resp.TokenUsage == nil {
		t.Fatal("expected non-nil token_usage")
	}
	if resp.TokenUsage.InputTokens != 3000 {
		t.Errorf("expected aggregated InputTokens 3000, got %d", resp.TokenUsage.InputTokens)
	}
	if resp.TokenUsage.OutputTokens != 500 {
		t.Errorf("expected aggregated OutputTokens 500, got %d", resp.TokenUsage.OutputTokens)
	}
	if resp.TokenUsage.CacheCreationInputTokens != 500 {
		t.Errorf("expected CacheCreationInputTokens 500, got %d", resp.TokenUsage.CacheCreationInputTokens)
	}
	if resp.TokenUsage.CacheReadInputTokens != 800 {
		t.Errorf("expected CacheReadInputTokens 800, got %d", resp.TokenUsage.CacheReadInputTokens)
	}
}

func TestPromptTool_BlockingCostFromPerMessageCost(t *testing.T) {
	// When total_cost_usd is missing from result, individual cost_usd
	// values from messages should be accumulated (issue #62).
	mock := &mockPrompter{
		result:    "Done!",
		sessionID: "sess-cost",
		messages: []claudepkg.StreamMessage{
			{Type: claudepkg.MessageTypeSystem, SessionID: "sess-cost"},
			{Type: claudepkg.MessageTypeAssistant, Subtype: claudepkg.SubtypeText, Text: "a", Cost: 0.01},
			{Type: claudepkg.MessageTypeAssistant, Subtype: claudepkg.SubtypeText, Text: "b", Cost: 0.02},
			{Type: claudepkg.MessageTypeResult, Result: "Done!"},
		},
	}

	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message":  "Hello",
		"blocking": true,
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	resp := parsePromptResponse(t, result)
	if resp.TotalCost == nil {
		t.Fatal("expected non-nil TotalCost when cost_usd present on messages")
	}
	if *resp.TotalCost != 0.03 {
		t.Errorf("expected accumulated cost 0.03, got %f", *resp.TotalCost)
	}
}

func TestPromptTool_BlockingNoCostReturnsNull(t *testing.T) {
	// When no cost data is present at all, TotalCost should be nil.
	mock := &mockPrompter{
		result:    "Done!",
		sessionID: "sess-nocost",
		messages: []claudepkg.StreamMessage{
			{Type: claudepkg.MessageTypeSystem, SessionID: "sess-nocost"},
			{Type: claudepkg.MessageTypeAssistant, Subtype: claudepkg.SubtypeText, Text: "thinking"},
			{Type: claudepkg.MessageTypeResult, Result: "Done!"},
		},
	}

	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message":  "Hello",
		"blocking": true,
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := parsePromptResponse(t, result)
	if resp.TotalCost != nil {
		t.Errorf("expected nil TotalCost when no cost data present, got %v", resp.TotalCost)
	}
}

func TestStatusTool_WithTokenUsage(t *testing.T) {
	mock := &mockPrompter{
		status: claudepkg.StatusInfo{
			Status:       claudepkg.ProcessStatusBusy,
			SessionID:    "sess-tu",
			MessageCount: 10,
			TotalCost:    claudepkg.Float64Ptr(0.25),
			TokenUsage: &claudepkg.TokenUsage{
				InputTokens:              5000,
				OutputTokens:             1000,
				CacheCreationInputTokens: 2000,
				CacheReadInputTokens:     3000,
			},
		},
	}

	tools := buildToolMap(mock)
	handler := tools["status"]

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var info claudepkg.StatusInfo
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}

	if info.TokenUsage == nil {
		t.Fatal("expected non-nil token_usage in status response")
	}
	if info.TokenUsage.InputTokens != 5000 {
		t.Errorf("expected InputTokens 5000, got %d", info.TokenUsage.InputTokens)
	}
	if info.TotalCost == nil || *info.TotalCost != 0.25 {
		t.Errorf("expected TotalCost 0.25, got %v", info.TotalCost)
	}
}

func TestStatusTool_NullCostWhenUnknown(t *testing.T) {
	mock := &mockPrompter{
		status: claudepkg.StatusInfo{
			Status:    claudepkg.ProcessStatusBusy,
			TotalCost: nil, // No cost seen
		},
	}

	tools := buildToolMap(mock)
	handler := tools["status"]

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	costVal := string(raw["total_cost_usd"])
	if costVal != "null" {
		t.Errorf("expected total_cost_usd to be null, got %s", costVal)
	}
}

func TestPromptTool_EmptyMessage(t *testing.T) {
	mock := &mockPrompter{}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message": "   ",
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty message")
	}
}

func TestPromptTool_MissingMessage(t *testing.T) {
	mock := &mockPrompter{}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing message")
	}
}

func TestPromptTool_BlockingWithRunOptions(t *testing.T) {
	mock := &mockPrompter{result: "ok"}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message":        "Do something",
		"blocking":       true,
		"session_id":     "sess-abc",
		"resume":         "sess-old",
		"continue":       true,
		"agent":          "reviewer",
		"json_schema":    `{"type":"object"}`,
		"max_budget_usd": 5.0,
		"effort":         "high",
		"fork_session":   true,
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	opts := mock.lastRunOpts
	if opts == nil {
		t.Fatal("expected RunOptions to be set")
	}
	if opts.SessionID != "sess-abc" {
		t.Errorf("expected session_id %q, got %q", "sess-abc", opts.SessionID)
	}
	if opts.Resume != "sess-old" {
		t.Errorf("expected resume %q, got %q", "sess-old", opts.Resume)
	}
	if !opts.ContinueSession {
		t.Error("expected continue to be true")
	}
	if opts.ActiveAgent != "reviewer" {
		t.Errorf("expected agent %q, got %q", "reviewer", opts.ActiveAgent)
	}
	if opts.JSONSchema != `{"type":"object"}` {
		t.Errorf("expected json_schema %q, got %q", `{"type":"object"}`, opts.JSONSchema)
	}
	if opts.MaxBudgetUSD != 5.0 {
		t.Errorf("expected max_budget_usd %f, got %f", 5.0, opts.MaxBudgetUSD)
	}
	if opts.Effort != "high" {
		t.Errorf("expected effort %q, got %q", "high", opts.Effort)
	}
	if !opts.ForkSession {
		t.Error("expected fork_session to be true")
	}
}

func TestPromptTool_InvalidEffort(t *testing.T) {
	mock := &mockPrompter{result: "ok"}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	// Invalid effort is rejected regardless of blocking mode.
	request := newCallToolRequest("prompt", map[string]any{
		"message":  "Do something",
		"blocking": true,
		"effort":   "extreme",
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid effort level")
	}
}

func TestPromptTool_InvalidParameterType(t *testing.T) {
	mock := &mockPrompter{result: "ok"}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	// session_id should be a string, not a number.
	request := newCallToolRequest("prompt", map[string]any{
		"message":    "Hello",
		"session_id": 12345,
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid parameter type")
	}
}

// --- Non-blocking prompt tests ---

func TestPromptTool_NonBlockingDefault(t *testing.T) {
	mock := &mockPrompter{
		result:    "Hello, world!",
		sessionID: "sess-123",
	}
	mock.status = claudepkg.StatusInfo{SessionID: "sess-123"}

	tools := buildToolMap(mock)
	handler := tools["prompt"]

	// Default (no blocking param) should be non-blocking.
	request := newCallToolRequest("prompt", map[string]any{
		"message": "Do something",
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	if !mock.submitCalled {
		t.Error("expected Submit() to be called for non-blocking mode")
	}
	if mock.lastPrompt != "Do something" {
		t.Errorf("expected prompt %q, got %q", "Do something", mock.lastPrompt)
	}

	// Parse response -- should be {status: "started", session_id: "..."}.
	text := extractText(t, result)
	var resp struct {
		Status    string `json:"status"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v (text: %s)", err, text)
	}
	if resp.Status != "started" {
		t.Errorf("expected status %q, got %q", "started", resp.Status)
	}
	if resp.SessionID != "sess-123" {
		t.Errorf("expected session_id %q, got %q", "sess-123", resp.SessionID)
	}
}

func TestPromptTool_NonBlockingExplicit(t *testing.T) {
	mock := &mockPrompter{result: "ok"}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message":  "Do something",
		"blocking": false,
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if !mock.submitCalled {
		t.Error("expected Submit() to be called for blocking=false")
	}
}

func TestPromptTool_NonBlockingWithRunOptions(t *testing.T) {
	mock := &mockPrompter{result: "ok"}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message":    "Do something",
		"session_id": "sess-abc",
		"effort":     "high",
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	if !mock.submitCalled {
		t.Error("expected Submit() to be called")
	}
	opts := mock.lastRunOpts
	if opts == nil {
		t.Fatal("expected RunOptions to be set")
	}
	if opts.SessionID != "sess-abc" {
		t.Errorf("expected session_id %q, got %q", "sess-abc", opts.SessionID)
	}
	if opts.Effort != "high" {
		t.Errorf("expected effort %q, got %q", "high", opts.Effort)
	}
}

func TestPromptTool_NonBlockingRunError(t *testing.T) {
	mock := &mockPrompter{
		runErr: fmt.Errorf("subprocess crashed"),
	}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message": "Hello",
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when Submit fails")
	}
}

func TestPromptTool_BlockingRunError(t *testing.T) {
	mock := &mockPrompter{
		runErr: fmt.Errorf("subprocess crashed"),
	}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message":  "Hello",
		"blocking": true,
	})

	result, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when process fails")
	}
}

// --- Status tool tests ---

func TestStatusTool(t *testing.T) {
	mock := &mockPrompter{
		status: claudepkg.StatusInfo{
			Status:        claudepkg.ProcessStatusBusy,
			SessionID:     "sess-xyz",
			MessageCount:  5,
			ToolCallCount: 2,
			ToolCalls:     map[string]int{"Bash": 1, "Read": 1},
			LastMessage:   "Working on it...",
			LastToolName:  "Bash",
		},
	}

	tools := buildToolMap(mock)
	handler := tools["status"]

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	text := extractText(t, result)
	var info claudepkg.StatusInfo
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("failed to parse status JSON: %v (text: %s)", err, text)
	}

	if info.Status != claudepkg.ProcessStatusBusy {
		t.Errorf("expected status %q, got %q", claudepkg.ProcessStatusBusy, info.Status)
	}
	if info.SessionID != "sess-xyz" {
		t.Errorf("expected session_id %q, got %q", "sess-xyz", info.SessionID)
	}
	if info.MessageCount != 5 {
		t.Errorf("expected message_count 5, got %d", info.MessageCount)
	}
	if info.ToolCallCount != 2 {
		t.Errorf("expected tool_call_count 2, got %d", info.ToolCallCount)
	}
	if info.ToolCalls["Bash"] != 1 {
		t.Errorf("expected tool_calls[Bash]=1, got %d", info.ToolCalls["Bash"])
	}
	if info.ToolCalls["Read"] != 1 {
		t.Errorf("expected tool_calls[Read]=1, got %d", info.ToolCalls["Read"])
	}
}

func TestStatusTool_WithResult(t *testing.T) {
	mock := &mockPrompter{
		status: claudepkg.StatusInfo{
			Status:    claudepkg.ProcessStatusCompleted,
			SessionID: "sess-xyz",
			Result:    "Task completed successfully",
		},
	}

	tools := buildToolMap(mock)
	handler := tools["status"]

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	text := extractText(t, result)
	var info claudepkg.StatusInfo
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("failed to parse status JSON: %v (text: %s)", err, text)
	}

	if info.Result != "Task completed successfully" {
		t.Errorf("expected result %q, got %q", "Task completed successfully", info.Result)
	}
}

// --- Result tool tests ---

func TestResultTool(t *testing.T) {
	mock := &mockPrompter{
		resultDetail: claudepkg.ResultDetailInfo{
			ResultText:   "Full result text here",
			MessageCount: 5,
			ToolCalls:    map[string]int{"Bash": 3, "Read": 2},
			TotalCost:    claudepkg.Float64Ptr(0.15),
			SessionID:    "sess-123",
			Status:       claudepkg.ProcessStatusIdle,
		},
	}

	tools := buildToolMap(mock)
	handler := tools["result"]

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	text := extractText(t, result)
	var detail claudepkg.ResultDetailInfo
	if err := json.Unmarshal([]byte(text), &detail); err != nil {
		t.Fatalf("failed to parse result detail JSON: %v (text: %s)", err, text)
	}

	if detail.ResultText != "Full result text here" {
		t.Errorf("expected result_text %q, got %q", "Full result text here", detail.ResultText)
	}
	if detail.MessageCount != 5 {
		t.Errorf("expected message_count 5, got %d", detail.MessageCount)
	}
	if detail.ToolCalls["Bash"] != 3 {
		t.Errorf("expected tool_calls[Bash]=3, got %d", detail.ToolCalls["Bash"])
	}
	if detail.ToolCalls["Read"] != 2 {
		t.Errorf("expected tool_calls[Read]=2, got %d", detail.ToolCalls["Read"])
	}
	if detail.TotalCost == nil || *detail.TotalCost != 0.15 {
		t.Errorf("expected total_cost_usd 0.15, got %v", detail.TotalCost)
	}
	if detail.SessionID != "sess-123" {
		t.Errorf("expected session_id %q, got %q", "sess-123", detail.SessionID)
	}
}

func TestResultTool_EmptyResult(t *testing.T) {
	mock := &mockPrompter{
		resultDetail: claudepkg.ResultDetailInfo{
			Status: claudepkg.ProcessStatusIdle,
		},
	}

	tools := buildToolMap(mock)
	handler := tools["result"]

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	text := extractText(t, result)
	var detail claudepkg.ResultDetailInfo
	if err := json.Unmarshal([]byte(text), &detail); err != nil {
		t.Fatalf("failed to parse result detail JSON: %v (text: %s)", err, text)
	}
	if detail.ResultText != "" {
		t.Errorf("expected empty result_text, got %q", detail.ResultText)
	}
}

// --- Stop tool tests ---

func TestStopTool(t *testing.T) {
	mock := &mockPrompter{}
	tools := buildToolMap(mock)
	handler := tools["stop"]

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if !mock.stopCalled {
		t.Error("expected Stop() to be called")
	}
}

// --- Messages tool tests ---

func TestMessagesTool_WithMessages(t *testing.T) {
	mock := &mockPrompter{
		messagesInfo: claudepkg.MessagesInfo{
			Status: claudepkg.ProcessStatusBusy,
			Messages: []claudepkg.MessageSummary{
				{Role: "system", Content: "Session: sess-001"},
				{Role: "assistant", Content: "Working on it..."},
				{Role: "assistant", Content: "Using tool: Bash"},
			},
		},
	}

	tools := buildToolMap(mock)
	handler := tools["messages"]

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	text := extractText(t, result)
	var info claudepkg.MessagesInfo
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("failed to parse messages JSON: %v (text: %s)", err, text)
	}

	if info.Status != claudepkg.ProcessStatusBusy {
		t.Errorf("expected status %q, got %q", claudepkg.ProcessStatusBusy, info.Status)
	}
	if len(info.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(info.Messages))
	}
	if info.Messages[0].Role != "system" {
		t.Errorf("expected first message role %q, got %q", "system", info.Messages[0].Role)
	}
	if info.Messages[1].Content != "Working on it..." {
		t.Errorf("expected second message content %q, got %q", "Working on it...", info.Messages[1].Content)
	}
	if info.Messages[2].Content != "Using tool: Bash" {
		t.Errorf("expected third message content %q, got %q", "Using tool: Bash", info.Messages[2].Content)
	}
}

func TestMessagesTool_Empty(t *testing.T) {
	mock := &mockPrompter{
		messagesInfo: claudepkg.MessagesInfo{
			Status: claudepkg.ProcessStatusIdle,
		},
	}

	tools := buildToolMap(mock)
	handler := tools["messages"]

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	text := extractText(t, result)
	var info claudepkg.MessagesInfo
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("failed to parse messages JSON: %v (text: %s)", err, text)
	}

	if info.Status != claudepkg.ProcessStatusIdle {
		t.Errorf("expected status %q, got %q", claudepkg.ProcessStatusIdle, info.Status)
	}
	if len(info.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(info.Messages))
	}
}

// --- Progress message tests ---

func TestProgressMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      claudepkg.StreamMessage
		expected string
	}{
		{
			name: "system with session",
			msg: claudepkg.StreamMessage{
				Type:      claudepkg.MessageTypeSystem,
				SessionID: "sess-001",
			},
			expected: "Session started: sess-001",
		},
		{
			name: "assistant tool use",
			msg: claudepkg.StreamMessage{
				Type:     claudepkg.MessageTypeAssistant,
				Subtype:  claudepkg.SubtypeToolUse,
				ToolName: "Bash",
			},
			expected: "Using tool: Bash",
		},
		{
			name: "assistant text",
			msg: claudepkg.StreamMessage{
				Type:    claudepkg.MessageTypeAssistant,
				Subtype: claudepkg.SubtypeText,
				Text:    "Working on the task...",
			},
			expected: "Assistant: Working on the task...",
		},
		{
			name: "result",
			msg: claudepkg.StreamMessage{
				Type: claudepkg.MessageTypeResult,
			},
			expected: "Task completed",
		},
		{
			name: "system without session",
			msg: claudepkg.StreamMessage{
				Type: claudepkg.MessageTypeSystem,
			},
			expected: "",
		},
		{
			name: "assistant text empty",
			msg: claudepkg.StreamMessage{
				Type:    claudepkg.MessageTypeAssistant,
				Subtype: claudepkg.SubtypeText,
				Text:    "",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := progressMessage(tt.msg)
			if got != tt.expected {
				t.Errorf("progressMessage() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// --- Helper extraction tests ---

func TestOptionalString(t *testing.T) {
	t.Run("present and valid", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{"key": "value"})
		v, err := optionalString(req, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != "value" {
			t.Errorf("expected %q, got %q", "value", v)
		}
	})

	t.Run("missing", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{})
		v, err := optionalString(req, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != "" {
			t.Errorf("expected empty string, got %q", v)
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{"key": 123})
		_, err := optionalString(req, "key")
		if err == nil {
			t.Error("expected error for wrong type")
		}
	})

	t.Run("nil value", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{"key": nil})
		v, err := optionalString(req, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != "" {
			t.Errorf("expected empty string, got %q", v)
		}
	})
}

func TestOptionalBool(t *testing.T) {
	t.Run("present and true", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{"flag": true})
		v, err := optionalBool(req, "flag")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !v {
			t.Error("expected true")
		}
	})

	t.Run("missing", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{})
		v, err := optionalBool(req, "flag")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v {
			t.Error("expected false")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{"flag": "yes"})
		_, err := optionalBool(req, "flag")
		if err == nil {
			t.Error("expected error for wrong type")
		}
	})
}

func TestOptionalFloat(t *testing.T) {
	t.Run("present and valid", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{"amount": 5.5})
		v, err := optionalFloat(req, "amount")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != 5.5 {
			t.Errorf("expected 5.5, got %f", v)
		}
	})

	t.Run("missing", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{})
		v, err := optionalFloat(req, "amount")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != 0 {
			t.Errorf("expected 0, got %f", v)
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		req := newCallToolRequest("test", map[string]any{"amount": "five"})
		_, err := optionalFloat(req, "amount")
		if err == nil {
			t.Error("expected error for wrong type")
		}
	})
}

// --- Test helpers ---

// buildToolMap registers tools and returns a name->handler map.
func buildToolMap(process claudepkg.Prompter) map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	tools := map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){}

	pt := promptTool(context.Background(), process)
	tools[pt.Tool.Name] = pt.Handler

	st := statusTool(process)
	tools[st.Tool.Name] = st.Handler

	stp := stopTool(process)
	tools[stp.Tool.Name] = stp.Handler

	rt := resultTool(process)
	tools[rt.Tool.Name] = rt.Handler

	mt := messagesTool(process)
	tools[mt.Tool.Name] = mt.Handler

	return tools
}

// newCallToolRequest builds a CallToolRequest with the given arguments.
func newCallToolRequest(name string, args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}
}

type promptResponse struct {
	Result       string                `json:"result"`
	MessageCount int                   `json:"message_count"`
	TotalCost    *float64              `json:"total_cost_usd"`
	TokenUsage   *claudepkg.TokenUsage `json:"token_usage"`
	SessionID    string                `json:"session_id"`
}

func parsePromptResponse(t *testing.T, result *mcp.CallToolResult) promptResponse {
	t.Helper()
	text := extractText(t, result)
	var resp promptResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse prompt response: %v (text: %s)", err, text)
	}
	return resp
}

func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

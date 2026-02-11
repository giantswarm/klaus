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

func (m *mockPrompter) Status() claudepkg.StatusInfo { return m.status }

func (m *mockPrompter) Stop() error {
	m.stopCalled = true
	return nil
}

func (m *mockPrompter) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (m *mockPrompter) MarshalStatus() ([]byte, error) {
	return json.Marshal(m.status)
}

// --- Prompt tool tests ---

func TestPromptTool_BasicPrompt(t *testing.T) {
	mock := &mockPrompter{
		result:    "Hello, world!",
		sessionID: "sess-123",
		totalCost: 0.05,
	}

	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message": "Say hello",
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

	// Parse the JSON result.
	resp := parsePromptResponse(t, result)
	if resp.Result != "Hello, world!" {
		t.Errorf("expected result %q, got %q", "Hello, world!", resp.Result)
	}
	if resp.SessionID != "sess-123" {
		t.Errorf("expected session_id %q, got %q", "sess-123", resp.SessionID)
	}
	if resp.TotalCost != 0.05 {
		t.Errorf("expected total_cost_usd %f, got %f", 0.05, resp.TotalCost)
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

func TestPromptTool_WithRunOptions(t *testing.T) {
	mock := &mockPrompter{result: "ok"}
	tools := buildToolMap(mock)
	handler := tools["prompt"]

	request := newCallToolRequest("prompt", map[string]any{
		"message":        "Do something",
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

	request := newCallToolRequest("prompt", map[string]any{
		"message": "Do something",
		"effort":  "extreme",
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

func TestPromptTool_RunError(t *testing.T) {
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

	pt := promptTool(process)
	tools[pt.Tool.Name] = pt.Handler

	st := statusTool(process)
	tools[st.Tool.Name] = st.Handler

	stp := stopTool(process)
	tools[stp.Tool.Name] = stp.Handler

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
	Result       string  `json:"result"`
	MessageCount int     `json:"message_count"`
	TotalCost    float64 `json:"total_cost_usd"`
	SessionID    string  `json:"session_id"`
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

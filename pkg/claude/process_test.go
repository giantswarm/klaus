package claude

import (
	"encoding/json"
	"testing"
)

func TestNewProcess_InitialState(t *testing.T) {
	process := NewProcess(DefaultOptions())

	status := process.Status()
	if status.Status != ProcessStatusIdle {
		t.Errorf("expected initial status %q, got %q", ProcessStatusIdle, status.Status)
	}
	if status.SessionID != "" {
		t.Errorf("expected empty session ID, got %q", status.SessionID)
	}
	if status.ErrorMessage != "" {
		t.Errorf("expected empty error message, got %q", status.ErrorMessage)
	}
	if status.MessageCount != 0 {
		t.Errorf("expected 0 message count, got %d", status.MessageCount)
	}
	if status.ToolCallCount != 0 {
		t.Errorf("expected 0 tool call count, got %d", status.ToolCallCount)
	}
	if status.ToolCalls != nil {
		t.Errorf("expected nil tool calls, got %v", status.ToolCalls)
	}
	if status.LastMessage != "" {
		t.Errorf("expected empty last message, got %q", status.LastMessage)
	}
	if status.LastToolName != "" {
		t.Errorf("expected empty last tool name, got %q", status.LastToolName)
	}
}

func TestNewProcess_DonePreClosed(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Done channel should be immediately readable (pre-closed).
	select {
	case <-process.Done():
		// Expected.
	default:
		t.Error("expected Done() to be immediately readable for new process")
	}
}

func TestProcess_MarshalStatus(t *testing.T) {
	process := NewProcess(DefaultOptions())

	data, err := process.MarshalStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var info StatusInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}

	if info.Status != ProcessStatusIdle {
		t.Errorf("expected status %q, got %q", ProcessStatusIdle, info.Status)
	}
}

func TestProcess_ImplementsPrompter(t *testing.T) {
	// Compile-time check that Process implements Prompter.
	var _ Prompter = (*Process)(nil)
}

func TestProcess_ResultDetail_InitialState(t *testing.T) {
	process := NewProcess(DefaultOptions())
	detail := process.ResultDetail()

	if detail.ResultText != "" {
		t.Errorf("expected empty result text, got %q", detail.ResultText)
	}
	if detail.Messages != nil {
		t.Errorf("expected nil messages, got %v", detail.Messages)
	}
	if detail.MessageCount != 0 {
		t.Errorf("expected 0 message count, got %d", detail.MessageCount)
	}
	if detail.Status != ProcessStatusIdle {
		t.Errorf("expected status %q, got %q", ProcessStatusIdle, detail.Status)
	}
}

func TestProcess_ResultDetail_MessageCountWhileBusy(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Simulate a busy process that has received messages but has no
	// completed result yet (result.messages is empty).
	process.mu.Lock()
	process.status = ProcessStatusBusy
	process.messageCount = 7
	process.mu.Unlock()

	detail := process.ResultDetail()
	if detail.MessageCount != 7 {
		t.Errorf("expected MessageCount 7 from live counter while busy, got %d", detail.MessageCount)
	}
	if detail.Status != ProcessStatusBusy {
		t.Errorf("expected status %q, got %q", ProcessStatusBusy, detail.Status)
	}
}

func TestProcess_StatusNoResultWhenBusy(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Simulate a process that has a stored result from a previous run
	// but is currently busy.
	process.mu.Lock()
	process.result = resultState{text: "old result", completed: true}
	process.status = ProcessStatusBusy
	process.mu.Unlock()

	status := process.Status()
	if status.Result != "" {
		t.Errorf("expected empty result when busy, got %q", status.Result)
	}
}

func TestProcess_StatusNoResultWhenIdle(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// idle with no completed result should not show result.
	status := process.Status()
	if status.Result != "" {
		t.Errorf("expected empty result when idle, got %q", status.Result)
	}
}

func TestProcess_StatusShowsResultWhenCompleted(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Simulate a completed Submit run.
	process.mu.Lock()
	process.result = resultState{text: "completed result", completed: true}
	process.status = ProcessStatusCompleted
	process.mu.Unlock()

	status := process.Status()
	if status.Status != ProcessStatusCompleted {
		t.Errorf("expected status %q, got %q", ProcessStatusCompleted, status.Status)
	}
	if status.Result != "completed result" {
		t.Errorf("expected result %q, got %q", "completed result", status.Result)
	}
}

func TestProcess_StatusTruncatesLongResult(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Create a result longer than maxStatusResultLen.
	long := make([]rune, maxStatusResultLen+100)
	for i := range long {
		long[i] = 'x'
	}
	process.mu.Lock()
	process.result = resultState{text: string(long), completed: true}
	process.status = ProcessStatusCompleted
	process.mu.Unlock()

	status := process.Status()
	// Truncated result should be maxStatusResultLen runes + "...".
	expected := string(long[:maxStatusResultLen]) + "..."
	if status.Result != expected {
		t.Errorf("expected truncated result of length %d, got length %d",
			len([]rune(expected)), len([]rune(status.Result)))
	}

	// ResultDetail should return the full untruncated text.
	detail := process.ResultDetail()
	if detail.ResultText != string(long) {
		t.Error("expected ResultDetail to return full untruncated text")
	}
}

func TestProcess_StatusToolCalls(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Simulate tool calls being recorded during a run.
	process.mu.Lock()
	process.status = ProcessStatusBusy
	process.toolCallCount = 5
	process.toolCalls = map[string]int{
		"Bash": 3,
		"Read": 2,
	}
	process.mu.Unlock()

	status := process.Status()
	if status.ToolCallCount != 5 {
		t.Errorf("expected tool_call_count 5, got %d", status.ToolCallCount)
	}
	if len(status.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool entries, got %d", len(status.ToolCalls))
	}
	if status.ToolCalls["Bash"] != 3 {
		t.Errorf("expected Bash count 3, got %d", status.ToolCalls["Bash"])
	}
	if status.ToolCalls["Read"] != 2 {
		t.Errorf("expected Read count 2, got %d", status.ToolCalls["Read"])
	}

	// Verify the returned map is a copy (not a reference to internal state).
	status.ToolCalls["Bash"] = 999
	status2 := process.Status()
	if status2.ToolCalls["Bash"] != 3 {
		t.Errorf("expected internal map to be unchanged, got Bash=%d", status2.ToolCalls["Bash"])
	}
}

func TestProcess_ResultDetailToolCalls(t *testing.T) {
	process := NewProcess(DefaultOptions())

	process.mu.Lock()
	process.status = ProcessStatusCompleted
	process.result = resultState{text: "done", completed: true}
	process.toolCalls = map[string]int{
		"Glob":      2,
		"TodoWrite": 1,
	}
	process.mu.Unlock()

	detail := process.ResultDetail()
	if len(detail.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool entries, got %d", len(detail.ToolCalls))
	}
	if detail.ToolCalls["Glob"] != 2 {
		t.Errorf("expected Glob count 2, got %d", detail.ToolCalls["Glob"])
	}
	if detail.ToolCalls["TodoWrite"] != 1 {
		t.Errorf("expected TodoWrite count 1, got %d", detail.ToolCalls["TodoWrite"])
	}
}

func TestProcess_ToolCallsInJSON(t *testing.T) {
	process := NewProcess(DefaultOptions())

	process.mu.Lock()
	process.status = ProcessStatusBusy
	process.toolCallCount = 3
	process.toolCalls = map[string]int{
		"Bash": 2,
		"Read": 1,
	}
	process.mu.Unlock()

	data, err := process.MarshalStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var info StatusInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}

	if info.ToolCalls["Bash"] != 2 {
		t.Errorf("expected Bash=2 in JSON, got %d", info.ToolCalls["Bash"])
	}
	if info.ToolCalls["Read"] != 1 {
		t.Errorf("expected Read=1 in JSON, got %d", info.ToolCalls["Read"])
	}
}

func TestProcess_ToolCallsOmittedWhenNil(t *testing.T) {
	process := NewProcess(DefaultOptions())

	data, err := process.MarshalStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// tool_calls should be omitted from JSON when nil.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if _, exists := raw["tool_calls"]; exists {
		t.Error("expected tool_calls to be omitted from JSON when nil")
	}
}

func TestProcess_TokenUsageAggregation(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Simulate token usage being aggregated during a run.
	process.mu.Lock()
	process.status = ProcessStatusBusy
	process.tokenUsage = TokenUsage{
		InputTokens:              15000,
		OutputTokens:             2500,
		CacheCreationInputTokens: 5000,
		CacheReadInputTokens:     8000,
	}
	process.mu.Unlock()

	status := process.Status()
	if status.TokenUsage == nil {
		t.Fatal("expected non-nil TokenUsage")
	}
	if status.TokenUsage.InputTokens != 15000 {
		t.Errorf("expected InputTokens 15000, got %d", status.TokenUsage.InputTokens)
	}
	if status.TokenUsage.OutputTokens != 2500 {
		t.Errorf("expected OutputTokens 2500, got %d", status.TokenUsage.OutputTokens)
	}
	if status.TokenUsage.CacheCreationInputTokens != 5000 {
		t.Errorf("expected CacheCreationInputTokens 5000, got %d", status.TokenUsage.CacheCreationInputTokens)
	}
	if status.TokenUsage.CacheReadInputTokens != 8000 {
		t.Errorf("expected CacheReadInputTokens 8000, got %d", status.TokenUsage.CacheReadInputTokens)
	}

	// Verify TokenUsage is a copy (not a reference to internal state).
	status.TokenUsage.InputTokens = 999
	status2 := process.Status()
	if status2.TokenUsage.InputTokens != 15000 {
		t.Errorf("expected internal TokenUsage to be unchanged, got InputTokens=%d", status2.TokenUsage.InputTokens)
	}
}

func TestProcess_TokenUsageOmittedWhenZero(t *testing.T) {
	process := NewProcess(DefaultOptions())

	status := process.Status()
	if status.TokenUsage != nil {
		t.Errorf("expected nil TokenUsage for fresh process, got %v", status.TokenUsage)
	}
}

func TestProcess_CostNilWhenNeverSeen(t *testing.T) {
	process := NewProcess(DefaultOptions())

	status := process.Status()
	if status.TotalCost != nil {
		t.Errorf("expected nil TotalCost when no cost observed, got %v", status.TotalCost)
	}
}

func TestProcess_CostSetWhenSeen(t *testing.T) {
	process := NewProcess(DefaultOptions())

	process.mu.Lock()
	process.totalCost = 0.42
	process.costSeen = true
	process.mu.Unlock()

	status := process.Status()
	if status.TotalCost == nil {
		t.Fatal("expected non-nil TotalCost when cost observed")
	}
	if *status.TotalCost != 0.42 {
		t.Errorf("expected TotalCost 0.42, got %f", *status.TotalCost)
	}
}

func TestProcess_ResultDetailTokenUsage(t *testing.T) {
	process := NewProcess(DefaultOptions())

	process.mu.Lock()
	process.status = ProcessStatusCompleted
	process.result = resultState{text: "done", completed: true}
	process.tokenUsage = TokenUsage{
		InputTokens:  500,
		OutputTokens: 100,
	}
	process.totalCost = 0.05
	process.costSeen = true
	process.mu.Unlock()

	detail := process.ResultDetail()
	if detail.TokenUsage == nil {
		t.Fatal("expected non-nil TokenUsage in ResultDetail")
	}
	if detail.TokenUsage.InputTokens != 500 {
		t.Errorf("expected InputTokens 500, got %d", detail.TokenUsage.InputTokens)
	}
	if detail.TotalCost == nil || *detail.TotalCost != 0.05 {
		t.Errorf("expected TotalCost 0.05, got %v", detail.TotalCost)
	}
}

func TestProcess_Messages_Idle(t *testing.T) {
	process := NewProcess(DefaultOptions())

	info := process.Messages()
	if info.Status != ProcessStatusIdle {
		t.Errorf("expected status %q, got %q", ProcessStatusIdle, info.Status)
	}
	if len(info.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(info.Messages))
	}
}

func TestProcess_Messages_Busy(t *testing.T) {
	process := NewProcess(DefaultOptions())

	process.mu.Lock()
	process.status = ProcessStatusBusy
	process.liveMessages = []StreamMessage{
		{Type: MessageTypeSystem, SessionID: "sess-1"},
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "thinking..."},
		{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Bash"},
	}
	process.mu.Unlock()

	info := process.Messages()
	if info.Status != ProcessStatusBusy {
		t.Errorf("expected status %q, got %q", ProcessStatusBusy, info.Status)
	}
	if len(info.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(info.Messages))
	}
	if info.Messages[0].Role != "system" {
		t.Errorf("expected role %q, got %q", "system", info.Messages[0].Role)
	}
	if info.Messages[0].Content != "Session: sess-1" {
		t.Errorf("expected content %q, got %q", "Session: sess-1", info.Messages[0].Content)
	}
	if info.Messages[1].Content != "thinking..." {
		t.Errorf("expected content %q, got %q", "thinking...", info.Messages[1].Content)
	}
	if info.Messages[2].Content != "Using tool: Bash" {
		t.Errorf("expected content %q, got %q", "Using tool: Bash", info.Messages[2].Content)
	}
}

func TestProcess_Messages_Completed(t *testing.T) {
	process := NewProcess(DefaultOptions())

	process.mu.Lock()
	process.status = ProcessStatusCompleted
	process.result = resultState{
		text:      "done",
		completed: true,
		messages: []StreamMessage{
			{Type: MessageTypeSystem, SessionID: "sess-1"},
			{Type: MessageTypeResult, Result: "final answer"},
		},
	}
	process.mu.Unlock()

	info := process.Messages()
	if info.Status != ProcessStatusCompleted {
		t.Errorf("expected status %q, got %q", ProcessStatusCompleted, info.Status)
	}
	if len(info.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(info.Messages))
	}
	if info.Messages[1].Role != "result" {
		t.Errorf("expected role %q, got %q", "result", info.Messages[1].Role)
	}
	if info.Messages[1].Content != "final answer" {
		t.Errorf("expected content %q, got %q", "final answer", info.Messages[1].Content)
	}
}

func TestSummarizeMessages(t *testing.T) {
	msgs := []StreamMessage{
		{Type: MessageTypeSystem, SessionID: "sess-1"},
		{Type: MessageTypeSystem}, // no session ID, should be skipped
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "hello"},
		{Type: MessageTypeAssistant, Subtype: SubtypeText}, // empty text, should be skipped
		{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Read"},
		{Type: MessageTypeResult, Result: "done"},
		{Type: MessageTypeResult}, // empty result, should be skipped
	}

	summaries := SummarizeMessages(msgs)
	if len(summaries) != 4 {
		t.Fatalf("expected 4 summaries, got %d: %+v", len(summaries), summaries)
	}
	if summaries[0].Role != "system" || summaries[0].Content != "Session: sess-1" {
		t.Errorf("unexpected summary[0]: %+v", summaries[0])
	}
	if summaries[1].Role != "assistant" || summaries[1].Content != "hello" {
		t.Errorf("unexpected summary[1]: %+v", summaries[1])
	}
	if summaries[2].Role != "assistant" || summaries[2].Content != "Using tool: Read" {
		t.Errorf("unexpected summary[2]: %+v", summaries[2])
	}
	if summaries[3].Role != "result" || summaries[3].Content != "done" {
		t.Errorf("unexpected summary[3]: %+v", summaries[3])
	}
}

func TestProcess_StopWhenNotRunning(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Stop on an idle process should be a no-op.
	if err := process.Stop(); err != nil {
		t.Errorf("unexpected error stopping idle process: %v", err)
	}
}

func TestProcess_MergedOpts(t *testing.T) {
	base := Options{
		Model:          "sonnet",
		PermissionMode: "bypassPermissions",
		MaxBudgetUSD:   5.0,
		ActiveAgent:    "base-agent",
		JSONSchema:     `{"type":"object"}`,
	}
	process := NewProcess(base)

	t.Run("nil RunOptions returns base opts unchanged", func(t *testing.T) {
		merged := process.mergedOpts(nil)
		if merged.Model != "sonnet" {
			t.Errorf("expected model %q, got %q", "sonnet", merged.Model)
		}
		if merged.ActiveAgent != "base-agent" {
			t.Errorf("expected active agent %q, got %q", "base-agent", merged.ActiveAgent)
		}
		if merged.JSONSchema != `{"type":"object"}` {
			t.Errorf("expected base json schema, got %q", merged.JSONSchema)
		}
	})

	t.Run("RunOptions overrides specific fields", func(t *testing.T) {
		runOpts := &RunOptions{
			ActiveAgent:  "reviewer",
			MaxBudgetUSD: 10.0,
			Effort:       "high",
			SessionID:    "sess-123",
		}
		merged := process.mergedOpts(runOpts)
		if merged.ActiveAgent != "reviewer" {
			t.Errorf("expected active agent %q, got %q", "reviewer", merged.ActiveAgent)
		}
		if merged.MaxBudgetUSD != 10.0 {
			t.Errorf("expected max budget 10.0, got %f", merged.MaxBudgetUSD)
		}
		if merged.Effort != "high" {
			t.Errorf("expected effort %q, got %q", "high", merged.Effort)
		}
		if merged.SessionID != "sess-123" {
			t.Errorf("expected session ID %q, got %q", "sess-123", merged.SessionID)
		}
		// Base field should remain.
		if merged.Model != "sonnet" {
			t.Errorf("expected model %q (from base), got %q", "sonnet", merged.Model)
		}
	})

	t.Run("Resume overrides base", func(t *testing.T) {
		runOpts := &RunOptions{Resume: "sess-456"}
		merged := process.mergedOpts(runOpts)
		if merged.Resume != "sess-456" {
			t.Errorf("expected resume %q, got %q", "sess-456", merged.Resume)
		}
	})

	t.Run("ContinueSession overrides base", func(t *testing.T) {
		runOpts := &RunOptions{ContinueSession: true}
		merged := process.mergedOpts(runOpts)
		if !merged.ContinueSession {
			t.Error("expected ContinueSession to be true")
		}
	})

	t.Run("ForkSession overrides base", func(t *testing.T) {
		runOpts := &RunOptions{ForkSession: true}
		merged := process.mergedOpts(runOpts)
		if !merged.ForkSession {
			t.Error("expected ForkSession to be true")
		}
	})

	t.Run("JSONSchema overrides base", func(t *testing.T) {
		runOpts := &RunOptions{JSONSchema: `{"type":"array"}`}
		merged := process.mergedOpts(runOpts)
		if merged.JSONSchema != `{"type":"array"}` {
			t.Errorf("expected json schema %q, got %q", `{"type":"array"}`, merged.JSONSchema)
		}
	})

	t.Run("zero-value RunOptions fields do not override base", func(t *testing.T) {
		runOpts := &RunOptions{} // All zero values.
		merged := process.mergedOpts(runOpts)
		if merged.MaxBudgetUSD != 5.0 {
			t.Errorf("expected base max budget 5.0, got %f", merged.MaxBudgetUSD)
		}
		if merged.ActiveAgent != "base-agent" {
			t.Errorf("expected base active agent %q, got %q", "base-agent", merged.ActiveAgent)
		}
		if merged.JSONSchema != `{"type":"object"}` {
			t.Errorf("expected base json schema, got %q", merged.JSONSchema)
		}
		if merged.ContinueSession {
			t.Error("expected ContinueSession to remain false")
		}
		if merged.ForkSession {
			t.Error("expected ForkSession to remain false")
		}
	})
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "abc", 3, "abc"},
		{"truncated", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},
		{"one over", "abcd", 3, "abc..."},
		{"multi-byte utf8 no truncate", "Hello \u4e16\u754c!", 9, "Hello \u4e16\u754c!"},
		{"truncate multi-byte", "Hello \u4e16\u754c\u4eba\u6c11", 8, "Hello \u4e16\u754c..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestCollectResultText(t *testing.T) {
	t.Run("returns result from result message", func(t *testing.T) {
		messages := []StreamMessage{
			{Type: MessageTypeSystem, SessionID: "sess-1"},
			{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "thinking..."},
			{Type: MessageTypeResult, Result: "final answer"},
		}
		got := CollectResultText(messages)
		if got != "final answer" {
			t.Errorf("expected %q, got %q", "final answer", got)
		}
	})

	t.Run("falls back to assistant text", func(t *testing.T) {
		messages := []StreamMessage{
			{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "part 1 "},
			{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "part 2"},
			{Type: MessageTypeResult},
		}
		got := CollectResultText(messages)
		if got != "part 1 part 2" {
			t.Errorf("expected %q, got %q", "part 1 part 2", got)
		}
	})

	t.Run("empty messages returns empty", func(t *testing.T) {
		got := CollectResultText(nil)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

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

func TestProcess_StatusNoResultWhenBusy(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Simulate a process that has a stored result from a previous run
	// but is currently busy.
	process.mu.Lock()
	process.result.text = "old result"
	process.status = ProcessStatusBusy
	process.mu.Unlock()

	status := process.Status()
	if status.Result != "" {
		t.Errorf("expected empty result when busy, got %q", status.Result)
	}
}

func TestProcess_StatusShowsResultWhenIdle(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Simulate a completed Submit run that stored a result.
	process.mu.Lock()
	process.result.text = "completed result"
	process.status = ProcessStatusIdle
	process.mu.Unlock()

	status := process.Status()
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
	process.result.text = string(long)
	process.status = ProcessStatusIdle
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

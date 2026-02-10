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
	}
	process := NewProcess(base)

	// Nil RunOptions should return base opts unchanged.
	merged := process.mergedOpts(nil)
	if merged.Model != "sonnet" {
		t.Errorf("expected model %q, got %q", "sonnet", merged.Model)
	}
	if merged.ActiveAgent != "base-agent" {
		t.Errorf("expected active agent %q, got %q", "base-agent", merged.ActiveAgent)
	}

	// RunOptions should override specific fields.
	runOpts := &RunOptions{
		ActiveAgent:  "reviewer",
		MaxBudgetUSD: 10.0,
		Effort:       "high",
		SessionID:    "sess-123",
	}
	merged = process.mergedOpts(runOpts)
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
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc..."},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

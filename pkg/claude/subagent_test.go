package claude

import (
	"encoding/json"
	"testing"
)

func TestIsSubagentTool(t *testing.T) {
	tests := []struct {
		name string
		tool string
		want bool
	}{
		{"Task tool", "Task", true},
		{"Agent tool", "Agent", true},
		{"Bash tool", "Bash", false},
		{"Read tool", "Read", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSubagentTool(tt.tool); got != tt.want {
				t.Errorf("isSubagentTool(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}

func TestSubagentTracker_HandleToolUse(t *testing.T) {
	t.Run("records Task tool_use as inflight subagent", func(t *testing.T) {
		tracker := newSubagentTracker()

		args, _ := json.Marshal(taskToolArgs{
			SubagentType: "Explore",
			Description:  "Explore codebase structure",
		})

		msg := StreamMessage{
			Type:     MessageTypeAssistant,
			Subtype:  SubtypeToolUse,
			ToolName: "Task",
			ToolID:   "tool-1",
			ToolArgs: args,
		}

		ok := tracker.handleToolUse(msg)
		if !ok {
			t.Error("expected handleToolUse to return true for Task tool")
		}

		calls := tracker.calls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].Type != "Explore" {
			t.Errorf("expected type %q, got %q", "Explore", calls[0].Type)
		}
		if calls[0].Description != "Explore codebase structure" {
			t.Errorf("expected description %q, got %q", "Explore codebase structure", calls[0].Description)
		}
		if calls[0].Status != "running" {
			t.Errorf("expected status %q, got %q", "running", calls[0].Status)
		}
		if calls[0].ToolID != "tool-1" {
			t.Errorf("expected tool_id %q, got %q", "tool-1", calls[0].ToolID)
		}
	})

	t.Run("ignores non-subagent tools", func(t *testing.T) {
		tracker := newSubagentTracker()

		msg := StreamMessage{
			Type:     MessageTypeAssistant,
			Subtype:  SubtypeToolUse,
			ToolName: "Bash",
			ToolID:   "tool-2",
		}

		ok := tracker.handleToolUse(msg)
		if ok {
			t.Error("expected handleToolUse to return false for Bash tool")
		}
		if len(tracker.calls()) != 0 {
			t.Error("expected no calls tracked")
		}
	})

	t.Run("handles Agent tool", func(t *testing.T) {
		tracker := newSubagentTracker()

		args, _ := json.Marshal(taskToolArgs{
			SubagentType: "general-purpose",
			Description:  "Research topic",
		})

		msg := StreamMessage{
			Type:     MessageTypeAssistant,
			Subtype:  SubtypeToolUse,
			ToolName: "Agent",
			ToolID:   "tool-3",
			ToolArgs: args,
		}

		ok := tracker.handleToolUse(msg)
		if !ok {
			t.Error("expected handleToolUse to return true for Agent tool")
		}

		calls := tracker.calls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].Type != "general-purpose" {
			t.Errorf("expected type %q, got %q", "general-purpose", calls[0].Type)
		}
	})

	t.Run("falls back to tool name when subagent_type is empty", func(t *testing.T) {
		tracker := newSubagentTracker()

		msg := StreamMessage{
			Type:     MessageTypeAssistant,
			Subtype:  SubtypeToolUse,
			ToolName: "Task",
			ToolID:   "tool-4",
			ToolArgs: json.RawMessage(`{}`),
		}

		tracker.handleToolUse(msg)
		calls := tracker.calls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].Type != "Task" {
			t.Errorf("expected type %q, got %q", "Task", calls[0].Type)
		}
	})
}

func TestSubagentTracker_HandleMessage_WithUsageBlock(t *testing.T) {
	tracker := newSubagentTracker()

	// First, record an inflight subagent.
	args, _ := json.Marshal(taskToolArgs{
		SubagentType: "Explore",
		Description:  "Explore codebase",
	})
	tracker.handleToolUse(StreamMessage{
		Type:     MessageTypeAssistant,
		Subtype:  SubtypeToolUse,
		ToolName: "Task",
		ToolID:   "tool-1",
		ToolArgs: args,
	})

	// Simulate a message containing the <usage> block.
	usageText := `Some result text
<usage>
total_tokens: 36568
tool_uses: 29
duration_ms: 43095
</usage>
`

	ok := tracker.handleMessage(StreamMessage{
		Type: MessageTypeAssistant,
		Text: usageText,
	})
	if !ok {
		t.Error("expected handleMessage to return true when usage block found")
	}

	calls := tracker.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 completed call, got %d", len(calls))
	}

	call := calls[0]
	if call.Status != "completed" {
		t.Errorf("expected status %q, got %q", "completed", call.Status)
	}
	if call.Tokens != 36568 {
		t.Errorf("expected tokens 36568, got %d", call.Tokens)
	}
	if call.ToolCalls != 29 {
		t.Errorf("expected tool_calls 29, got %d", call.ToolCalls)
	}
	if call.DurationMS != 43095 {
		t.Errorf("expected duration_ms 43095, got %f", call.DurationMS)
	}
	if call.Type != "Explore" {
		t.Errorf("expected type %q, got %q", "Explore", call.Type)
	}

	// Inflight should be empty now.
	if len(tracker.inflight) != 0 {
		t.Errorf("expected 0 inflight, got %d", len(tracker.inflight))
	}
}

func TestSubagentTracker_HandleMessage_NoInflight(t *testing.T) {
	tracker := newSubagentTracker()

	ok := tracker.handleMessage(StreamMessage{
		Type: MessageTypeAssistant,
		Text: "<usage>\ntotal_tokens: 100\ntool_uses: 5\nduration_ms: 1000\n</usage>",
	})
	if ok {
		t.Error("expected handleMessage to return false when no inflight subagents")
	}
}

func TestSubagentTracker_HandleMessage_NoUsageBlock(t *testing.T) {
	tracker := newSubagentTracker()

	// Add an inflight subagent.
	tracker.handleToolUse(StreamMessage{
		Type:     MessageTypeAssistant,
		Subtype:  SubtypeToolUse,
		ToolName: "Task",
		ToolID:   "tool-1",
	})

	// Message without usage block should not complete the subagent.
	ok := tracker.handleMessage(StreamMessage{
		Type: MessageTypeAssistant,
		Text: "Just some regular text",
	})
	if ok {
		t.Error("expected handleMessage to return false for message without usage block")
	}

	// Still inflight.
	calls := tracker.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Status != "running" {
		t.Errorf("expected status %q, got %q", "running", calls[0].Status)
	}
}

func TestSubagentTracker_MultipleSubagents(t *testing.T) {
	tracker := newSubagentTracker()

	// Dispatch two subagents.
	args1, _ := json.Marshal(taskToolArgs{SubagentType: "Explore", Description: "First"})
	args2, _ := json.Marshal(taskToolArgs{SubagentType: "general-purpose", Description: "Second"})

	tracker.handleToolUse(StreamMessage{
		Type: MessageTypeAssistant, Subtype: SubtypeToolUse,
		ToolName: "Task", ToolID: "tool-1", ToolArgs: args1,
	})
	tracker.handleToolUse(StreamMessage{
		Type: MessageTypeAssistant, Subtype: SubtypeToolUse,
		ToolName: "Agent", ToolID: "tool-2", ToolArgs: args2,
	})

	if len(tracker.calls()) != 2 {
		t.Fatalf("expected 2 inflight calls, got %d", len(tracker.calls()))
	}

	// Complete the first one via usage block.
	tracker.handleMessage(StreamMessage{
		Type: MessageTypeAssistant,
		Text: "<usage>\ntotal_tokens: 1000\ntool_uses: 10\nduration_ms: 5000\n</usage>",
	})

	// One completed, one still inflight.
	calls := tracker.calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 total calls, got %d", len(calls))
	}

	completedCount := 0
	runningCount := 0
	for _, c := range calls {
		switch c.Status {
		case "completed":
			completedCount++
		case "running":
			runningCount++
		}
	}
	if completedCount != 1 {
		t.Errorf("expected 1 completed, got %d", completedCount)
	}
	if runningCount != 1 {
		t.Errorf("expected 1 running, got %d", runningCount)
	}
}

func TestSubagentTracker_Reset(t *testing.T) {
	tracker := newSubagentTracker()

	tracker.handleToolUse(StreamMessage{
		Type: MessageTypeAssistant, Subtype: SubtypeToolUse,
		ToolName: "Task", ToolID: "tool-1",
	})

	tracker.reset()

	if len(tracker.calls()) != 0 {
		t.Error("expected 0 calls after reset")
	}
	if len(tracker.inflight) != 0 {
		t.Error("expected 0 inflight after reset")
	}
	if len(tracker.completed) != 0 {
		t.Error("expected 0 completed after reset")
	}
}

func TestCopySubagentCalls(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		if got := copySubagentCalls(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("empty returns nil", func(t *testing.T) {
		if got := copySubagentCalls([]SubagentCall{}); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("returns independent copy", func(t *testing.T) {
		original := []SubagentCall{
			{Type: "Explore", Description: "test", Tokens: 100},
		}
		cp := copySubagentCalls(original)
		cp[0].Tokens = 999
		if original[0].Tokens != 100 {
			t.Errorf("expected original unchanged, got Tokens=%d", original[0].Tokens)
		}
	})
}

func TestCollectSubagentCalls(t *testing.T) {
	args, _ := json.Marshal(taskToolArgs{
		SubagentType: "Explore",
		Description:  "Explore codebase",
	})

	messages := []StreamMessage{
		{Type: MessageTypeSystem, SessionID: "sess-1"},
		{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Task", ToolID: "t1", ToolArgs: args},
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "<usage>\ntotal_tokens: 5000\ntool_uses: 15\nduration_ms: 10000\n</usage>"},
		{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Bash", ToolID: "t2"},
		{Type: MessageTypeResult, Result: "done"},
	}

	calls := collectSubagentCalls(messages)
	if len(calls) != 1 {
		t.Fatalf("expected 1 subagent call, got %d", len(calls))
	}
	if calls[0].Type != "Explore" {
		t.Errorf("expected type %q, got %q", "Explore", calls[0].Type)
	}
	if calls[0].Tokens != 5000 {
		t.Errorf("expected tokens 5000, got %d", calls[0].Tokens)
	}
	if calls[0].ToolCalls != 15 {
		t.Errorf("expected tool_calls 15, got %d", calls[0].ToolCalls)
	}
}

func TestCollectSubagentCalls_NoSubagents(t *testing.T) {
	messages := []StreamMessage{
		{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Bash", ToolID: "t1"},
		{Type: MessageTypeResult, Result: "done"},
	}

	calls := collectSubagentCalls(messages)
	if calls != nil {
		t.Errorf("expected nil, got %v", calls)
	}
}

func TestUsageBlockRegex(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		tokens  string
		tools   string
		durMs   string
		matches bool
	}{
		{
			name:    "standard format",
			input:   "<usage>\ntotal_tokens: 36568\ntool_uses: 29\nduration_ms: 43095\n</usage>",
			tokens:  "36568",
			tools:   "29",
			durMs:   "43095",
			matches: true,
		},
		{
			name:    "embedded in text",
			input:   "Result\n<usage>\ntotal_tokens: 100\ntool_uses: 5\nduration_ms: 2000\n</usage>\nMore text",
			tokens:  "100",
			tools:   "5",
			durMs:   "2000",
			matches: true,
		},
		{
			name:    "no usage block",
			input:   "just regular text",
			matches: false,
		},
		{
			name:    "partial usage block",
			input:   "<usage>\ntotal_tokens: 100\n</usage>",
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := usageBlockRe.FindStringSubmatch(tt.input)
			if tt.matches {
				if len(matches) != 4 {
					t.Fatalf("expected 4 matches, got %d", len(matches))
				}
				if matches[1] != tt.tokens {
					t.Errorf("expected tokens %q, got %q", tt.tokens, matches[1])
				}
				if matches[2] != tt.tools {
					t.Errorf("expected tools %q, got %q", tt.tools, matches[2])
				}
				if matches[3] != tt.durMs {
					t.Errorf("expected duration_ms %q, got %q", tt.durMs, matches[3])
				}
			} else if len(matches) > 0 {
				t.Errorf("expected no match, got %v", matches)
			}
		})
	}
}

func TestSubagentCallJSON(t *testing.T) {
	call := SubagentCall{
		Type:        "Explore",
		Description: "Explore codebase structure",
		ToolID:      "tool-1",
		ToolCalls:   29,
		Tokens:      36568,
		DurationMS:  43095,
		Status:      "completed",
	}

	data, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed SubagentCall
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Type != "Explore" {
		t.Errorf("expected type %q, got %q", "Explore", parsed.Type)
	}
	if parsed.Tokens != 36568 {
		t.Errorf("expected tokens 36568, got %d", parsed.Tokens)
	}
	if parsed.ToolCalls != 29 {
		t.Errorf("expected tool_calls 29, got %d", parsed.ToolCalls)
	}
	if parsed.DurationMS != 43095 {
		t.Errorf("expected duration_ms 43095, got %f", parsed.DurationMS)
	}
}

func TestSubagentCallOmittedFromStatusWhenEmpty(t *testing.T) {
	process := NewProcess(DefaultOptions())

	data, err := process.MarshalStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if _, exists := raw["subagent_calls"]; exists {
		t.Error("expected subagent_calls to be omitted from JSON when empty")
	}
}

func TestProcess_StatusIncludesSubagentCalls(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Simulate subagent tracking.
	process.mu.Lock()
	process.status = ProcessStatusBusy
	args, _ := json.Marshal(taskToolArgs{SubagentType: "Explore", Description: "test"})
	process.subagents.handleToolUse(StreamMessage{
		Type: MessageTypeAssistant, Subtype: SubtypeToolUse,
		ToolName: "Task", ToolID: "t1", ToolArgs: args,
	})
	process.subagents.handleMessage(StreamMessage{
		Type: MessageTypeAssistant,
		Text: "<usage>\ntotal_tokens: 5000\ntool_uses: 10\nduration_ms: 3000\n</usage>",
	})
	process.mu.Unlock()

	status := process.Status()
	if len(status.SubagentCalls) != 1 {
		t.Fatalf("expected 1 subagent call, got %d", len(status.SubagentCalls))
	}
	if status.SubagentCalls[0].Type != "Explore" {
		t.Errorf("expected type %q, got %q", "Explore", status.SubagentCalls[0].Type)
	}
	if status.SubagentCalls[0].Tokens != 5000 {
		t.Errorf("expected tokens 5000, got %d", status.SubagentCalls[0].Tokens)
	}

	// Verify returned slice is a copy.
	status.SubagentCalls[0].Tokens = 999
	status2 := process.Status()
	if status2.SubagentCalls[0].Tokens != 5000 {
		t.Errorf("expected internal state to be unchanged, got tokens=%d", status2.SubagentCalls[0].Tokens)
	}
}

func TestProcess_ResultDetailIncludesSubagentCalls(t *testing.T) {
	process := NewProcess(DefaultOptions())

	process.mu.Lock()
	process.status = ProcessStatusCompleted
	process.result = resultState{text: "done", completed: true}
	args, _ := json.Marshal(taskToolArgs{SubagentType: "Explore", Description: "test"})
	process.subagents.handleToolUse(StreamMessage{
		Type: MessageTypeAssistant, Subtype: SubtypeToolUse,
		ToolName: "Task", ToolID: "t1", ToolArgs: args,
	})
	process.subagents.handleMessage(StreamMessage{
		Type: MessageTypeAssistant,
		Text: "<usage>\ntotal_tokens: 2000\ntool_uses: 5\nduration_ms: 1000\n</usage>",
	})
	process.mu.Unlock()

	detail := process.ResultDetail()
	if len(detail.SubagentCalls) != 1 {
		t.Fatalf("expected 1 subagent call, got %d", len(detail.SubagentCalls))
	}
	if detail.SubagentCalls[0].Tokens != 2000 {
		t.Errorf("expected tokens 2000, got %d", detail.SubagentCalls[0].Tokens)
	}
}

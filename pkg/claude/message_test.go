package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestParseStreamMessage_System(t *testing.T) {
	data := []byte(`{"type":"system","session_id":"abc-123"}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != MessageTypeSystem {
		t.Errorf("expected type %q, got %q", MessageTypeSystem, msg.Type)
	}
	if msg.SessionID != "abc-123" {
		t.Errorf("expected session_id %q, got %q", "abc-123", msg.SessionID)
	}
	if msg.Raw == nil {
		t.Error("expected Raw to be populated")
	}
}

func TestParseStreamMessage_AssistantText(t *testing.T) {
	data := []byte(`{"type":"assistant","subtype":"text","text":"Hello world"}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != MessageTypeAssistant {
		t.Errorf("expected type %q, got %q", MessageTypeAssistant, msg.Type)
	}
	if msg.Subtype != SubtypeText {
		t.Errorf("expected subtype %q, got %q", SubtypeText, msg.Subtype)
	}
	if msg.Text != "Hello world" {
		t.Errorf("expected text %q, got %q", "Hello world", msg.Text)
	}
}

func TestParseStreamMessage_Result(t *testing.T) {
	data := []byte(`{"type":"result","result":"done","duration_ms":1234.5,"cost_usd":0.01,"total_cost_usd":0.05}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != MessageTypeResult {
		t.Errorf("expected type %q, got %q", MessageTypeResult, msg.Type)
	}
	if msg.Result != "done" {
		t.Errorf("expected result %q, got %q", "done", msg.Result)
	}
	if msg.Duration != 1234.5 {
		t.Errorf("expected duration 1234.5, got %f", msg.Duration)
	}
	if msg.TotalCost != 0.05 {
		t.Errorf("expected total_cost 0.05, got %f", msg.TotalCost)
	}
}

func TestParseStreamMessage_ToolUse(t *testing.T) {
	data := []byte(`{"type":"assistant","subtype":"tool_use","tool_name":"read","tool_id":"t1"}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Subtype != SubtypeToolUse {
		t.Errorf("expected subtype %q, got %q", SubtypeToolUse, msg.Subtype)
	}
	if msg.ToolName != "read" {
		t.Errorf("expected tool_name %q, got %q", "read", msg.ToolName)
	}
}

func TestParseStreamMessage_InvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	_, err := ParseStreamMessage(data)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseStreamMessage_RawPreserved(t *testing.T) {
	data := []byte(`{"type":"system","session_id":"s1"}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(msg.Raw) != string(data) {
		t.Errorf("expected Raw to equal original data")
	}

	// Ensure it's a copy, not the same slice.
	data[0] = 'X'
	if msg.Raw[0] == 'X' {
		t.Error("Raw should be a copy, not a reference to the input")
	}
}

func TestParseStreamMessage_Timestamp(t *testing.T) {
	data := []byte(`{"type":"system","session_id":"abc-123"}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Timestamp == "" {
		t.Fatal("expected Timestamp to be set")
	}

	// Verify it's valid RFC3339.
	parsed, err := time.Parse(time.RFC3339, msg.Timestamp)
	if err != nil {
		t.Fatalf("expected valid RFC3339 timestamp, got %q: %v", msg.Timestamp, err)
	}

	// Timestamp should be recent (within last 5 seconds).
	if time.Since(parsed) > 5*time.Second {
		t.Errorf("expected recent timestamp, got %v", parsed)
	}
}

func TestParseStreamMessage_UsageField(t *testing.T) {
	data := []byte(`{
		"type":"assistant",
		"subtype":"text",
		"text":"Hello",
		"usage": {
			"input_tokens": 150,
			"output_tokens": 50,
			"cache_creation_input_tokens": 1000,
			"cache_read_input_tokens": 500
		}
	}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Usage == nil {
		t.Fatal("expected Usage to be parsed")
	}
	if msg.Usage.InputTokens != 150 {
		t.Errorf("expected InputTokens 150, got %d", msg.Usage.InputTokens)
	}
	if msg.Usage.OutputTokens != 50 {
		t.Errorf("expected OutputTokens 50, got %d", msg.Usage.OutputTokens)
	}
	if msg.Usage.CacheCreationInputTokens != 1000 {
		t.Errorf("expected CacheCreationInputTokens 1000, got %d", msg.Usage.CacheCreationInputTokens)
	}
	if msg.Usage.CacheReadInputTokens != 500 {
		t.Errorf("expected CacheReadInputTokens 500, got %d", msg.Usage.CacheReadInputTokens)
	}
}

func TestParseStreamMessage_NoUsage(t *testing.T) {
	data := []byte(`{"type":"assistant","subtype":"text","text":"Hello"}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Usage != nil {
		t.Errorf("expected nil Usage when not present, got %v", msg.Usage)
	}
}

func TestParseStreamMessage_CostOnAssistant(t *testing.T) {
	// Some Claude CLI versions include cost_usd on assistant messages.
	data := []byte(`{"type":"assistant","subtype":"text","text":"Hello","cost_usd":0.003}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Cost != 0.003 {
		t.Errorf("expected cost_usd 0.003, got %f", msg.Cost)
	}
}

func TestFloat64Ptr(t *testing.T) {
	p := Float64Ptr(1.23)
	if p == nil {
		t.Fatal("expected non-nil pointer")
	}
	if *p != 1.23 {
		t.Errorf("expected 1.23, got %f", *p)
	}
}

func TestStatusInfo_TotalCostNullWhenUnknown(t *testing.T) {
	// When no cost has been observed, TotalCost should be nil (serializes as null).
	info := StatusInfo{Status: ProcessStatusBusy}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	costVal := string(raw["total_cost_usd"])
	if costVal != "null" {
		t.Errorf("expected total_cost_usd to be null when not set, got %s", costVal)
	}
}

func TestStatusInfo_TotalCostZeroWhenExplicit(t *testing.T) {
	// When cost is known to be 0, TotalCost should serialize as 0, not null.
	zero := 0.0
	info := StatusInfo{Status: ProcessStatusCompleted, TotalCost: &zero}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	costVal := string(raw["total_cost_usd"])
	if costVal != "0" {
		t.Errorf("expected total_cost_usd to be 0, got %s", costVal)
	}
}

func TestTokenUsage_OmittedWhenEmpty(t *testing.T) {
	info := StatusInfo{Status: ProcessStatusIdle}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if _, exists := raw["token_usage"]; exists {
		t.Error("expected token_usage to be omitted when nil")
	}
}

func TestTokenUsage_PresentWhenPopulated(t *testing.T) {
	info := StatusInfo{
		Status: ProcessStatusBusy,
		TokenUsage: &TokenUsage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if _, exists := raw["token_usage"]; !exists {
		t.Error("expected token_usage to be present when populated")
	}
}

func TestProcessStatus_Values(t *testing.T) {
	statuses := []ProcessStatus{
		ProcessStatusStarting,
		ProcessStatusIdle,
		ProcessStatusBusy,
		ProcessStatusCompleted,
		ProcessStatusStopped,
		ProcessStatusError,
	}

	for _, s := range statuses {
		if s == "" {
			t.Error("expected non-empty status value")
		}
	}
}

func TestSubmitDrain(t *testing.T) {
	t.Run("drains messages and calls storeFn", func(t *testing.T) {
		ch := make(chan StreamMessage, 3)
		ch <- StreamMessage{Type: MessageTypeSystem, SessionID: "sess-1"}
		ch <- StreamMessage{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "working..."}
		ch <- StreamMessage{Type: MessageTypeResult, Result: "done"}
		close(ch)

		var gotText string
		var gotMessages []StreamMessage
		done := make(chan struct{})

		submitDrain(context.Background(), ch, func(text string, messages []StreamMessage) {
			gotText = text
			gotMessages = messages
			close(done)
		})

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("submitDrain did not complete in time")
		}

		if gotText != "done" {
			t.Errorf("expected result text %q, got %q", "done", gotText)
		}
		if len(gotMessages) != 3 {
			t.Errorf("expected 3 messages, got %d", len(gotMessages))
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		// Use an unbuffered channel so the send below blocks until the
		// drain goroutine reads the message, guaranteeing it is collected
		// before context cancellation.
		ch := make(chan StreamMessage)

		ctx, cancel := context.WithCancel(context.Background())

		var gotText string
		var gotMessages []StreamMessage
		done := make(chan struct{})

		submitDrain(ctx, ch, func(text string, messages []StreamMessage) {
			gotText = text
			gotMessages = messages
			close(done)
		})

		// Send blocks until the drain goroutine receives the message.
		ch <- StreamMessage{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "partial"}

		// Now cancel -- the message is guaranteed to have been read.
		cancel()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("submitDrain did not complete after context cancellation")
		}

		// Should have collected partial messages.
		if len(gotMessages) != 1 {
			t.Errorf("expected 1 message, got %d", len(gotMessages))
		}
		// Fallback to assistant text since no result message.
		if gotText != "partial" {
			t.Errorf("expected result text %q, got %q", "partial", gotText)
		}
	})

	t.Run("empty channel returns empty result", func(t *testing.T) {
		ch := make(chan StreamMessage)
		close(ch)

		var gotText string
		var gotMessages []StreamMessage
		done := make(chan struct{})

		submitDrain(context.Background(), ch, func(text string, messages []StreamMessage) {
			gotText = text
			gotMessages = messages
			close(done)
		})

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("submitDrain did not complete in time")
		}

		if gotText != "" {
			t.Errorf("expected empty result text, got %q", gotText)
		}
		if len(gotMessages) != 0 {
			t.Errorf("expected 0 messages, got %d", len(gotMessages))
		}
	})
}

func TestCopyToolCalls(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		if got := copyToolCalls(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("empty map returns empty non-nil map", func(t *testing.T) {
		cp := copyToolCalls(map[string]int{})
		if cp == nil {
			t.Error("expected non-nil copy of empty map")
		}
		if len(cp) != 0 {
			t.Errorf("expected empty map, got %v", cp)
		}
	})

	t.Run("returns independent copy", func(t *testing.T) {
		original := map[string]int{"Bash": 3, "Read": 1}
		cp := copyToolCalls(original)
		cp["Bash"] = 999
		if original["Bash"] != 3 {
			t.Errorf("expected original unchanged, got Bash=%d", original["Bash"])
		}
	})
}

func TestSubmitAsync(t *testing.T) {
	t.Run("preserves previous result on run failure", func(t *testing.T) {
		// Simulate a previous result that should be preserved when the
		// next Submit fails (e.g. process already busy).
		previousResult := resultState{
			text:     "previous result",
			messages: []StreamMessage{{Type: MessageTypeResult, Result: "previous result"}},
		}
		current := previousResult

		failingRunFn := func(_ context.Context, _ string, _ *RunOptions) (<-chan StreamMessage, error) {
			return nil, fmt.Errorf("claude process is already busy")
		}

		err := submitAsync(context.Background(), "new prompt", nil, failingRunFn, func(rs resultState) {
			current = rs
		})
		if err == nil {
			t.Fatal("expected error from runFn")
		}

		// Previous result should be preserved -- setResult should NOT have been called.
		if current.text != "previous result" {
			t.Errorf("expected previous result to be preserved, got %q", current.text)
		}
		if len(current.messages) != 1 {
			t.Errorf("expected previous messages to be preserved, got %d messages", len(current.messages))
		}
	})

	t.Run("clears and stores result on success", func(t *testing.T) {
		var current resultState
		var setCount int
		done := make(chan struct{})

		successRunFn := func(_ context.Context, _ string, _ *RunOptions) (<-chan StreamMessage, error) {
			ch := make(chan StreamMessage, 1)
			ch <- StreamMessage{Type: MessageTypeResult, Result: "new result"}
			close(ch)
			return ch, nil
		}

		err := submitAsync(context.Background(), "do something", nil, successRunFn, func(rs resultState) {
			setCount++
			current = rs
			// The second call (from submitDrain) signals completion.
			if setCount == 2 {
				close(done)
			}
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("submitAsync did not complete in time")
		}

		// setResult should have been called twice: once to clear, once to store.
		if setCount != 2 {
			t.Errorf("expected setResult called 2 times, got %d", setCount)
		}
		if current.text != "new result" {
			t.Errorf("expected result %q, got %q", "new result", current.text)
		}
	})
}

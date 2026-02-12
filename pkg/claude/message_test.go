package claude

import (
	"context"
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

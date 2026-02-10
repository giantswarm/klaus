package claude

import (
	"testing"
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
		ProcessStatusStopped,
		ProcessStatusError,
	}

	for _, s := range statuses {
		if s == "" {
			t.Error("expected non-empty status value")
		}
	}
}

package claude

import (
	"context"
	"encoding/json"
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

func TestParseStreamMessage_NestedAssistantText(t *testing.T) {
	// Claude Code 2.1+ format: text content nested in message.content[].
	data := []byte(`{"type":"assistant","message":{"model":"claude-sonnet-4-6","id":"msg_01","type":"message","role":"assistant","content":[{"type":"text","text":"Hi!"}],"usage":{"input_tokens":3,"output_tokens":1}},"session_id":"sess-1"}`)
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
	if msg.Text != "Hi!" {
		t.Errorf("expected text %q, got %q", "Hi!", msg.Text)
	}
	if msg.Usage == nil {
		t.Fatal("expected Usage to be parsed from nested message")
	}
	if msg.Usage.InputTokens != 3 {
		t.Errorf("expected InputTokens 3, got %d", msg.Usage.InputTokens)
	}
	if msg.Usage.OutputTokens != 1 {
		t.Errorf("expected OutputTokens 1, got %d", msg.Usage.OutputTokens)
	}
	if msg.SessionID != "sess-1" {
		t.Errorf("expected session_id %q, got %q", "sess-1", msg.SessionID)
	}
}

func TestParseStreamMessage_NestedAssistantToolUse(t *testing.T) {
	// Claude Code 2.1+ format: tool_use content nested in message.content[].
	data := []byte(`{"type":"assistant","message":{"id":"msg_02","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls -la"}}]},"session_id":"sess-2"}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != MessageTypeAssistant {
		t.Errorf("expected type %q, got %q", MessageTypeAssistant, msg.Type)
	}
	if msg.Subtype != SubtypeToolUse {
		t.Errorf("expected subtype %q, got %q", SubtypeToolUse, msg.Subtype)
	}
	if msg.ToolName != "Bash" {
		t.Errorf("expected tool_name %q, got %q", "Bash", msg.ToolName)
	}
	if msg.ToolID != "toolu_1" {
		t.Errorf("expected tool_id %q, got %q", "toolu_1", msg.ToolID)
	}
	if string(msg.ToolArgs) != `{"command":"ls -la"}` {
		t.Errorf("expected tool_args %q, got %q", `{"command":"ls -la"}`, string(msg.ToolArgs))
	}
}

func TestParseStreamMessage_NestedUsageFallback(t *testing.T) {
	// When top-level usage is nil, nested message.usage should be used.
	data := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":10,"output_tokens":5}}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Usage == nil {
		t.Fatal("expected Usage from nested message")
	}
	if msg.Usage.InputTokens != 10 || msg.Usage.OutputTokens != 5 {
		t.Errorf("unexpected usage: %+v", msg.Usage)
	}
}

func TestParseStreamMessage_NestedUsageDoesNotOverrideTopLevel(t *testing.T) {
	// When top-level usage is present, nested usage should not override it.
	data := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":99,"output_tokens":99}},"usage":{"input_tokens":10,"output_tokens":5}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Usage == nil {
		t.Fatal("expected Usage to be present")
	}
	// Top-level usage should win.
	if msg.Usage.InputTokens != 10 || msg.Usage.OutputTokens != 5 {
		t.Errorf("expected top-level usage to be preserved, got %+v", msg.Usage)
	}
}

func TestParseStreamMessage_OldFormatStillWorks(t *testing.T) {
	// Old format with top-level fields should continue to work unchanged.
	data := []byte(`{"type":"assistant","subtype":"text","text":"Hello world","usage":{"input_tokens":100,"output_tokens":50}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Subtype != SubtypeText {
		t.Errorf("expected subtype %q, got %q", SubtypeText, msg.Subtype)
	}
	if msg.Text != "Hello world" {
		t.Errorf("expected text %q, got %q", "Hello world", msg.Text)
	}
	if msg.Usage == nil || msg.Usage.InputTokens != 100 {
		t.Errorf("expected top-level usage to be preserved")
	}
}

func TestParseStreamMessage_NestedTextConcatenation(t *testing.T) {
	// Multiple text blocks should be concatenated.
	data := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello "},{"type":"text","text":"world!"}]}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Subtype != SubtypeText {
		t.Errorf("expected subtype %q, got %q", SubtypeText, msg.Subtype)
	}
	if msg.Text != "Hello world!" {
		t.Errorf("expected concatenated text %q, got %q", "Hello world!", msg.Text)
	}
}

func TestParseStreamMessage_NestedInvalidEnvelope(t *testing.T) {
	// When message is present but not a valid envelope, degrade gracefully.
	data := []byte(`{"type":"assistant","message":{"content":"not-an-array"}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall through without populating nested fields.
	if msg.Subtype != "" {
		t.Errorf("expected empty subtype for unparseable envelope, got %q", msg.Subtype)
	}
}

func TestParseStreamMessage_NestedEmptyContent(t *testing.T) {
	// Empty content array should not populate any fields.
	data := []byte(`{"type":"assistant","message":{"content":[]}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Subtype != "" {
		t.Errorf("expected empty subtype for empty content, got %q", msg.Subtype)
	}
}

func TestParseStreamMessage_NestedMultipleToolUse(t *testing.T) {
	// Only the first tool_use block should be used.
	data := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"cmd":"a"}},{"type":"tool_use","id":"t2","name":"Read","input":{"path":"b"}}]}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ToolName != "Bash" {
		t.Errorf("expected first tool_use (Bash), got %q", msg.ToolName)
	}
	if msg.ToolID != "t1" {
		t.Errorf("expected first tool_id (t1), got %q", msg.ToolID)
	}
}

func TestParseStreamMessage_NestedToolUseOverridesTextSubtype(t *testing.T) {
	// When both text and tool_use blocks are present, tool_use should win for subtype.
	data := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Let me check..."},{"type":"tool_use","id":"t1","name":"Read","input":{"path":"/tmp"}}]}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Subtype != SubtypeToolUse {
		t.Errorf("expected subtype %q, got %q", SubtypeToolUse, msg.Subtype)
	}
	if msg.Text != "Let me check..." {
		t.Errorf("expected text %q, got %q", "Let me check...", msg.Text)
	}
	if msg.ToolName != "Read" {
		t.Errorf("expected tool_name %q, got %q", "Read", msg.ToolName)
	}
}

func TestSummarizeMessages_NestedFormat(t *testing.T) {
	// Verify that SummarizeMessages works with messages parsed from the new format.
	textData := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hi there!"}]}}`)
	toolData := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`)

	textMsg, err := ParseStreamMessage(textData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	toolMsg, err := ParseStreamMessage(toolData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	summaries := SummarizeMessages([]StreamMessage{textMsg, toolMsg})
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if summaries[0].Content != "Hi there!" {
		t.Errorf("expected text content %q, got %q", "Hi there!", summaries[0].Content)
	}
	if summaries[1].Content != "Using tool: Bash" {
		t.Errorf("expected tool content %q, got %q", "Using tool: Bash", summaries[1].Content)
	}
}

func TestCollectResultText_NestedFormat(t *testing.T) {
	// Fallback to concatenated assistant text should work with nested format.
	textData := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"partial output"}]}}`)
	msg, err := ParseStreamMessage(textData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := CollectResultText([]StreamMessage{msg})
	if text != "partial output" {
		t.Errorf("expected %q, got %q", "partial output", text)
	}
}

func TestParseStreamMessage_StreamEvent_ContentBlockDelta(t *testing.T) {
	data := []byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != MessageTypeStreamEvent {
		t.Errorf("expected type %q, got %q", MessageTypeStreamEvent, msg.Type)
	}
	if msg.EventType != "content_block_delta" {
		t.Errorf("expected EventType %q, got %q", "content_block_delta", msg.EventType)
	}
	if msg.DeltaText != "Hello" {
		t.Errorf("expected DeltaText %q, got %q", "Hello", msg.DeltaText)
	}
	if msg.Raw == nil {
		t.Error("expected Raw to be populated")
	}
}

func TestParseStreamMessage_StreamEvent_MessageStart(t *testing.T) {
	data := []byte(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-20250514"}}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != MessageTypeStreamEvent {
		t.Errorf("expected type %q, got %q", MessageTypeStreamEvent, msg.Type)
	}
	if msg.EventType != "message_start" {
		t.Errorf("expected EventType %q, got %q", "message_start", msg.EventType)
	}
	if msg.DeltaText != "" {
		t.Errorf("expected empty DeltaText for message_start, got %q", msg.DeltaText)
	}
}

func TestParseStreamMessage_StreamEvent_ContentBlockStop(t *testing.T) {
	data := []byte(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != MessageTypeStreamEvent {
		t.Errorf("expected type %q, got %q", MessageTypeStreamEvent, msg.Type)
	}
	if msg.EventType != "content_block_stop" {
		t.Errorf("expected EventType %q, got %q", "content_block_stop", msg.EventType)
	}
	if msg.DeltaText != "" {
		t.Errorf("expected empty DeltaText for content_block_stop, got %q", msg.DeltaText)
	}
}

func TestParseStreamMessage_StreamEvent_NonTextDelta(t *testing.T) {
	// input_json_delta has type != "text_delta", so DeltaText should stay empty.
	data := []byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.EventType != "content_block_delta" {
		t.Errorf("expected EventType %q, got %q", "content_block_delta", msg.EventType)
	}
	if msg.DeltaText != "" {
		t.Errorf("expected empty DeltaText for input_json_delta, got %q", msg.DeltaText)
	}
}

func TestParseStreamMessage_StreamEvent_ContentBlockStartToolUse(t *testing.T) {
	data := []byte(`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"Read","input":{}}}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != MessageTypeStreamEvent {
		t.Errorf("expected type %q, got %q", MessageTypeStreamEvent, msg.Type)
	}
	if msg.EventType != "content_block_start" {
		t.Errorf("expected EventType %q, got %q", "content_block_start", msg.EventType)
	}
	if msg.ToolUseName != "Read" {
		t.Errorf("expected ToolUseName %q, got %q", "Read", msg.ToolUseName)
	}
	if msg.DeltaText != "" {
		t.Errorf("expected empty DeltaText, got %q", msg.DeltaText)
	}
}

func TestParseStreamMessage_StreamEvent_ContentBlockStartText(t *testing.T) {
	data := []byte(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.EventType != "content_block_start" {
		t.Errorf("expected EventType %q, got %q", "content_block_start", msg.EventType)
	}
	if msg.ToolUseName != "" {
		t.Errorf("expected empty ToolUseName for text block, got %q", msg.ToolUseName)
	}
}

func TestSummarizeMessages_SkipsStreamEvent(t *testing.T) {
	msgs := []StreamMessage{
		{Type: MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "token"},
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "Hello"},
		{Type: MessageTypeStreamEvent, EventType: "message_stop"},
		{Type: MessageTypeResult, Result: "done"},
	}

	summaries := SummarizeMessages(msgs)

	// stream_event messages have no role/content, so summarizeMessage returns
	// empty MessageSummary which SummarizeMessages filters out.
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries (assistant + result), got %d", len(summaries))
	}
	if summaries[0].Role != "assistant" || summaries[0].Content != "Hello" {
		t.Errorf("expected assistant summary, got %+v", summaries[0])
	}
	if summaries[1].Role != "result" || summaries[1].Content != "done" {
		t.Errorf("expected result summary, got %+v", summaries[1])
	}
}

func TestCollectResultText_IgnoresStreamEvent(t *testing.T) {
	msgs := []StreamMessage{
		{Type: MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "partial"},
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "full response"},
		{Type: MessageTypeStreamEvent, EventType: "message_stop"},
		{Type: MessageTypeResult, Result: "final result"},
	}

	text := CollectResultText(msgs)
	if text != "final result" {
		t.Errorf("expected %q, got %q", "final result", text)
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

	t.Run("filters out stream_event messages", func(t *testing.T) {
		ch := make(chan StreamMessage, 5)
		ch <- StreamMessage{Type: MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "He"}
		ch <- StreamMessage{Type: MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "llo"}
		ch <- StreamMessage{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "Hello"}
		ch <- StreamMessage{Type: MessageTypeStreamEvent, EventType: "message_stop"}
		ch <- StreamMessage{Type: MessageTypeResult, Result: "Hello"}
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

		if gotText != "Hello" {
			t.Errorf("expected result text %q, got %q", "Hello", gotText)
		}
		// Only assistant and result messages should be stored, not stream_event.
		if len(gotMessages) != 2 {
			t.Fatalf("expected 2 messages (stream_event filtered), got %d", len(gotMessages))
		}
		for _, msg := range gotMessages {
			if msg.Type == MessageTypeStreamEvent {
				t.Error("stream_event message should not be stored in drain results")
			}
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
			return nil, ErrBusy
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

func TestSummarizeMessages_DeduplicatesSystem(t *testing.T) {
	msgs := []StreamMessage{
		{Type: MessageTypeSystem, SessionID: "sess-1"},
		{Type: MessageTypeSystem, SessionID: "sess-1"},
		{Type: MessageTypeSystem, SessionID: "sess-1"},
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "Hello"},
	}

	summaries := SummarizeMessages(msgs)
	systemCount := 0
	for _, s := range summaries {
		if s.Role == "system" {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Errorf("expected 1 system message after dedup, got %d", systemCount)
	}
	if len(summaries) != 2 {
		t.Errorf("expected 2 summaries total, got %d", len(summaries))
	}
}

func TestSummarizeMessages_IncludesUserToolResults(t *testing.T) {
	msgs := []StreamMessage{
		{
			Type:    MessageTypeUser,
			Message: json.RawMessage(`{"content":[{"type":"tool_result","tool_use_id":"toolu_abc","content":"output text","is_error":false}]}`),
		},
	}

	summaries := SummarizeMessages(msgs)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Role != "tool" {
		t.Errorf("expected role 'tool', got %q", summaries[0].Role)
	}
	if summaries[0].Content != "output text" {
		t.Errorf("expected content 'output text', got %q", summaries[0].Content)
	}
	if summaries[0].ToolCallID != "toolu_abc" {
		t.Errorf("expected tool_call_id 'toolu_abc', got %q", summaries[0].ToolCallID)
	}
}

func TestSummarizeMessages_ToolCallInfo(t *testing.T) {
	msgs := []StreamMessage{
		{
			Type:     MessageTypeAssistant,
			Subtype:  SubtypeToolUse,
			ToolName: "Bash",
			ToolID:   "toolu_xyz",
			ToolArgs: json.RawMessage(`{"command":"echo hi"}`),
		},
	}

	summaries := SummarizeMessages(msgs)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if len(summaries[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(summaries[0].ToolCalls))
	}
	tc := summaries[0].ToolCalls[0]
	if tc.ID != "toolu_xyz" {
		t.Errorf("expected ID 'toolu_xyz', got %q", tc.ID)
	}
	if tc.Name != "Bash" {
		t.Errorf("expected name 'Bash', got %q", tc.Name)
	}
	if string(tc.Args) != `{"command":"echo hi"}` {
		t.Errorf("expected args, got %q", string(tc.Args))
	}
}

func TestParseStreamMessage_StreamEvent_ToolUseBlockID(t *testing.T) {
	data := []byte(`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","id":"toolu_abc123","name":"Bash"}}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.ToolUseName != "Bash" {
		t.Errorf("expected ToolUseName %q, got %q", "Bash", msg.ToolUseName)
	}
	if msg.ToolUseBlockID != "toolu_abc123" {
		t.Errorf("expected ToolUseBlockID %q, got %q", "toolu_abc123", msg.ToolUseBlockID)
	}
}

func TestParseStreamMessage_StreamEvent_InputJSONDelta(t *testing.T) {
	data := []byte(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"cmd\":\"ls\"}"}}}`)
	msg, err := ParseStreamMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.EventType != "content_block_delta" {
		t.Errorf("expected EventType %q, got %q", "content_block_delta", msg.EventType)
	}
	if msg.InputJSONDelta != `{"cmd":"ls"}` {
		t.Errorf("expected InputJSONDelta %q, got %q", `{"cmd":"ls"}`, msg.InputJSONDelta)
	}
}

package claude

import (
	"encoding/json"
	"testing"
)

// helper to create a StreamMessage from raw JSON.
func rawMsg(t *testing.T, raw string) StreamMessage {
	t.Helper()
	msg, err := ParseStreamMessage([]byte(raw))
	if err != nil {
		t.Fatalf("ParseStreamMessage failed: %v", err)
	}
	return msg
}

func TestToOpenAIMessages_UserPrompt(t *testing.T) {
	msgs := []StreamMessage{
		syntheticUserMessage("Fix the Dockerfile"),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("expected role user, got %s", result[0].Role)
	}
	if result[0].Content == nil || *result[0].Content != "Fix the Dockerfile" {
		t.Errorf("expected content 'Fix the Dockerfile', got %v", result[0].Content)
	}
}

func TestToOpenAIMessages_AssistantText(t *testing.T) {
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "assistant",
			"message": {
				"id": "msg_01",
				"role": "assistant",
				"content": [{"type": "text", "text": "I'll read the file."}]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != "assistant" {
		t.Errorf("expected role assistant, got %s", result[0].Role)
	}
	if result[0].Content == nil || *result[0].Content != "I'll read the file." {
		t.Errorf("expected content, got %v", result[0].Content)
	}
}

func TestToOpenAIMessages_AssistantToolUse(t *testing.T) {
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "assistant",
			"message": {
				"id": "msg_01",
				"role": "assistant",
				"content": [{
					"type": "tool_use",
					"id": "toolu_01",
					"name": "Read",
					"input": {"file_path": "/workspace/Dockerfile"}
				}]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	m := result[0]
	if m.Content != nil {
		t.Errorf("expected content nil for tool_calls-only message, got %v", *m.Content)
	}
	if len(m.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(m.ToolCalls))
	}
	tc := m.ToolCalls[0]
	if tc.ID != "toolu_01" {
		t.Errorf("expected tool call id toolu_01, got %s", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("expected type function, got %s", tc.Type)
	}
	if tc.Function.Name != "Read" {
		t.Errorf("expected function name Read, got %s", tc.Function.Name)
	}
	// Arguments must be a JSON string, not a parsed object.
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments is not valid JSON: %v", err)
	}
	if args["file_path"] != "/workspace/Dockerfile" {
		t.Errorf("expected file_path in arguments, got %v", args)
	}
}

func TestToOpenAIMessages_Consolidation(t *testing.T) {
	// Multiple assistant messages with the same message.id should be consolidated.
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "assistant",
			"message": {
				"id": "msg_01",
				"role": "assistant",
				"content": [{"type": "text", "text": "Let me read the file."}]
			}
		}`),
		rawMsg(t, `{
			"type": "assistant",
			"message": {
				"id": "msg_01",
				"role": "assistant",
				"content": [{
					"type": "tool_use",
					"id": "toolu_01",
					"name": "Read",
					"input": {"file_path": "/workspace/main.go"}
				}]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 consolidated message, got %d", len(result))
	}
	m := result[0]
	if m.Content == nil || *m.Content != "Let me read the file." {
		t.Errorf("expected consolidated text content, got %v", m.Content)
	}
	if len(m.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call in consolidated message, got %d", len(m.ToolCalls))
	}
}

func TestToOpenAIMessages_DifferentIDsNotConsolidated(t *testing.T) {
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "assistant",
			"message": {
				"id": "msg_01",
				"role": "assistant",
				"content": [{"type": "text", "text": "First message."}]
			}
		}`),
		rawMsg(t, `{
			"type": "assistant",
			"message": {
				"id": "msg_02",
				"role": "assistant",
				"content": [{"type": "text", "text": "Second message."}]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 separate messages, got %d", len(result))
	}
}

func TestToOpenAIMessages_ToolResult(t *testing.T) {
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "user",
			"message": {
				"role": "user",
				"content": [
					{"type": "tool_result", "content": "file contents here", "tool_use_id": "toolu_01"},
					{"type": "tool_result", "content": "another result", "tool_use_id": "toolu_02", "is_error": true}
				]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 tool messages (one per tool_result), got %d", len(result))
	}
	if result[0].Role != "tool" {
		t.Errorf("expected role tool, got %s", result[0].Role)
	}
	if result[0].ToolCallID != "toolu_01" {
		t.Errorf("expected tool_call_id toolu_01, got %s", result[0].ToolCallID)
	}
	if result[1].ToolCallID != "toolu_02" {
		t.Errorf("expected tool_call_id toolu_02, got %s", result[1].ToolCallID)
	}
}

func TestToOpenAIMessages_ToolResultArrayContent(t *testing.T) {
	// MCP tools return content as an array of content blocks, not a string.
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "user",
			"message": {
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_01NjuBT6ZoH9rk49V4tHqX1B",
						"content": [
							{"type": "text", "text": "{\"filtered_count\": 5, \"filters\": {\"pattern\": \"*pro*\"}}"}
						]
					}
				]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool message, got %d", len(result))
	}
	if result[0].Role != "tool" {
		t.Errorf("expected role tool, got %s", result[0].Role)
	}
	if result[0].ToolCallID != "toolu_01NjuBT6ZoH9rk49V4tHqX1B" {
		t.Errorf("expected tool_call_id, got %s", result[0].ToolCallID)
	}
	if result[0].Content == nil || *result[0].Content != `{"filtered_count": 5, "filters": {"pattern": "*pro*"}}` {
		t.Errorf("expected MCP tool content, got %v", result[0].Content)
	}
}

func TestToOpenAIMessages_ToolResultToolReference(t *testing.T) {
	// ToolSearch returns tool_reference blocks in the content array.
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "user",
			"message": {
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_014i6TwewPcxFFMDd2bvT97Q",
						"content": [
							{"type": "tool_reference", "tool_name": "mcp__muster__filter_tools"},
							{"type": "tool_reference", "tool_name": "mcp__muster__call_tool"}
						]
					}
				]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool message, got %d", len(result))
	}
	if result[0].Content == nil || *result[0].Content != "mcp__muster__filter_toolsmcp__muster__call_tool" {
		t.Errorf("expected concatenated tool names, got %v", result[0].Content)
	}
}

func TestToOpenAIMessages_MixedToolResultFormats(t *testing.T) {
	// Mix of string content (built-in) and array content (MCP) in the same user message.
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "user",
			"message": {
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_builtin",
						"content": "file contents here"
					},
					{
						"type": "tool_result",
						"tool_use_id": "toolu_mcp",
						"content": [{"type": "text", "text": "mcp result data"}]
					},
					{
						"type": "text",
						"text": "Some user context"
					}
				]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages (user text + 2 tool results), got %d", len(result))
	}
	// Text blocks come first (prepended).
	if result[0].Role != "user" || *result[0].Content != "Some user context" {
		t.Errorf("expected user text message first, got %+v", result[0])
	}
	// Built-in tool result (string content).
	if result[1].Role != "tool" || result[1].ToolCallID != "toolu_builtin" {
		t.Errorf("expected built-in tool result, got %+v", result[1])
	}
	if *result[1].Content != "file contents here" {
		t.Errorf("expected string content, got %s", *result[1].Content)
	}
	// MCP tool result (array content).
	if result[2].Role != "tool" || result[2].ToolCallID != "toolu_mcp" {
		t.Errorf("expected MCP tool result, got %+v", result[2])
	}
	if *result[2].Content != "mcp result data" {
		t.Errorf("expected array content extracted, got %s", *result[2].Content)
	}
}

func TestToOpenAIMessages_Thinking(t *testing.T) {
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "assistant",
			"message": {
				"id": "msg_01",
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "Let me think about this...", "signature": "sig123"}
				]
			}
		}`),
		rawMsg(t, `{
			"type": "assistant",
			"message": {
				"id": "msg_01",
				"role": "assistant",
				"content": [
					{"type": "text", "text": "Here's my answer."}
				]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 consolidated message, got %d", len(result))
	}
	m := result[0]
	if m.ReasoningContent != "Let me think about this..." {
		t.Errorf("expected reasoning_content, got %q", m.ReasoningContent)
	}
	if len(m.ThinkingBlocks) != 1 {
		t.Fatalf("expected 1 thinking block, got %d", len(m.ThinkingBlocks))
	}
	if m.ThinkingBlocks[0].Signature != "sig123" {
		t.Errorf("expected signature sig123, got %s", m.ThinkingBlocks[0].Signature)
	}
	if m.Content == nil || *m.Content != "Here's my answer." {
		t.Errorf("expected text content, got %v", m.Content)
	}
}

func TestToOpenAIMessages_SystemExtractedToMetadata(t *testing.T) {
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "system",
			"subtype": "init",
			"session_id": "sess-123",
			"model": "claude-opus-4-6",
			"claude_code_version": "2.1.81",
			"tools": ["Bash", "Read", "Write"],
			"plugins": [{"name": "base", "path": "/var/lib/klaus/plugins/base"}]
		}`),
		rawMsg(t, `{
			"type": "system",
			"subtype": "hook_response",
			"hook_name": "SessionStart:startup",
			"output": "ready",
			"exit_code": 0
		}`),
		rawMsg(t, `{
			"type": "system",
			"subtype": "task_started",
			"task_id": "task-1",
			"description": "Run tests",
			"task_type": "local_bash"
		}`),
	}
	result, meta := ToOpenAIMessages(msgs)
	if len(result) != 0 {
		t.Errorf("system messages should not appear in messages array, got %d", len(result))
	}
	if meta.SessionID != "sess-123" {
		t.Errorf("expected session_id sess-123, got %s", meta.SessionID)
	}
	if meta.Model != "claude-opus-4-6" {
		t.Errorf("expected model claude-opus-4-6, got %s", meta.Model)
	}
	if len(meta.Tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(meta.Tools))
	}
	if len(meta.Plugins) != 1 || meta.Plugins[0].Name != "base" {
		t.Errorf("expected 1 plugin named base, got %v", meta.Plugins)
	}
	if len(meta.Hooks) != 1 || meta.Hooks[0].HookName != "SessionStart:startup" {
		t.Errorf("expected 1 hook, got %v", meta.Hooks)
	}
	if len(meta.Subagents) != 1 || meta.Subagents[0].TaskID != "task-1" {
		t.Errorf("expected 1 subagent, got %v", meta.Subagents)
	}
}

func TestToOpenAIMessages_ResultExtractedToMetadata(t *testing.T) {
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "result",
			"subtype": "success",
			"duration_ms": 5000,
			"num_turns": 10,
			"total_cost_usd": 1.5,
			"usage": {"input_tokens": 100, "output_tokens": 200}
		}`),
	}
	result, meta := ToOpenAIMessages(msgs)
	if len(result) != 0 {
		t.Errorf("result messages should not appear in messages array, got %d", len(result))
	}
	if meta.CostUSD != 1.5 {
		t.Errorf("expected cost 1.5, got %f", meta.CostUSD)
	}
	if meta.DurationMS != 5000 {
		t.Errorf("expected duration 5000, got %f", meta.DurationMS)
	}
	if meta.NumTurns != 10 {
		t.Errorf("expected 10 turns, got %d", meta.NumTurns)
	}
	if meta.Usage == nil || meta.Usage.InputTokens != 100 {
		t.Errorf("expected usage with 100 input tokens, got %v", meta.Usage)
	}
}

func TestToOpenAIMessages_SyntheticUserText(t *testing.T) {
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "user",
			"isSynthetic": true,
			"message": {
				"role": "user",
				"content": [{"type": "text", "text": "System context injection..."}]
			}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("expected role user, got %s", result[0].Role)
	}
	if result[0].Content == nil || *result[0].Content != "System context injection..." {
		t.Errorf("expected content, got %v", result[0].Content)
	}
}

func TestToOpenAIMessages_FullConversation(t *testing.T) {
	// Simulate a realistic conversation flow:
	// system/init → user prompt → assistant thinking+text → assistant tool_use → tool_result → assistant text → result
	msgs := []StreamMessage{
		rawMsg(t, `{"type":"system","subtype":"init","session_id":"s1","model":"claude-opus-4-6"}`),
		syntheticUserMessage("Read /etc/hostname"),
		rawMsg(t, `{
			"type": "assistant",
			"message": {"id": "msg_01", "role": "assistant",
				"content": [{"type": "thinking", "thinking": "The user wants to read a file.", "signature": "sig1"}]}
		}`),
		rawMsg(t, `{
			"type": "assistant",
			"message": {"id": "msg_01", "role": "assistant",
				"content": [{"type": "text", "text": "I'll read that file."}]}
		}`),
		rawMsg(t, `{
			"type": "assistant",
			"message": {"id": "msg_01", "role": "assistant",
				"content": [{"type": "tool_use", "id": "toolu_01", "name": "Read", "input": {"file_path": "/etc/hostname"}}]}
		}`),
		rawMsg(t, `{
			"type": "user",
			"message": {"role": "user",
				"content": [{"type": "tool_result", "content": "myhost", "tool_use_id": "toolu_01"}]}
		}`),
		rawMsg(t, `{
			"type": "assistant",
			"message": {"id": "msg_02", "role": "assistant",
				"content": [{"type": "text", "text": "The hostname is myhost."}]}
		}`),
		rawMsg(t, `{"type":"result","total_cost_usd":0.05,"duration_ms":1000,"num_turns":2}`),
	}

	result, meta := ToOpenAIMessages(msgs)

	// Expected messages: user, assistant(consolidated thinking+text+tool), tool, assistant
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	// Message 0: user prompt
	if result[0].Role != "user" || *result[0].Content != "Read /etc/hostname" {
		t.Errorf("msg[0]: expected user prompt, got %+v", result[0])
	}

	// Message 1: consolidated assistant (thinking + text + tool_use)
	m1 := result[1]
	if m1.Role != "assistant" {
		t.Errorf("msg[1]: expected assistant, got %s", m1.Role)
	}
	if m1.Content == nil || *m1.Content != "I'll read that file." {
		t.Errorf("msg[1]: expected text content, got %v", m1.Content)
	}
	if m1.ReasoningContent != "The user wants to read a file." {
		t.Errorf("msg[1]: expected reasoning_content, got %q", m1.ReasoningContent)
	}
	if len(m1.ToolCalls) != 1 || m1.ToolCalls[0].Function.Name != "Read" {
		t.Errorf("msg[1]: expected 1 tool call (Read), got %v", m1.ToolCalls)
	}

	// Message 2: tool result
	if result[2].Role != "tool" || result[2].ToolCallID != "toolu_01" {
		t.Errorf("msg[2]: expected tool result, got %+v", result[2])
	}

	// Message 3: final assistant text
	if result[3].Role != "assistant" || *result[3].Content != "The hostname is myhost." {
		t.Errorf("msg[3]: expected final assistant text, got %+v", result[3])
	}

	// Metadata
	if meta.SessionID != "s1" {
		t.Errorf("expected session_id s1, got %s", meta.SessionID)
	}
	if meta.CostUSD != 0.05 {
		t.Errorf("expected cost 0.05, got %f", meta.CostUSD)
	}
}

func TestToOpenAIMessages_MultipleToolCalls(t *testing.T) {
	// Multiple tool_use blocks with the same message.id → single message with multiple tool_calls.
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "assistant",
			"message": {"id": "msg_01", "role": "assistant",
				"content": [{"type": "tool_use", "id": "toolu_01", "name": "Read", "input": {"file_path": "a.go"}}]}
		}`),
		rawMsg(t, `{
			"type": "assistant",
			"message": {"id": "msg_01", "role": "assistant",
				"content": [{"type": "tool_use", "id": "toolu_02", "name": "Read", "input": {"file_path": "b.go"}}]}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 consolidated message, got %d", len(result))
	}
	if len(result[0].ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result[0].ToolCalls))
	}
	if result[0].Content != nil {
		t.Errorf("expected null content for tool_calls-only message, got %v", *result[0].Content)
	}
}

func TestToOpenAIMessages_FlatFormat(t *testing.T) {
	// Legacy flat format (pre-2.1) without nested message object.
	msgs := []StreamMessage{
		{
			Type:    MessageTypeAssistant,
			Subtype: SubtypeText,
			Text:    "Hello world",
			Raw:     json.RawMessage(`{"type":"assistant","subtype":"text","text":"Hello world"}`),
		},
		{
			Type:     MessageTypeAssistant,
			Subtype:  SubtypeToolUse,
			ToolName: "Bash",
			ToolID:   "tool-1",
			ToolArgs: json.RawMessage(`{"command":"ls"}`),
			Raw:      json.RawMessage(`{"type":"assistant","subtype":"tool_use","tool_name":"Bash","tool_id":"tool-1"}`),
		},
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Content == nil || *result[0].Content != "Hello world" {
		t.Errorf("expected text content, got %v", result[0].Content)
	}
	if len(result[1].ToolCalls) != 1 || result[1].ToolCalls[0].Function.Name != "Bash" {
		t.Errorf("expected Bash tool call, got %v", result[1].ToolCalls)
	}
}

func TestToOpenAIMessages_UserContentAsString(t *testing.T) {
	// User message where content is a plain string (the injected prompt format).
	msgs := []StreamMessage{
		rawMsg(t, `{
			"type": "user",
			"message": {"role": "user", "content": "Hello from stdin"}
		}`),
	}
	result, _ := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Content == nil || *result[0].Content != "Hello from stdin" {
		t.Errorf("expected content 'Hello from stdin', got %v", result[0].Content)
	}
}

func TestCollectOpenAIMessages_Offset(t *testing.T) {
	msgs := []StreamMessage{
		syntheticUserMessage("prompt 1"),
		rawMsg(t, `{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"reply 1"}]}}`),
		syntheticUserMessage("prompt 2"),
		rawMsg(t, `{"type":"assistant","message":{"id":"m2","role":"assistant","content":[{"type":"text","text":"reply 2"}]}}`),
	}

	info := collectOpenAIMessages(ProcessStatusIdle, msgs, 2)
	if info.Total != 4 {
		t.Errorf("expected total 4, got %d", info.Total)
	}
	if len(info.Messages) != 2 {
		t.Fatalf("expected 2 messages after offset, got %d", len(info.Messages))
	}
	if info.Messages[0].Role != "user" || *info.Messages[0].Content != "prompt 2" {
		t.Errorf("expected second user prompt, got %+v", info.Messages[0])
	}
}

func TestCollectOpenAIMessages_OffsetBeyondTotal(t *testing.T) {
	msgs := []StreamMessage{
		syntheticUserMessage("prompt"),
	}
	info := collectOpenAIMessages(ProcessStatusIdle, msgs, 100)
	if info.Total != 1 {
		t.Errorf("expected total 1, got %d", info.Total)
	}
	if len(info.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(info.Messages))
	}
}

func TestSyntheticUserMessage(t *testing.T) {
	msg := syntheticUserMessage("test prompt")
	if msg.Type != MessageTypeUser {
		t.Errorf("expected type user, got %s", msg.Type)
	}
	if msg.Text != "test prompt" {
		t.Errorf("expected text 'test prompt', got %s", msg.Text)
	}
	if len(msg.Raw) == 0 {
		t.Error("expected Raw to be populated")
	}

	// Verify the Raw JSON is valid and contains expected structure.
	var env map[string]interface{}
	if err := json.Unmarshal(msg.Raw, &env); err != nil {
		t.Fatalf("Raw is not valid JSON: %v", err)
	}
	if env["type"] != "user" {
		t.Errorf("expected type user in Raw, got %v", env["type"])
	}
}

func TestOpenAIMessage_ContentNullJSON(t *testing.T) {
	// Verify that nil content marshals as null, not as "".
	m := OpenAIMessage{Role: "assistant", ToolCalls: []OpenAIToolCall{
		{ID: "t1", Type: "function", Function: OpenAIFunction{Name: "Read", Arguments: "{}"}},
	}}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	// content should be JSON null.
	if parsed["content"] != nil {
		t.Errorf("expected content to be null, got %v", parsed["content"])
	}
}

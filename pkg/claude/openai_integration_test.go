//go:build integration

package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

// TestOpenAIConversion_Fixture loads a captured stream-json fixture and verifies
// the converter produces valid OpenAI Chat Completions compatible output.
// This test catches regressions when the Claude CLI stream-json format changes.
func TestOpenAIConversion_Fixture(t *testing.T) {
	f, err := os.Open("testdata/stream_json_fixture.jsonl")
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer f.Close()

	var msgs []StreamMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		msg, err := ParseStreamMessage(line)
		if err != nil {
			t.Fatalf("failed to parse fixture line: %v\nline: %s", err, string(line))
		}
		msgs = append(msgs, msg)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	result, meta := ToOpenAIMessages(msgs)

	// --- Metadata assertions ---
	if meta.SessionID != "99737756-11d7-4a2b-9c3e-abc123def456" {
		t.Errorf("metadata.session_id: expected 99737756-..., got %s", meta.SessionID)
	}
	if meta.Model != "claude-opus-4-6" {
		t.Errorf("metadata.model: expected claude-opus-4-6, got %s", meta.Model)
	}
	if meta.ClaudeCodeVersion != "2.1.81" {
		t.Errorf("metadata.claude_code_version: expected 2.1.81, got %s", meta.ClaudeCodeVersion)
	}
	if len(meta.Tools) != 7 {
		t.Errorf("metadata.tools: expected 7, got %d", len(meta.Tools))
	}
	if len(meta.Plugins) != 1 || meta.Plugins[0].Name != "base" {
		t.Errorf("metadata.plugins: expected 1 plugin (base), got %v", meta.Plugins)
	}
	if len(meta.Hooks) != 1 || meta.Hooks[0].HookName != "SessionStart:startup" {
		t.Errorf("metadata.hooks: expected 1 hook, got %v", meta.Hooks)
	}
	if meta.Hooks[0].ExitCode != 0 {
		t.Errorf("metadata.hooks[0].exit_code: expected 0, got %d", meta.Hooks[0].ExitCode)
	}
	if len(meta.Subagents) != 1 || meta.Subagents[0].TaskID != "b5a8kbtpc" {
		t.Errorf("metadata.subagents: expected 1 subagent, got %v", meta.Subagents)
	}
	if meta.CostUSD != 1.43096525 {
		t.Errorf("metadata.cost_usd: expected 1.43096525, got %f", meta.CostUSD)
	}
	if meta.DurationMS != 516519 {
		t.Errorf("metadata.duration_ms: expected 516519, got %f", meta.DurationMS)
	}
	if meta.NumTurns != 62 {
		t.Errorf("metadata.num_turns: expected 62, got %d", meta.NumTurns)
	}
	if meta.Usage == nil || meta.Usage.InputTokens != 1113 {
		t.Errorf("metadata.usage: expected input_tokens 1113, got %v", meta.Usage)
	}

	// --- Message structure assertions ---
	// Expected message sequence:
	// 0: assistant (consolidated: thinking + text + tool_use for Read /etc/hostname)
	// 1: tool (result for Read)
	// 2: assistant (text: "The hostname is...")
	// 3: user (synthetic: "Base directory for this skill...")
	// 4: assistant (consolidated: text + tool_use Write)
	// 5: tool (result for Write)
	// 6: assistant (tool_use Read -- same msg ID but non-consecutive, so separate)
	// 7: tool (result for Read)
	// 8: assistant (final text)

	if len(result) != 9 {
		for i, m := range result {
			t.Logf("  msg[%d]: role=%s content=%v tool_calls=%d tool_call_id=%s",
				i, m.Role, m.Content, len(m.ToolCalls), m.ToolCallID)
		}
		t.Fatalf("expected 9 messages, got %d", len(result))
	}

	// Message 0: consolidated assistant (thinking + text + tool_use)
	m0 := result[0]
	if m0.Role != "assistant" {
		t.Errorf("msg[0].role: expected assistant, got %s", m0.Role)
	}
	if m0.ReasoningContent == "" {
		t.Error("msg[0].reasoning_content: expected non-empty")
	}
	if len(m0.ThinkingBlocks) != 1 {
		t.Errorf("msg[0].thinking_blocks: expected 1, got %d", len(m0.ThinkingBlocks))
	}
	if m0.ThinkingBlocks[0].Signature != "ErcCCkYICxgC" {
		t.Errorf("msg[0].thinking_blocks[0].signature: expected ErcCCkYICxgC, got %s", m0.ThinkingBlocks[0].Signature)
	}
	if m0.Content == nil || *m0.Content != "I'll read the hostname file for you." {
		t.Errorf("msg[0].content: expected text, got %v", m0.Content)
	}
	if len(m0.ToolCalls) != 1 {
		t.Fatalf("msg[0].tool_calls: expected 1, got %d", len(m0.ToolCalls))
	}
	tc0 := m0.ToolCalls[0]
	if tc0.ID != "toolu_012VgxQn" {
		t.Errorf("msg[0].tool_calls[0].id: expected toolu_012VgxQn, got %s", tc0.ID)
	}
	if tc0.Type != "function" {
		t.Errorf("msg[0].tool_calls[0].type: expected function, got %s", tc0.Type)
	}
	if tc0.Function.Name != "Read" {
		t.Errorf("msg[0].tool_calls[0].function.name: expected Read, got %s", tc0.Function.Name)
	}
	// Verify arguments is a JSON string.
	var args0 map[string]interface{}
	if err := json.Unmarshal([]byte(tc0.Function.Arguments), &args0); err != nil {
		t.Fatalf("msg[0].tool_calls[0].function.arguments: not valid JSON: %v", err)
	}
	if args0["file_path"] != "/etc/hostname" {
		t.Errorf("msg[0].tool_calls[0].function.arguments: expected file_path /etc/hostname")
	}

	// Message 1: tool result
	m1 := result[1]
	if m1.Role != "tool" {
		t.Errorf("msg[1].role: expected tool, got %s", m1.Role)
	}
	if m1.ToolCallID != "toolu_012VgxQn" {
		t.Errorf("msg[1].tool_call_id: expected toolu_012VgxQn, got %s", m1.ToolCallID)
	}
	if m1.Content == nil || *m1.Content != "test-host-001" {
		t.Errorf("msg[1].content: expected 'test-host-001', got %v", m1.Content)
	}

	// Message 2: assistant text
	m2 := result[2]
	if m2.Role != "assistant" {
		t.Errorf("msg[2].role: expected assistant, got %s", m2.Role)
	}
	if m2.Content == nil || *m2.Content != "The hostname is `test-host-001`." {
		t.Errorf("msg[2].content: unexpected value %v", m2.Content)
	}

	// Message 3: synthetic user text
	m3 := result[3]
	if m3.Role != "user" {
		t.Errorf("msg[3].role: expected user, got %s", m3.Role)
	}

	// Message 4: consolidated assistant (text + tool_use Write)
	m4 := result[4]
	if m4.Role != "assistant" {
		t.Errorf("msg[4].role: expected assistant, got %s", m4.Role)
	}
	if m4.Content == nil || *m4.Content != "I'll check the Dockerfile now." {
		t.Errorf("msg[4].content: unexpected %v", m4.Content)
	}
	if len(m4.ToolCalls) != 1 {
		t.Errorf("msg[4].tool_calls: expected 1 (Write), got %d", len(m4.ToolCalls))
	}

	// Message 5: tool result for Write
	if result[5].Role != "tool" {
		t.Errorf("msg[5]: expected tool role, got %s", result[5].Role)
	}

	// Message 6: assistant tool_use Read (same msg ID but non-consecutive after tool result)
	m6 := result[6]
	if m6.Role != "assistant" {
		t.Errorf("msg[6].role: expected assistant, got %s", m6.Role)
	}
	if len(m6.ToolCalls) != 1 || m6.ToolCalls[0].Function.Name != "Read" {
		t.Errorf("msg[6]: expected Read tool call, got %v", m6.ToolCalls)
	}

	// Message 7: tool result for Read
	if result[7].Role != "tool" {
		t.Errorf("msg[7]: expected tool role, got %s", result[7].Role)
	}

	// Message 8: final assistant text
	m8 := result[8]
	if m8.Role != "assistant" {
		t.Errorf("msg[8].role: expected assistant, got %s", m8.Role)
	}

	// --- Roundtrip validation: marshal to JSON and verify parseable ---
	envelope := OpenAIMessagesInfo{
		Messages: result,
		Metadata: meta,
		Total:    len(result),
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal envelope: %v", err)
	}

	// Verify the output is valid JSON and can be re-parsed.
	var roundtrip OpenAIMessagesInfo
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("failed to roundtrip parse: %v", err)
	}
	if len(roundtrip.Messages) != len(result) {
		t.Errorf("roundtrip message count mismatch: %d vs %d", len(roundtrip.Messages), len(result))
	}

	// Verify no system or result messages leaked into the messages array.
	for i, m := range result {
		if m.Role != "user" && m.Role != "assistant" && m.Role != "tool" {
			t.Errorf("msg[%d]: unexpected role %q (system/result should be in metadata)", i, m.Role)
		}
	}

	// Log the full output for manual inspection.
	t.Logf("Converted %d stream messages to %d OpenAI messages", len(msgs), len(result))
	t.Logf("Envelope JSON:\n%s", string(data))
}

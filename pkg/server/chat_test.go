package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/giantswarm/klaus/pkg/claude"
)

// chatTestPrompter implements claude.Prompter with configurable behavior
// for testing the chat endpoints. Unlike the existing mockPrompter in
// handlers_test.go, this allows injecting custom RunWithOptions logic
// to simulate streaming messages, busy states, etc.
type chatTestPrompter struct {
	status       claude.StatusInfo
	messagesInfo claude.MessagesInfo
	runFn        func(ctx context.Context, prompt string, opts *claude.RunOptions) (<-chan claude.StreamMessage, error)
	stopped      bool
}

func (p *chatTestPrompter) Run(_ context.Context, _ string) (<-chan claude.StreamMessage, error) {
	ch := make(chan claude.StreamMessage)
	close(ch)
	return ch, nil
}

func (p *chatTestPrompter) RunWithOptions(ctx context.Context, prompt string, opts *claude.RunOptions) (<-chan claude.StreamMessage, error) {
	if p.runFn != nil {
		return p.runFn(ctx, prompt, opts)
	}
	ch := make(chan claude.StreamMessage)
	close(ch)
	return ch, nil
}

func (p *chatTestPrompter) RunSyncWithOptions(_ context.Context, _ string, _ *claude.RunOptions) (string, []claude.StreamMessage, error) {
	return "", nil, nil
}

func (p *chatTestPrompter) Submit(_ context.Context, _ string, _ *claude.RunOptions) error {
	return nil
}

func (p *chatTestPrompter) Status() claude.StatusInfo {
	return p.status
}

func (p *chatTestPrompter) Stop() error {
	p.stopped = true
	return nil
}

func (p *chatTestPrompter) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (p *chatTestPrompter) ResultDetail() claude.ResultDetailInfo {
	return claude.ResultDetailInfo{}
}

func (p *chatTestPrompter) Messages() claude.MessagesInfo {
	return p.messagesInfo
}

func (p *chatTestPrompter) RawMessages(_ int, _ []string) claude.RawMessagesInfo {
	return claude.RawMessagesInfo{Status: p.status.Status}
}

func (p *chatTestPrompter) MarshalStatus() ([]byte, error) {
	return json.Marshal(p.status)
}

func TestHandleChatCompletions_Streaming(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 6)
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "message_start"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "Hello"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: " world"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_stop"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeText, Text: "Hello world"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}

	chunks, gotDone := parseSSEChunks(t, w)

	if !gotDone {
		t.Error("expected [DONE] sentinel")
	}

	// Two content_block_delta chunks + one final chunk with finish_reason "stop".
	// message_start, content_block_stop, assistant, and result are all skipped.
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (2 deltas + 1 final), got %d", len(chunks))
	}

	if chunks[0].Choices[0].Delta.Content != "Hello" {
		t.Errorf("expected first delta content %q, got %q", "Hello", chunks[0].Choices[0].Delta.Content)
	}
	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("expected role on first chunk, got %q", chunks[0].Choices[0].Delta.Role)
	}
	if chunks[1].Choices[0].Delta.Content != " world" {
		t.Errorf("expected second delta content %q, got %q", " world", chunks[1].Choices[0].Delta.Content)
	}
	if chunks[1].Choices[0].Delta.Role != "" {
		t.Errorf("expected empty role on subsequent chunks, got %q", chunks[1].Choices[0].Delta.Role)
	}

	lastChunk := chunks[len(chunks)-1]
	if lastChunk.Choices[0].FinishReason == nil || *lastChunk.Choices[0].FinishReason != "stop" {
		t.Error("expected last chunk finish_reason to be 'stop'")
	}

	for i, chunk := range chunks {
		if chunk.Object != "chat.completion.chunk" {
			t.Errorf("chunk %d: expected object 'chat.completion.chunk', got %q", i, chunk.Object)
		}
	}
}

func TestHandleChatCompletions_StreamingSkipsAssistantMessages(t *testing.T) {
	// With --include-partial-messages, assistant messages are redundant
	// (content already delivered via stream_event deltas) and must be skipped.
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 5)
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "answer"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeToolUse, ToolName: "Read"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeText, Text: "answer"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"read file"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	chunks, _ := parseSSEChunks(t, w)

	// Only 1 stream_event delta + 1 final stop chunk.
	// Both assistant messages (tool_use and text) are skipped.
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (1 delta + 1 final), got %d", len(chunks))
	}

	if chunks[0].Choices[0].Delta.Content != "answer" {
		t.Errorf("expected delta content %q, got %q", "answer", chunks[0].Choices[0].Delta.Content)
	}
}

func TestHandleChatCompletions_StreamingToolUseEvents(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 6)
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "Let me check."}
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_start", ToolUseName: "Read", ToolUseBlockID: "toolu_123"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "The file contains..."}
			ch <- claude.StreamMessage{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeText, Text: "Let me check. The file contains..."}
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"read it"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	chunks, _ := parseSSEChunks(t, w)

	// 2 text deltas + 1 tool_calls + 1 final stop = 4 chunks.
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks (2 deltas + 1 tool + 1 final), got %d", len(chunks))
	}

	if chunks[0].Choices[0].Delta.Content != "Let me check." {
		t.Errorf("expected first delta %q, got %q", "Let me check.", chunks[0].Choices[0].Delta.Content)
	}

	// Tool use event should emit structured tool_calls, not [Using tool: X] text.
	toolDelta := chunks[1].Choices[0].Delta
	if len(toolDelta.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolDelta.ToolCalls))
	}
	if toolDelta.ToolCalls[0].ID != "toolu_123" {
		t.Errorf("expected tool call ID %q, got %q", "toolu_123", toolDelta.ToolCalls[0].ID)
	}
	if toolDelta.ToolCalls[0].Function.Name != "Read" {
		t.Errorf("expected tool name %q, got %q", "Read", toolDelta.ToolCalls[0].Function.Name)
	}
	if toolDelta.ToolCalls[0].Type != "function" {
		t.Errorf("expected tool type %q, got %q", "function", toolDelta.ToolCalls[0].Type)
	}

	if chunks[2].Choices[0].Delta.Content != "The file contains..." {
		t.Errorf("expected second delta %q, got %q", "The file contains...", chunks[2].Choices[0].Delta.Content)
	}
}

func TestHandleChatCompletions_StreamingToolInputDelta(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 5)
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_start", ToolUseName: "Bash", ToolUseBlockID: "toolu_456"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", InputJSONDelta: `{"command":`}
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", InputJSONDelta: `"echo hello"}`}
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"run echo"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	chunks, _ := parseSSEChunks(t, w)

	// 1 tool_calls start + 2 input_json_delta + 1 final = 4 chunks.
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}

	// First chunk: tool call start with name and ID.
	if len(chunks[0].Choices[0].Delta.ToolCalls) != 1 {
		t.Fatal("expected tool_calls in first chunk")
	}
	if chunks[0].Choices[0].Delta.ToolCalls[0].Function.Name != "Bash" {
		t.Errorf("expected tool name Bash, got %q", chunks[0].Choices[0].Delta.ToolCalls[0].Function.Name)
	}

	// Second chunk: input JSON delta.
	if len(chunks[1].Choices[0].Delta.ToolCalls) != 1 {
		t.Fatal("expected tool_calls in second chunk")
	}
	if chunks[1].Choices[0].Delta.ToolCalls[0].Function.Arguments != `{"command":` {
		t.Errorf("expected arguments chunk, got %q", chunks[1].Choices[0].Delta.ToolCalls[0].Function.Arguments)
	}
}

func TestHandleChatCompletions_StreamingToolResult(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 3)
			ch <- claude.StreamMessage{
				Type:    claude.MessageTypeUser,
				Message: json.RawMessage(`{"content":[{"type":"tool_result","tool_use_id":"toolu_789","content":"hello","is_error":false}]}`),
			}
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"test"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	chunks, _ := parseSSEChunks(t, w)

	// 1 tool result + 1 final stop.
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	delta := chunks[0].Choices[0].Delta
	if delta.Role != "tool" {
		t.Errorf("expected role 'tool', got %q", delta.Role)
	}
	if delta.ToolCallID != "toolu_789" {
		t.Errorf("expected tool_call_id 'toolu_789', got %q", delta.ToolCallID)
	}
	if delta.Content != "hello" {
		t.Errorf("expected content 'hello', got %q", delta.Content)
	}
}

func TestHandleChatCompletions_StreamingUsage(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 3)
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "hi"}
			ch <- claude.StreamMessage{
				Type:   claude.MessageTypeResult,
				Result: "done",
				Usage:  &claude.TokenUsage{InputTokens: 100, OutputTokens: 50, CacheReadInputTokens: 200},
			}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	chunks, _ := parseSSEChunks(t, w)

	// 1 delta + 1 final with usage.
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	lastChunk := chunks[len(chunks)-1]
	if lastChunk.Usage == nil {
		t.Fatal("expected usage in final chunk")
	}
	if lastChunk.Usage.PromptTokens != 300 { // 100 input + 200 cache_read
		t.Errorf("expected prompt_tokens 300, got %d", lastChunk.Usage.PromptTokens)
	}
	if lastChunk.Usage.CompletionTokens != 50 {
		t.Errorf("expected completion_tokens 50, got %d", lastChunk.Usage.CompletionTokens)
	}
	if lastChunk.Usage.TotalTokens != 350 {
		t.Errorf("expected total_tokens 350, got %d", lastChunk.Usage.TotalTokens)
	}
}

func TestHandleChatCompletions_StreamingEmptyDeltaSkipped(t *testing.T) {
	// content_block_delta with empty DeltaText should not produce an SSE chunk.
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 3)
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "hi"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: ""}
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"test"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	chunks, _ := parseSSEChunks(t, w)

	// 1 non-empty delta + 1 final stop chunk. Empty delta is skipped.
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Choices[0].Delta.Content != "hi" {
		t.Errorf("expected delta %q, got %q", "hi", chunks[0].Choices[0].Delta.Content)
	}
}

func TestHandleChatCompletions_NonStreamingWithStreamEvents(t *testing.T) {
	// collectResponse should work correctly when stream_event messages are mixed
	// into the channel -- they are naturally ignored by CollectResultText.
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 5)
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "He"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "llo"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeText, Text: "Hello"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "Hello"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	var chatResp chatCompletionResponse
	if err := json.NewDecoder(w.Body).Decode(&chatResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if chatResp.Choices[0].Message.Content != "Hello" {
		t.Errorf("expected content %q, got %q", "Hello", chatResp.Choices[0].Message.Content)
	}
}

func TestStreamMessageToDelta(t *testing.T) {
	tests := []struct {
		name          string
		msg           claude.StreamMessage
		wantNil       bool
		wantText      string
		wantToolCalls bool
		wantToolRole  string
	}{
		{
			name:     "content_block_delta with text",
			msg:      claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: "hello"},
			wantText: "hello",
		},
		{
			name:    "content_block_delta with empty text",
			msg:     claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", DeltaText: ""},
			wantNil: true,
		},
		{
			name:    "message_start event",
			msg:     claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "message_start"},
			wantNil: true,
		},
		{
			name:          "content_block_start tool_use",
			msg:           claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_start", ToolUseName: "Read", ToolUseBlockID: "toolu_abc"},
			wantToolCalls: true,
		},
		{
			name:    "content_block_start text (no tool)",
			msg:     claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_start"},
			wantNil: true,
		},
		{
			name:    "content_block_stop event",
			msg:     claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_stop"},
			wantNil: true,
		},
		{
			name:    "message_stop event",
			msg:     claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "message_stop"},
			wantNil: true,
		},
		{
			name:    "assistant text message",
			msg:     claude.StreamMessage{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeText, Text: "full text"},
			wantNil: true,
		},
		{
			name:    "assistant tool_use message",
			msg:     claude.StreamMessage{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeToolUse, ToolName: "Bash"},
			wantNil: true,
		},
		{
			name:    "result message",
			msg:     claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"},
			wantNil: true,
		},
		{
			name:    "system message",
			msg:     claude.StreamMessage{Type: claude.MessageTypeSystem, SessionID: "sess-1"},
			wantNil: true,
		},
		{
			name:          "input_json_delta",
			msg:           claude.StreamMessage{Type: claude.MessageTypeStreamEvent, EventType: "content_block_delta", InputJSONDelta: `{"cmd":"ls"}`},
			wantToolCalls: true,
		},
		{
			name: "user tool_result message",
			msg: claude.StreamMessage{
				Type:    claude.MessageTypeUser,
				Message: json.RawMessage(`{"content":[{"type":"tool_result","tool_use_id":"toolu_xyz","content":"output","is_error":false}]}`),
			},
			wantToolRole: "tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolIndex := 0
			got := streamMessageToDelta(tt.msg, &toolIndex)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if tt.wantToolCalls {
				if len(got.ToolCalls) == 0 {
					t.Error("expected tool_calls to be present")
				}
				if got.Role != "assistant" {
					t.Errorf("expected role 'assistant', got %q", got.Role)
				}
				return
			}
			if tt.wantToolRole != "" {
				if got.Role != tt.wantToolRole {
					t.Errorf("expected role %q, got %q", tt.wantToolRole, got.Role)
				}
				return
			}
			if got.Role != "assistant" {
				t.Errorf("expected role %q, got %q", "assistant", got.Role)
			}
			if got.Content != tt.wantText {
				t.Errorf("expected content %q, got %q", tt.wantText, got.Content)
			}
		})
	}
}

func TestHandleChatCompletions_NonStreaming(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 2)
			ch <- claude.StreamMessage{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeText, Text: "Hello world"}
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "Hello world"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var chatResp chatCompletionResponse
	if err := json.NewDecoder(w.Body).Decode(&chatResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if chatResp.Object != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got %q", chatResp.Object)
	}

	if len(chatResp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chatResp.Choices))
	}

	choice := chatResp.Choices[0]
	if choice.Message == nil {
		t.Fatal("expected message in choice")
	}
	if choice.Message.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", choice.Message.Role)
	}
	if choice.Message.Content != "Hello world" {
		t.Errorf("expected content %q, got %q", "Hello world", choice.Message.Content)
	}
	if choice.FinishReason == nil || *choice.FinishReason != "stop" {
		t.Error("expected finish_reason 'stop'")
	}
}

func TestHandleChatCompletions_NonStreamingUsage(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 2)
			ch <- claude.StreamMessage{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeText, Text: "hi"}
			ch <- claude.StreamMessage{
				Type:   claude.MessageTypeResult,
				Result: "hi",
				Usage:  &claude.TokenUsage{InputTokens: 10, OutputTokens: 5},
			}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	var chatResp chatCompletionResponse
	if err := json.NewDecoder(w.Body).Decode(&chatResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if chatResp.Usage == nil {
		t.Fatal("expected usage in response")
	}
	if chatResp.Usage.PromptTokens != 10 {
		t.Errorf("expected prompt_tokens 10, got %d", chatResp.Usage.PromptTokens)
	}
	if chatResp.Usage.CompletionTokens != 5 {
		t.Errorf("expected completion_tokens 5, got %d", chatResp.Usage.CompletionTokens)
	}
}

func TestHandleChatCompletions_BusyReturns429(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusBusy},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			return nil, claude.ErrBusy
		},
	}

	body := `{"messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestHandleChatCompletions_InvalidBody(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid body, got %d", w.Code)
	}
}

func TestHandleChatCompletions_NoUserMessage(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
	}

	body := `{"messages":[{"role":"system","content":"you are helpful"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for no user message, got %d", w.Code)
	}
}

func TestHandleChatCompletions_MethodNotAllowed(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET on completions, got %d", w.Code)
	}
}

func TestHandleChatCompletions_DefaultModel(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 1)
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	var chatResp chatCompletionResponse
	if err := json.NewDecoder(w.Body).Decode(&chatResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if chatResp.Model != "klaus" {
		t.Errorf("expected default model 'klaus', got %q", chatResp.Model)
	}
}

func TestHandleChatCompletions_CustomModel(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			ch := make(chan claude.StreamMessage, 1)
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"}
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"hi"}],"stream":false,"model":"claude-sonnet"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	var chatResp chatCompletionResponse
	if err := json.NewDecoder(w.Body).Decode(&chatResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if chatResp.Model != "claude-sonnet" {
		t.Errorf("expected model 'claude-sonnet', got %q", chatResp.Model)
	}
}

func TestHandleChatCompletions_UsesLastUserMessage(t *testing.T) {
	var capturedPrompt string
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, prompt string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			capturedPrompt = prompt
			ch := make(chan claude.StreamMessage)
			close(ch)
			return ch, nil
		},
	}

	body := `{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"reply"},{"role":"user","content":"second"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleChatCompletions(prompter)(w, req)

	if capturedPrompt != "second" {
		t.Errorf("expected prompt to be last user message %q, got %q", "second", capturedPrompt)
	}
}

func TestHandleChatCompletions_ContinueSessionOnSubsequentCalls(t *testing.T) {
	var capturedOpts []*claude.RunOptions
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		runFn: func(_ context.Context, _ string, opts *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			capturedOpts = append(capturedOpts, opts)
			ch := make(chan claude.StreamMessage, 1)
			ch <- claude.StreamMessage{Type: claude.MessageTypeResult, Result: "done"}
			close(ch)
			return ch, nil
		},
	}

	handler := handleChatCompletions(prompter)

	// First call: no ContinueSession.
	body := `{"messages":[{"role":"user","content":"first"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler(w, req)

	if capturedOpts[0] != nil {
		t.Error("expected nil RunOptions on first call")
	}

	// Second call: should have ContinueSession.
	body = `{"messages":[{"role":"user","content":"second"}],"stream":false}`
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w = httptest.NewRecorder()
	handler(w, req)

	if capturedOpts[1] == nil || !capturedOpts[1].ContinueSession {
		t.Error("expected ContinueSession=true on second call")
	}
}

func TestHandleChatMessages(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		messagesInfo: claude.MessagesInfo{
			Status: claude.ProcessStatusIdle,
			Messages: []claude.MessageSummary{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "Hi there!"},
				{Role: "", Content: ""},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/messages", nil)
	w := httptest.NewRecorder()

	handleChatMessages(prompter)(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var msgResp chatMessagesResponse
	if err := json.NewDecoder(w.Body).Decode(&msgResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if msgResp.Status != "idle" {
		t.Errorf("expected status 'idle', got %q", msgResp.Status)
	}

	if len(msgResp.Messages) != 2 {
		t.Fatalf("expected 2 messages (empty filtered), got %d", len(msgResp.Messages))
	}
	if msgResp.Messages[0].Role != "user" || msgResp.Messages[0].Content != "hello" {
		t.Errorf("unexpected first message: %+v", msgResp.Messages[0])
	}
	if msgResp.Messages[1].Role != "assistant" || msgResp.Messages[1].Content != "Hi there!" {
		t.Errorf("unexpected second message: %+v", msgResp.Messages[1])
	}
}

func TestHandleChatMessages_StructuredToolCalls(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		messagesInfo: claude.MessagesInfo{
			Status: claude.ProcessStatusIdle,
			Messages: []claude.MessageSummary{
				{
					Role:    "assistant",
					Content: "Using tool: Bash",
					ToolCalls: []claude.ToolCallInfo{{
						ID:   "toolu_abc",
						Name: "Bash",
						Args: json.RawMessage(`{"command":"echo hi"}`),
					}},
				},
				{
					Role:       "tool",
					Content:    "hi",
					ToolCallID: "toolu_abc",
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/messages", nil)
	w := httptest.NewRecorder()

	handleChatMessages(prompter)(w, req)

	var msgResp chatMessagesResponse
	if err := json.NewDecoder(w.Body).Decode(&msgResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(msgResp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgResp.Messages))
	}

	// First message: assistant with tool_calls.
	msg0 := msgResp.Messages[0]
	if len(msg0.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg0.ToolCalls))
	}
	if msg0.ToolCalls[0].ID != "toolu_abc" {
		t.Errorf("expected tool call ID 'toolu_abc', got %q", msg0.ToolCalls[0].ID)
	}
	if msg0.ToolCalls[0].Function.Name != "Bash" {
		t.Errorf("expected tool name 'Bash', got %q", msg0.ToolCalls[0].Function.Name)
	}
	if msg0.ToolCalls[0].Function.Arguments != `{"command":"echo hi"}` {
		t.Errorf("expected tool arguments, got %q", msg0.ToolCalls[0].Function.Arguments)
	}

	// Second message: tool result with tool_call_id.
	msg1 := msgResp.Messages[1]
	if msg1.Role != "tool" {
		t.Errorf("expected role 'tool', got %q", msg1.Role)
	}
	if msg1.ToolCallID != "toolu_abc" {
		t.Errorf("expected tool_call_id 'toolu_abc', got %q", msg1.ToolCallID)
	}
}

func TestHandleChatMessages_Empty(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
		messagesInfo: claude.MessagesInfo{
			Status: claude.ProcessStatusIdle,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/messages", nil)
	w := httptest.NewRecorder()

	handleChatMessages(prompter)(w, req)

	var msgResp chatMessagesResponse
	if err := json.NewDecoder(w.Body).Decode(&msgResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(msgResp.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgResp.Messages))
	}
}

func TestHandleChatMessages_MethodNotAllowed(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusIdle},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", strings.NewReader("{}"))
	w := httptest.NewRecorder()

	handleChatMessages(prompter)(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST on messages, got %d", w.Code)
	}
}

// parseSSEChunks scans the recorded response body for SSE data lines and returns
// the parsed chat completion chunks along with whether a [DONE] sentinel was seen.
func parseSSEChunks(t *testing.T, w *httptest.ResponseRecorder) ([]chatCompletionResponse, bool) {
	t.Helper()
	scanner := bufio.NewScanner(w.Body)
	var chunks []chatCompletionResponse
	var gotDone bool
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if line == "data: [DONE]" {
			gotDone = true
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			var chunk chatCompletionResponse
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
				t.Fatalf("failed to unmarshal SSE chunk: %v", err)
			}
			chunks = append(chunks, chunk)
		}
	}
	return chunks, gotDone
}

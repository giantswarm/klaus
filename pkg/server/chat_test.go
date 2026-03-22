package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
	if chunks[1].Choices[0].Delta.Content != " world" {
		t.Errorf("expected second delta content %q, got %q", " world", chunks[1].Choices[0].Delta.Content)
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
		name     string
		msg      claude.StreamMessage
		wantNil  bool
		wantText string
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamMessageToDelta(tt.msg)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
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

func TestHandleChatCompletions_BusyReturns429(t *testing.T) {
	prompter := &chatTestPrompter{
		status: claude.StatusInfo{Status: claude.ProcessStatusBusy},
		runFn: func(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
			return nil, fmt.Errorf("claude process is already busy")
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

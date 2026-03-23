package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	claudepkg "github.com/giantswarm/klaus/pkg/claude"
)

// OpenAI-compatible request/response types for the chat completions endpoint.

type chatCompletionRequest struct {
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Model    string        `json:"model,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role,omitempty"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type chatCompletionResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []chatChoice       `json:"choices"`
	Usage   *chatCompletionUse `json:"usage,omitempty"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      *chatMessage `json:"message,omitempty"`
	Delta        *chatMessage `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type chatCompletionUse struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// handleChatCompletions returns an http.HandlerFunc that accepts an
// OpenAI-compatible chat completion request and streams the response as SSE
// (or returns a complete response for non-streaming requests).
//
// Conversation history accumulates across calls: the second and subsequent
// requests use ContinueSession so that Claude sees the full conversation.
func handleChatCompletions(process claudepkg.Prompter) http.HandlerFunc {
	callCount := 0

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}

		prompt := extractLastUserMessage(req.Messages)
		if prompt == "" {
			http.Error(w, "no user message found", http.StatusBadRequest)
			return
		}

		var runOpts *claudepkg.RunOptions
		if callCount > 0 {
			runOpts = &claudepkg.RunOptions{ContinueSession: true}
		}

		ch, err := process.RunWithOptions(r.Context(), prompt, runOpts)
		if err != nil {
			if errors.Is(err, claudepkg.ErrBusy) {
				http.Error(w, "agent is busy", http.StatusTooManyRequests)
				return
			}
			http.Error(w, "failed to start prompt: "+err.Error(), http.StatusInternalServerError)
			return
		}

		callCount++

		model := req.Model
		if model == "" {
			model = "klaus"
		}

		if req.Stream {
			streamResponse(w, r, process, ch, model)
		} else {
			collectResponse(w, ch, model)
		}
	}
}

// streamResponse writes SSE events in the OpenAI streaming delta format.
func streamResponse(w http.ResponseWriter, r *http.Request, process claudepkg.Prompter, ch <-chan claudepkg.StreamMessage, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Disable the server's write timeout for this long-lived SSE stream.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Printf("[chat] failed to disable write deadline: %v", err)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	sentRole := false
	var usage *chatCompletionUse
	toolIndex := 0

	for {
		select {
		case <-r.Context().Done():
			if err := process.Stop(); err != nil {
				log.Printf("[chat] failed to stop process on client disconnect: %v", err)
			}
			return
		case msg, ok := <-ch:
			if !ok {
				writeDoneChunk(w, flusher, id, model, usage)
				return
			}

			delta := streamMessageToDelta(msg, &toolIndex)
			if delta == nil {
				// Capture usage from result messages even though we don't emit a delta.
				if msg.Type == claudepkg.MessageTypeResult {
					usage = collectUsage(msg, usage)
				}
				continue
			}

			if sentRole {
				delta.Role = ""
			} else {
				sentRole = true
			}

			chunk := chatCompletionResponse{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   model,
				Choices: []chatChoice{{Index: 0, Delta: delta}},
			}

			data, err := json.Marshal(chunk)
			if err != nil {
				log.Printf("[chat] failed to marshal SSE chunk: %v", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// writeDoneChunk sends the final SSE chunk with finish_reason "stop" and the [DONE] sentinel.
func writeDoneChunk(w http.ResponseWriter, flusher http.Flusher, id, model string, usage *chatCompletionUse) {
	stop := "stop"
	final := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatChoice{{Index: 0, Delta: &chatMessage{}, FinishReason: &stop}},
		Usage:   usage,
	}
	data, err := json.Marshal(final)
	if err != nil {
		log.Printf("[chat] failed to marshal final SSE chunk: %v", err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// collectUsage extracts token usage from a result StreamMessage and merges it
// with any previously accumulated usage.
func collectUsage(msg claudepkg.StreamMessage, existing *chatCompletionUse) *chatCompletionUse {
	if msg.Usage == nil {
		return existing
	}
	u := &chatCompletionUse{
		PromptTokens:     int(msg.Usage.InputTokens + msg.Usage.CacheReadInputTokens),
		CompletionTokens: int(msg.Usage.OutputTokens),
	}
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	return u
}

// collectResponse drains the message channel and returns a single non-streaming response.
func collectResponse(w http.ResponseWriter, ch <-chan claudepkg.StreamMessage, model string) {
	var msgs []claudepkg.StreamMessage
	for msg := range ch {
		msgs = append(msgs, msg)
	}

	resultText := claudepkg.CollectResultText(msgs)

	stop := "stop"
	resp := chatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      &chatMessage{Role: "assistant", Content: resultText},
			FinishReason: &stop,
		}},
	}

	// Include usage from the result message.
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Type == claudepkg.MessageTypeResult {
			resp.Usage = collectUsage(msgs[i], nil)
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[chat] failed to encode response: %v", err)
	}
}

// streamMessageToDelta converts a StreamMessage to an OpenAI chat delta.
// Returns nil for message types that should be skipped.
//
// With --include-partial-messages, content arrives token-by-token as
// stream_event / content_block_delta messages. The full assistant message
// that follows is redundant and must be skipped to avoid duplication.
//
// Tool use events are emitted as delta.tool_calls per the OpenAI spec.
// Tool results from user messages are emitted with role "tool" and tool_call_id.
func streamMessageToDelta(msg claudepkg.StreamMessage, toolIndex *int) *chatMessage {
	switch msg.Type {
	case claudepkg.MessageTypeStreamEvent:
		if msg.EventType == "content_block_delta" && msg.DeltaText != "" {
			return &chatMessage{Role: "assistant", Content: msg.DeltaText}
		}
		if msg.EventType == "content_block_delta" && msg.InputJSONDelta != "" {
			idx := *toolIndex
			if idx > 0 {
				idx-- // point to the current tool call
			}
			return &chatMessage{
				Role: "assistant",
				ToolCalls: []chatToolCall{{
					Index:    idx,
					Function: chatToolFunction{Arguments: msg.InputJSONDelta},
				}},
			}
		}
		if msg.EventType == "content_block_start" && msg.ToolUseName != "" {
			idx := *toolIndex
			*toolIndex++
			return &chatMessage{
				Role: "assistant",
				ToolCalls: []chatToolCall{{
					Index: idx,
					ID:    msg.ToolUseBlockID,
					Type:  "function",
					Function: chatToolFunction{
						Name: msg.ToolUseName,
					},
				}},
			}
		}
		return nil
	case claudepkg.MessageTypeUser:
		blocks := claudepkg.ExtractToolResults(msg)
		if len(blocks) == 0 {
			return nil
		}
		block := blocks[0]
		return &chatMessage{
			Role:       "tool",
			Content:    block.Content,
			ToolCallID: block.ToolUseID,
		}
	case claudepkg.MessageTypeAssistant:
		return nil
	case claudepkg.MessageTypeResult:
		return nil
	}
	return nil
}

// extractLastUserMessage returns the content of the last message with role "user".
func extractLastUserMessage(messages []chatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

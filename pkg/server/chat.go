package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
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
	Role    string `json:"role"`
	Content string `json:"content"`
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

type chatMessagesResponse struct {
	Status   string        `json:"status"`
	Messages []chatMessage `json:"messages"`
}

// handleChatCompletions returns an http.HandlerFunc that accepts an
// OpenAI-compatible chat completion request and streams the response as SSE
// (or returns a complete response for non-streaming requests).
func handleChatCompletions(process claudepkg.Prompter) http.HandlerFunc {
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

		ch, err := process.RunWithOptions(r.Context(), prompt, nil)
		if err != nil {
			if strings.Contains(err.Error(), "already busy") {
				http.Error(w, "agent is busy", http.StatusTooManyRequests)
				return
			}
			http.Error(w, "failed to start prompt: "+err.Error(), http.StatusInternalServerError)
			return
		}

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

	for {
		select {
		case <-r.Context().Done():
			if err := process.Stop(); err != nil {
				log.Printf("[chat] failed to stop process on client disconnect: %v", err)
			}
			return
		case msg, ok := <-ch:
			if !ok {
				writeDoneChunk(w, flusher, id, model)
				return
			}

			delta := streamMessageToDelta(msg)
			if delta == nil {
				continue
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
func writeDoneChunk(w http.ResponseWriter, flusher http.Flusher, id, model string) {
	stop := "stop"
	final := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatChoice{{Index: 0, Delta: &chatMessage{}, FinishReason: &stop}},
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
func streamMessageToDelta(msg claudepkg.StreamMessage) *chatMessage {
	switch msg.Type {
	case claudepkg.MessageTypeStreamEvent:
		if msg.EventType == "content_block_delta" && msg.DeltaText != "" {
			return &chatMessage{Role: "assistant", Content: msg.DeltaText}
		}
		return nil
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

// handleChatMessages returns an http.HandlerFunc that returns the conversation
// history as a JSON array of {role, content} messages.
func handleChatMessages(process claudepkg.Prompter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		info := process.Messages()

		messages := make([]chatMessage, 0, len(info.Messages))
		for _, m := range info.Messages {
			if m.Content == "" {
				continue
			}
			messages = append(messages, chatMessage{
				Role:    m.Role,
				Content: m.Content,
			})
		}

		resp := chatMessagesResponse{
			Status:   string(info.Status),
			Messages: messages,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("[chat] failed to encode messages response: %v", err)
		}
	}
}

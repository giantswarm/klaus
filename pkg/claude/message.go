package claude

import (
	"context"
	"encoding/json"
)

// MessageType identifies the kind of message in the stream-json protocol.
type MessageType string

const (
	MessageTypeSystem    MessageType = "system"
	MessageTypeAssistant MessageType = "assistant"
	MessageTypeResult    MessageType = "result"
)

// MessageSubtype identifies the subtype of an assistant message.
type MessageSubtype string

const (
	SubtypeText    MessageSubtype = "text"
	SubtypeToolUse MessageSubtype = "tool_use"
)

// StreamMessage is the top-level envelope for all stream-json messages
// emitted by the Claude CLI on stdout.
type StreamMessage struct {
	Type    MessageType     `json:"type"`
	Subtype MessageSubtype  `json:"subtype,omitempty"`
	Message json.RawMessage `json:"message,omitempty"`

	// Fields present on "system" messages.
	SessionID string `json:"session_id,omitempty"`

	// Fields present on "assistant" messages with subtype "text".
	Text string `json:"text,omitempty"`

	// Fields present on "assistant" messages with subtype "tool_use".
	ToolName string          `json:"tool_name,omitempty"`
	ToolID   string          `json:"tool_id,omitempty"`
	ToolArgs json.RawMessage `json:"tool_args,omitempty"`

	// Fields present on "result" messages.
	Result   string  `json:"result,omitempty"`
	Duration float64 `json:"duration_ms,omitempty"`
	Cost     float64 `json:"cost_usd,omitempty"`
	IsError  bool    `json:"is_error,omitempty"`

	// TotalCost tracks the running total cost of the session.
	TotalCost float64 `json:"total_cost_usd,omitempty"`

	// Raw holds the original JSON for messages we don't fully parse.
	Raw json.RawMessage `json:"-"`
}

// ParseStreamMessage unmarshals a single line of stream-json output.
func ParseStreamMessage(data []byte) (StreamMessage, error) {
	var msg StreamMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return msg, err
	}
	msg.Raw = make(json.RawMessage, len(data))
	copy(msg.Raw, data)
	return msg, nil
}

type ProcessStatus string

const (
	ProcessStatusStarting  ProcessStatus = "starting"
	ProcessStatusIdle      ProcessStatus = "idle"
	ProcessStatusBusy      ProcessStatus = "busy"
	ProcessStatusCompleted ProcessStatus = "completed"
	ProcessStatusStopped   ProcessStatus = "stopped"
	ProcessStatusError     ProcessStatus = "error"
)

// maxStatusResultLen is the maximum number of runes included in the
// StatusInfo.Result field. Longer results are truncated with "...";
// use the result debug tool for the full untruncated text.
const maxStatusResultLen = 4000

type StatusInfo struct {
	Status        ProcessStatus `json:"status"`
	SessionID     string        `json:"session_id,omitempty"`
	ErrorMessage  string        `json:"error,omitempty"`
	TotalCost     float64       `json:"total_cost_usd,omitempty"`
	MessageCount  int           `json:"message_count,omitempty"`
	ToolCallCount int           `json:"tool_call_count,omitempty"`
	LastMessage   string        `json:"last_message,omitempty"`
	LastToolName  string        `json:"last_tool_name,omitempty"`
	// Result contains the agent's final output text from the last completed
	// non-blocking Submit run, truncated to maxStatusResultLen runes. It is
	// populated when the status is "completed". Use the result debug tool
	// for the full untruncated text.
	//
	// The result persists until the next Submit call clears it (along with
	// resetting the status to "busy"). There is no explicit "consumed"
	// acknowledgement; callers should track whether they have already
	// processed a given result.
	Result string `json:"result,omitempty"`
}

// ResultDetailInfo contains the full untruncated result and detailed metadata
// from the last completed run. Intended for debugging and troubleshooting.
// Unlike StatusInfo.Result, ResultText is never truncated.
type ResultDetailInfo struct {
	ResultText   string          `json:"result_text"`
	Messages     []StreamMessage `json:"messages,omitempty"`
	MessageCount int             `json:"message_count"`
	TotalCost    float64         `json:"total_cost_usd,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	Status       ProcessStatus   `json:"status"`
	ErrorMessage string          `json:"error,omitempty"`
}

// resultState holds the output of the last completed Submit run.
// Access must be synchronized by the parent process's mutex.
type resultState struct {
	text      string
	messages  []StreamMessage
	completed bool // true after the drain goroutine finishes; false when cleared
}

// submitDrain starts a background goroutine that reads all messages from ch,
// collects the result text, and calls storeFn with the result when complete.
// ctx controls the drain goroutine's lifetime.
func submitDrain(ctx context.Context, ch <-chan StreamMessage, storeFn func(string, []StreamMessage)) {
	go func() {
		var messages []StreamMessage
		for {
			select {
			case <-ctx.Done():
				storeFn(CollectResultText(messages), messages)
				return
			case msg, ok := <-ch:
				if !ok {
					storeFn(CollectResultText(messages), messages)
					return
				}
				messages = append(messages, msg)
			}
		}
	}()
}

// submitAsync is the shared implementation for non-blocking prompt submission.
// It calls runFn to start the prompt; on success it clears previous results
// and spawns a background drain goroutine that stores new results via setResult.
// Previous results are preserved if runFn fails (e.g. "already busy").
func submitAsync(
	ctx context.Context,
	prompt string,
	opts *RunOptions,
	runFn func(context.Context, string, *RunOptions) (<-chan StreamMessage, error),
	setResult func(resultState),
) error {
	ch, err := runFn(ctx, prompt, opts)
	if err != nil {
		return err
	}

	setResult(resultState{}) // Clear previous result now that the new run started.

	submitDrain(ctx, ch, func(text string, messages []StreamMessage) {
		setResult(resultState{text: text, messages: messages, completed: true})
	})
	return nil
}

// Truncate returns s truncated to maxLen runes with "..." appended if truncated.
// It operates on runes to ensure clean truncation of multi-byte UTF-8 strings.
func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// CollectResultText extracts the result text from a completed set of stream messages.
// It returns the text from the last result message, falling back to concatenated
// assistant text messages if no result message contains text.
func CollectResultText(messages []StreamMessage) string {
	// Try to get result from the last result message.
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Type == MessageTypeResult && messages[i].Result != "" {
			return messages[i].Result
		}
	}
	// Fallback: concatenate assistant text messages.
	var text string
	for _, msg := range messages {
		if msg.Type == MessageTypeAssistant && msg.Subtype == SubtypeText {
			text += msg.Text
		}
	}
	return text
}

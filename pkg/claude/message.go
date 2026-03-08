package claude

import (
	"context"
	"encoding/json"
	"regexp"
	"time"
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

// TokenUsage holds per-message token counts from the Claude API usage object.
type TokenUsage struct {
	InputTokens              int64 `json:"input_tokens,omitempty"`
	OutputTokens             int64 `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

// StreamMessage is the top-level envelope for all stream-json messages
// emitted by the Claude CLI on stdout.
type StreamMessage struct {
	Type      MessageType     `json:"type"`
	Subtype   MessageSubtype  `json:"subtype,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`

	// Fields present on "system" messages.
	SessionID string `json:"session_id,omitempty"`

	// Fields present on "assistant" messages with subtype "text".
	Text string `json:"text,omitempty"`

	// Fields present on "assistant" messages with subtype "tool_use".
	ToolName string          `json:"tool_name,omitempty"`
	ToolID   string          `json:"tool_id,omitempty"`
	ToolArgs json.RawMessage `json:"tool_args,omitempty"`

	// Usage holds token counts from the Claude API, present on "assistant" messages.
	Usage *TokenUsage `json:"usage,omitempty"`

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
// It stamps each message with the current UTC time in RFC3339 format.
func ParseStreamMessage(data []byte) (StreamMessage, error) {
	var msg StreamMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return msg, err
	}
	msg.Timestamp = time.Now().UTC().Format(time.RFC3339)
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

// AllProcessStatuses is the canonical list of all ProcessStatus values.
// Used by the metrics package to initialise the process_status gauge.
var AllProcessStatuses = []ProcessStatus{
	ProcessStatusStarting,
	ProcessStatusIdle,
	ProcessStatusBusy,
	ProcessStatusCompleted,
	ProcessStatusStopped,
	ProcessStatusError,
}

// maxStatusResultLen is the maximum number of runes included in the
// StatusInfo.Result field. Longer results are truncated with "...";
// use the result debug tool for the full untruncated text.
const maxStatusResultLen = 4000

type StatusInfo struct {
	Status        ProcessStatus  `json:"status"`
	SessionID     string         `json:"session_id,omitempty"`
	ErrorMessage  string         `json:"error,omitempty"`
	TotalCost     *float64       `json:"total_cost_usd"`
	MessageCount  int            `json:"message_count,omitempty"`
	ToolCallCount int            `json:"tool_call_count,omitempty"`
	ToolCalls     map[string]int `json:"tool_calls,omitempty"`
	TokenUsage    *TokenUsage    `json:"token_usage,omitempty"`
	LastMessage   string         `json:"last_message,omitempty"`
	LastToolName  string         `json:"last_tool_name,omitempty"`
	// SubagentCalls tracks subagent dispatches via the Task/Agent tool.
	SubagentCalls []SubagentCall `json:"subagent_calls,omitempty"`
	ModelUsage    map[string]int `json:"model_usage,omitempty"`
	ErrorCount    int            `json:"error_count,omitempty"`
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
	ResultText    string          `json:"result_text"`
	Messages      []StreamMessage `json:"messages,omitempty"`
	MessageCount  int             `json:"message_count"`
	ToolCalls     map[string]int  `json:"tool_calls,omitempty"`
	SubagentCalls []SubagentCall  `json:"subagent_calls,omitempty"`
	ModelUsage    map[string]int  `json:"model_usage,omitempty"`
	PRURLs        []string        `json:"pr_urls,omitempty"`
	ErrorCount    int             `json:"error_count,omitempty"`
	TokenUsage    *TokenUsage     `json:"token_usage,omitempty"`
	TotalCost     *float64        `json:"total_cost_usd"`
	SessionID     string          `json:"session_id,omitempty"`
	Status        ProcessStatus   `json:"status"`
	ErrorMessage  string          `json:"error,omitempty"`
}

// Float64Ptr returns a pointer to the given float64 value.
func Float64Ptr(f float64) *float64 {
	return &f
}

// SubagentCall tracks a single subagent dispatch via the Task/Agent tool.
type SubagentCall struct {
	Type        string  `json:"type"`
	Description string  `json:"description,omitempty"`
	ToolID      string  `json:"tool_id,omitempty"`
	ToolCalls   int     `json:"tool_calls,omitempty"`
	Tokens      int     `json:"tokens,omitempty"`
	DurationMS  float64 `json:"duration_ms,omitempty"`
	Status      string  `json:"status,omitempty"`
}

// messageModelEnvelope is used for partial-parsing msg.Message to extract the model field.
type messageModelEnvelope struct {
	Model string `json:"model"`
}

// messageContentEnvelope is used for parsing the content array from msg.Message.
type messageContentEnvelope struct {
	Content []ToolResultBlock `json:"content"`
}

// ToolResultBlock represents a single content block from a user message (tool result).
type ToolResultBlock struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// prURLPattern matches GitHub pull request URLs in tool result content.
var prURLPattern = regexp.MustCompile(`https://github\.com/[\w.\-]+/[\w.\-]+/pull/\d+`)

// extractModel parses the model field from a StreamMessage's raw Message JSON.
// Returns an empty string if the message has no model field or cannot be parsed.
func extractModel(msg StreamMessage) string {
	if len(msg.Message) == 0 {
		return ""
	}
	var env messageModelEnvelope
	if err := json.Unmarshal(msg.Message, &env); err != nil {
		return ""
	}
	return env.Model
}

// ExtractToolResults parses tool_result content blocks from a StreamMessage's raw Message JSON.
// Returns nil if the message has no content blocks or cannot be parsed.
func ExtractToolResults(msg StreamMessage) []ToolResultBlock {
	if len(msg.Message) == 0 {
		return nil
	}
	var env messageContentEnvelope
	if err := json.Unmarshal(msg.Message, &env); err != nil {
		return nil
	}
	var results []ToolResultBlock
	for _, block := range env.Content {
		if block.Type == "tool_result" {
			results = append(results, block)
		}
	}
	return results
}

// extractPRURLs returns all unique GitHub PR URLs found in the given text.
func extractPRURLs(text string) []string {
	return prURLPattern.FindAllString(text, -1)
}

// countErrors returns the number of tool_result blocks with is_error set to true.
func countErrors(blocks []ToolResultBlock) int {
	n := 0
	for _, b := range blocks {
		if b.IsError {
			n++
		}
	}
	return n
}

// appendUnique appends values to dst that are not already present.
func appendUnique(dst []string, values ...string) []string {
	seen := make(map[string]bool, len(dst))
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range values {
		if !seen[v] {
			dst = append(dst, v)
			seen[v] = true
		}
	}
	return dst
}

// copyStringSlice returns a copy of the slice. Returns nil for nil/empty input.
func copyStringSlice(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	cp := make([]string, len(s))
	copy(cp, s)
	return cp
}

// CollectModelUsage extracts model usage counts from a set of messages.
func CollectModelUsage(messages []StreamMessage) map[string]int {
	usage := make(map[string]int)
	for _, msg := range messages {
		if msg.Type == MessageTypeAssistant {
			if model := extractModel(msg); model != "" {
				usage[model]++
			}
		}
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}

// CollectPRURLs extracts unique GitHub PR URLs from tool_result content blocks.
func CollectPRURLs(messages []StreamMessage) []string {
	var urls []string
	for _, msg := range messages {
		blocks := ExtractToolResults(msg)
		for _, block := range blocks {
			found := extractPRURLs(block.Content)
			urls = appendUnique(urls, found...)
		}
	}
	if len(urls) == 0 {
		return nil
	}
	return urls
}

// CollectErrorCount counts tool_result blocks with is_error set to true.
func CollectErrorCount(messages []StreamMessage) int {
	n := 0
	for _, msg := range messages {
		blocks := ExtractToolResults(msg)
		n += countErrors(blocks)
	}
	return n
}

// copyToolCalls returns a shallow copy of the map to avoid exposing internal
// state to callers. Returns nil for nil input.
func copyToolCalls(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	cp := make(map[string]int, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
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

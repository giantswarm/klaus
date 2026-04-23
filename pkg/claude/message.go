package claude

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

// ErrBusy is returned when a prompt is submitted while the process is already
// handling another prompt.
var ErrBusy = errors.New("claude process is already busy")

// MessageType identifies the kind of message in the stream-json protocol.
type MessageType string

const (
	MessageTypeSystem      MessageType = "system"
	MessageTypeAssistant   MessageType = "assistant"
	MessageTypeUser        MessageType = "user"
	MessageTypeResult      MessageType = "result"
	MessageTypeStreamEvent MessageType = "stream_event"
)

// MessageSubtype identifies the subtype of an assistant message.
type MessageSubtype string

const (
	SubtypeText    MessageSubtype = "text"
	SubtypeToolUse MessageSubtype = "tool_use"
)

// Stream event type names emitted by the Claude CLI inside stream_event envelopes.
const (
	EventContentBlockStart = "content_block_start"
	EventContentBlockDelta = "content_block_delta"
	EventContentBlockStop  = "content_block_stop"
)

// Tool name for the bash command execution tool.
const ToolNameBash = "Bash"

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

	// Fields present on "stream_event" messages (--include-partial-messages).
	EventType      string `json:"-"`
	DeltaText      string `json:"-"`
	ToolUseName    string `json:"-"` // tool name from content_block_start with type "tool_use"
	ToolUseBlockID string `json:"-"` // tool use ID from content_block_start with type "tool_use"
	InputJSONDelta string `json:"-"` // partial JSON from content_block_delta with input_json_delta

	// Raw holds the original JSON for messages we don't fully parse.
	Raw json.RawMessage `json:"-"`
}

// ParseStreamMessage unmarshals a single line of stream-json output.
// It stamps each message with the current UTC time in RFC3339 format.
//
// Claude Code 2.1+ nests assistant message content inside message.content[]
// instead of using top-level fields. When top-level subtype is empty and
// msg.Message is populated, this function extracts content from the nested
// structure to populate Subtype, Text, ToolName, ToolID, ToolArgs, and Usage.
func ParseStreamMessage(data []byte) (StreamMessage, error) {
	var msg StreamMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return msg, err
	}
	msg.Timestamp = time.Now().UTC().Format(time.RFC3339)
	msg.Raw = make(json.RawMessage, len(data))
	copy(msg.Raw, data)

	if msg.Type == MessageTypeStreamEvent {
		var env streamEventEnvelope
		if err := json.Unmarshal(data, &env); err == nil {
			msg.EventType = env.Event.Type
			if env.Event.Delta.Type == "text_delta" {
				msg.DeltaText = env.Event.Delta.Text
			}
			if env.Event.Delta.Type == "input_json_delta" {
				msg.InputJSONDelta = env.Event.Delta.PartialJSON
			}
			if env.Event.Type == EventContentBlockStart && env.Event.ContentBlock.Type == string(SubtypeToolUse) {
				msg.ToolUseName = env.Event.ContentBlock.Name
				msg.ToolUseBlockID = env.Event.ContentBlock.ID
			}
		}
	}

	// Extract nested content for assistant messages in the new format.
	if msg.Type == MessageTypeAssistant && msg.Subtype == "" && len(msg.Message) > 0 {
		var env assistantMessageEnvelope
		if err := json.Unmarshal(msg.Message, &env); err == nil {
			// The Claude CLI emits at most one tool_use block per assistant
			// message. If multiple text blocks are present they are concatenated
			// (capped at maxNestedTextLen to prevent unbounded allocation).
			// Only the first tool_use block is used; subsequent ones are ignored.
			// tool_use takes precedence over text for the Subtype field.
			textLen := 0
			for _, block := range env.Content {
				switch block.Type {
				case string(SubtypeText):
					if msg.Subtype == "" {
						msg.Subtype = SubtypeText
					}
					remaining := maxNestedTextLen - textLen
					if remaining <= 0 {
						continue
					}
					t := block.Text
					if len(t) > remaining {
						t = t[:remaining]
					}
					msg.Text += t
					textLen += len(t)
				case string(SubtypeToolUse):
					if msg.Subtype == SubtypeToolUse {
						continue // keep the first tool_use block
					}
					msg.Subtype = SubtypeToolUse
					msg.ToolName = block.Name
					msg.ToolID = block.ID
					msg.ToolArgs = block.Input
				}
			}
			if msg.Usage == nil && env.Usage != nil {
				msg.Usage = env.Usage
			}
		}
	}

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

// maxNestedTextLen caps the accumulated text length extracted from nested
// message.content[] blocks to prevent unbounded memory allocation.
const maxNestedTextLen = 1 << 20 // 1 MiB

// maxStatusResultLen is the maximum number of runes included in the
// StatusInfo.Result field. Longer results are truncated with "...";
// use the result debug tool for the full untruncated text.
const maxStatusResultLen = 4000

type StatusInfo struct {
	Status        ProcessStatus  `json:"status"`
	SessionID     string         `json:"session_id,omitempty"`
	ErrorMessage  string         `json:"error,omitempty"`
	PreviousError string         `json:"previous_error,omitempty"`
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

// MessagesInfo holds the current conversation messages along with the
// process status. Used by the messages MCP tool for real-time access.
type MessagesInfo struct {
	Status   ProcessStatus    `json:"status"`
	Messages []MessageSummary `json:"messages"`
}

// RawMessagesInfo holds raw stream-json messages along with the process
// status and total message count. Used by the messages MCP tool to return
// lossless message data for external consumers.
type RawMessagesInfo struct {
	Status   ProcessStatus     `json:"status"`
	Total    int               `json:"total"`
	Messages []json.RawMessage `json:"messages"`
}

// MessageSummary is a simplified representation of a StreamMessage
// with only role and content, suitable for external consumers.
type MessageSummary struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []ToolCallInfo `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Timestamp  string         `json:"timestamp,omitempty"`
}

// ToolCallInfo holds structured tool call data for external consumers.
type ToolCallInfo struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// ToolResultInfo holds structured tool result data for external consumers.
type ToolResultInfo struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

// SummarizeMessages converts a slice of StreamMessages to simplified
// MessageSummary entries for external consumption. System messages are
// deduplicated by content.
func SummarizeMessages(msgs []StreamMessage) []MessageSummary {
	summaries := make([]MessageSummary, 0, len(msgs))
	seenSystem := make(map[string]bool)
	for _, msg := range msgs {
		s := summarizeMessage(msg)
		if s.Content == "" && s.Role == "" {
			continue
		}
		// Deduplicate system messages by content.
		if s.Role == string(MessageTypeSystem) {
			if seenSystem[s.Content] {
				continue
			}
			seenSystem[s.Content] = true
		}
		summaries = append(summaries, s)
	}
	return summaries
}

// collectRawMessages builds a RawMessagesInfo from a slice of StreamMessages,
// applying offset and type filtering. Total always reflects the unfiltered
// count so callers can detect whether more messages exist.
func collectRawMessages(status ProcessStatus, msgs []StreamMessage, offset int, types []string) RawMessagesInfo {
	total := len(msgs)

	// Build type filter set.
	var typeSet map[string]bool
	if len(types) > 0 {
		typeSet = make(map[string]bool, len(types))
		for _, t := range types {
			typeSet[t] = true
		}
	}

	// Apply offset.
	if offset >= len(msgs) {
		return RawMessagesInfo{Status: status, Total: total, Messages: []json.RawMessage{}}
	}
	if offset > 0 {
		msgs = msgs[offset:]
	}

	raw := make([]json.RawMessage, 0, len(msgs))
	for _, msg := range msgs {
		if typeSet != nil && !typeSet[string(msg.Type)] {
			continue
		}
		if len(msg.Raw) > 0 {
			raw = append(raw, msg.Raw)
		}
	}

	return RawMessagesInfo{
		Status:   status,
		Total:    total,
		Messages: raw,
	}
}

// summarizeMessage converts a single StreamMessage to a MessageSummary.
func summarizeMessage(msg StreamMessage) MessageSummary {
	switch msg.Type {
	case MessageTypeSystem:
		content := ""
		if msg.SessionID != "" {
			content = "Session: " + msg.SessionID
		}
		if content == "" {
			return MessageSummary{}
		}
		return MessageSummary{Role: "system", Content: content, Timestamp: msg.Timestamp}
	case MessageTypeAssistant:
		if msg.Subtype == SubtypeToolUse {
			tc := ToolCallInfo{
				ID:   msg.ToolID,
				Name: msg.ToolName,
				Args: msg.ToolArgs,
			}
			return MessageSummary{
				Role:      "assistant",
				Content:   "Using tool: " + msg.ToolName,
				ToolCalls: []ToolCallInfo{tc},
				Timestamp: msg.Timestamp,
			}
		}
		if msg.Subtype == SubtypeText && msg.Text != "" {
			return MessageSummary{Role: "assistant", Content: msg.Text, Timestamp: msg.Timestamp}
		}
		return MessageSummary{}
	case MessageTypeUser:
		// User messages contain tool results.
		blocks := ExtractToolResults(msg)
		if len(blocks) == 0 {
			return MessageSummary{}
		}
		// Return the first tool result as a "tool" role message.
		block := blocks[0]
		return MessageSummary{
			Role:       "tool",
			Content:    block.Content,
			ToolCallID: block.ToolUseID,
			Timestamp:  msg.Timestamp,
		}
	case MessageTypeResult:
		if msg.Result != "" {
			return MessageSummary{Role: "result", Content: msg.Result, Timestamp: msg.Timestamp}
		}
		return MessageSummary{}
	default:
		return MessageSummary{}
	}
}

// Float64Ptr returns a pointer to the given float64 value.
func Float64Ptr(f float64) *float64 {
	return &f
}

// Subagent status values reported in SubagentCall.Status.
const (
	SubagentStatusRunning   = "running"
	SubagentStatusCompleted = "completed"
)

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

// streamEventEnvelope is used to extract the nested event payload from
// stream_event messages emitted with --include-partial-messages.
type streamEventEnvelope struct {
	Event streamEventPayload `json:"event"`
}

type streamEventPayload struct {
	Type         string                `json:"type"`
	Delta        streamEventDelta      `json:"delta,omitempty"`
	ContentBlock streamEventBlockStart `json:"content_block,omitempty"`
}

type streamEventBlockStart struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type streamEventDelta struct {
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

// assistantContentBlock represents a single content block in message.content[]
// for assistant messages (text or tool_use). Claude Code 2.1+ nests assistant
// content here instead of using top-level fields.
type assistantContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// assistantMessageEnvelope is used to extract content and usage from the nested
// message object in the new Claude Code 2.1+ stream-json format.
type assistantMessageEnvelope struct {
	Content []assistantContentBlock `json:"content"`
	Usage   *TokenUsage             `json:"usage,omitempty"`
}

// ToolResultBlock represents a single content block from a user message (tool result).
type ToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

// prURLPattern matches GitHub pull request URLs in tool result content.
var prURLPattern = regexp.MustCompile(`https://github\.com/[\w.\-]+/[\w.\-]+/pull/\d+`)

// maxModelNameLen caps model name strings to prevent unbounded map key growth.
const maxModelNameLen = 256

// ExtractModel parses the model field from a StreamMessage's raw Message JSON.
// Returns an empty string if the message has no model field or cannot be parsed.
func ExtractModel(msg StreamMessage) string {
	if len(msg.Message) == 0 {
		return ""
	}
	var env messageModelEnvelope
	if err := json.Unmarshal(msg.Message, &env); err != nil {
		return ""
	}
	if len(env.Model) > maxModelNameLen {
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

// isBashTool reports whether the given tool name refers to a Bash/shell tool
// whose output may contain real PR URLs (as opposed to file content tools like
// Read/Write that may contain PR URLs embedded in source code or docs).
func isBashTool(name string) bool {
	return name == ToolNameBash || name == "bash"
}

// maxPRURLsPerBlock caps the number of PR URLs extracted from a single content block.
const maxPRURLsPerBlock = 20

// extractPRURLs returns GitHub PR URLs found in the given text, capped at maxPRURLsPerBlock.
func extractPRURLs(text string) []string {
	return prURLPattern.FindAllString(text, maxPRURLsPerBlock)
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
			if model := ExtractModel(msg); model != "" {
				usage[model]++
			}
		}
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}

// CollectPRURLs extracts unique GitHub PR URLs from tool_result content blocks
// that correspond to Bash/shell tool invocations. Tool results from file-reading
// tools (Read, Write, etc.) are skipped to avoid false positives from PR URLs
// embedded in source code or documentation.
func CollectPRURLs(messages []StreamMessage) []string {
	// Build a map from tool_use_id to tool_name by scanning assistant tool_use messages.
	toolNames := make(map[string]string)
	for _, msg := range messages {
		if msg.Type == MessageTypeAssistant && msg.Subtype == SubtypeToolUse && msg.ToolID != "" {
			toolNames[msg.ToolID] = msg.ToolName
		}
	}

	var urls []string
	for _, msg := range messages {
		blocks := ExtractToolResults(msg)
		for _, block := range blocks {
			// Only extract PR URLs from Bash tool results.
			if block.ToolUseID != "" {
				toolName := toolNames[block.ToolUseID]
				if !isBashTool(toolName) {
					continue
				}
			}
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

// copyStreamMessages returns a copy of the slice to avoid exposing internal
// state to callers. Returns nil for nil/empty input.
func copyStreamMessages(msgs []StreamMessage) []StreamMessage {
	if len(msgs) == 0 {
		return nil
	}
	cp := make([]StreamMessage, len(msgs))
	copy(cp, msgs)
	return cp
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
				if msg.Type == MessageTypeStreamEvent {
					continue
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

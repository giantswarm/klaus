package claude

import (
	"encoding/json"
)

// OpenAIMessage represents a message in OpenAI Chat Completions compatible format.
type OpenAIMessage struct {
	Role             string           `json:"role"`
	Content          *string          `json:"content"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ThinkingBlocks   []ThinkingBlock  `json:"thinking_blocks,omitempty"`
}

// OpenAIToolCall represents a tool call in OpenAI function calling format.
type OpenAIToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction holds the function name and stringified arguments.
type OpenAIFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ThinkingBlock preserves Anthropic thinking blocks with signatures,
// following the LiteLLM/DeepSeek convention.
type ThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}

// OpenAIMetadata holds infrastructure metadata extracted from system and
// result messages, excluded from the messages array.
type OpenAIMetadata struct {
	SessionID         string           `json:"session_id,omitempty"`
	Model             string           `json:"model,omitempty"`
	ClaudeCodeVersion string           `json:"claude_code_version,omitempty"`
	Tools             []string         `json:"tools,omitempty"`
	Plugins           []OpenAIPlugin   `json:"plugins,omitempty"`
	Hooks             []OpenAIHook     `json:"hooks,omitempty"`
	Subagents         []OpenAISubagent `json:"subagents,omitempty"`
	CostUSD           float64          `json:"cost_usd,omitempty"`
	DurationMS        float64          `json:"duration_ms,omitempty"`
	NumTurns          int              `json:"num_turns,omitempty"`
	Usage             *TokenUsage      `json:"usage,omitempty"`
}

// OpenAIPlugin holds plugin metadata from system/init messages.
type OpenAIPlugin struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

// OpenAIHook holds hook execution metadata from system/hook_response messages.
type OpenAIHook struct {
	HookName string `json:"hook_name"`
	Output   string `json:"output,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}

// OpenAISubagent holds subagent dispatch metadata from system/task_started messages.
type OpenAISubagent struct {
	TaskID      string `json:"task_id"`
	Description string `json:"description,omitempty"`
	TaskType    string `json:"task_type,omitempty"`
}

// OpenAIMessagesInfo is the response envelope for the MCP messages tool,
// containing OpenAI-compatible messages, metadata, and pagination info.
type OpenAIMessagesInfo struct {
	Messages []OpenAIMessage `json:"messages"`
	Metadata OpenAIMetadata  `json:"metadata"`
	Total    int             `json:"total"`
}

// ToOpenAIMessages converts raw stream-json StreamMessages into OpenAI Chat
// Completions compatible format. System and result messages are excluded from
// the messages array and extracted into metadata. Consecutive assistant
// messages with the same message.id are consolidated into a single message.
func ToOpenAIMessages(msgs []StreamMessage) ([]OpenAIMessage, OpenAIMetadata) {
	var result []OpenAIMessage
	var meta OpenAIMetadata

	// Track the current assistant message being consolidated.
	// We merge consecutive assistant messages with the same message.id.
	var currentAssistantID string
	var currentAssistantIdx int = -1

	for _, msg := range msgs {
		switch msg.Type {
		case MessageTypeSystem:
			extractSystemMetadata(msg, &meta)

		case MessageTypeResult:
			extractResultMetadata(msg, &meta)

		case MessageTypeAssistant:
			converted := convertAssistantMessage(msg)
			if converted == nil {
				continue
			}
			msgID := extractMessageID(msg)

			// Consolidate consecutive messages with the same message.id.
			if msgID != "" && msgID == currentAssistantID && currentAssistantIdx >= 0 {
				mergeAssistantMessage(&result[currentAssistantIdx], converted)
			} else {
				result = append(result, *converted)
				currentAssistantIdx = len(result) - 1
				currentAssistantID = msgID
			}

		case MessageTypeUser:
			currentAssistantID = ""
			currentAssistantIdx = -1
			userMsgs := convertUserMessage(msg)
			result = append(result, userMsgs...)

		default:
			// Skip stream_event and unknown types.
		}
	}

	return result, meta
}

// --- System/result metadata extraction ---

// systemInitEnvelope extracts system/init fields from Raw JSON.
type systemInitEnvelope struct {
	Subtype           string         `json:"subtype"`
	SessionID         string         `json:"session_id,omitempty"`
	Model             string         `json:"model,omitempty"`
	ClaudeCodeVersion string         `json:"claude_code_version,omitempty"`
	Tools             []string       `json:"tools,omitempty"`
	Plugins           []OpenAIPlugin `json:"plugins,omitempty"`
	// hook fields
	HookName string `json:"hook_name,omitempty"`
	Output   string `json:"output,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	// task_started fields
	TaskID      string `json:"task_id,omitempty"`
	Description string `json:"description,omitempty"`
	TaskType    string `json:"task_type,omitempty"`
}

func extractSystemMetadata(msg StreamMessage, meta *OpenAIMetadata) {
	if len(msg.Raw) == 0 {
		return
	}
	var env systemInitEnvelope
	if err := json.Unmarshal(msg.Raw, &env); err != nil {
		return
	}

	switch env.Subtype {
	case "init":
		if env.SessionID != "" {
			meta.SessionID = env.SessionID
		}
		if env.Model != "" {
			meta.Model = env.Model
		}
		if env.ClaudeCodeVersion != "" {
			meta.ClaudeCodeVersion = env.ClaudeCodeVersion
		}
		if len(env.Tools) > 0 {
			meta.Tools = env.Tools
		}
		if len(env.Plugins) > 0 {
			meta.Plugins = env.Plugins
		}
	case "hook_response":
		hook := OpenAIHook{
			HookName: env.HookName,
			Output:   env.Output,
		}
		if env.ExitCode != nil {
			hook.ExitCode = *env.ExitCode
		}
		meta.Hooks = append(meta.Hooks, hook)
	case "task_started":
		meta.Subagents = append(meta.Subagents, OpenAISubagent{
			TaskID:      env.TaskID,
			Description: env.Description,
			TaskType:    env.TaskType,
		})
	}
}

// resultEnvelope extracts result message fields from Raw JSON.
type resultEnvelope struct {
	DurationMS   float64     `json:"duration_ms,omitempty"`
	NumTurns     int         `json:"num_turns,omitempty"`
	TotalCostUSD float64     `json:"total_cost_usd,omitempty"`
	Usage        *TokenUsage `json:"usage,omitempty"`
}

func extractResultMetadata(msg StreamMessage, meta *OpenAIMetadata) {
	if len(msg.Raw) == 0 {
		return
	}
	var env resultEnvelope
	if err := json.Unmarshal(msg.Raw, &env); err != nil {
		return
	}
	if env.TotalCostUSD > 0 {
		meta.CostUSD = env.TotalCostUSD
	}
	if env.DurationMS > 0 {
		meta.DurationMS = env.DurationMS
	}
	if env.NumTurns > 0 {
		meta.NumTurns = env.NumTurns
	}
	if env.Usage != nil {
		meta.Usage = env.Usage
	}
}

// --- Assistant message conversion ---

// openaiContentBlock is used to parse ALL content block types from
// the nested message.content[] array (text, tool_use, thinking).
type openaiContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
}

// openaiAssistantEnvelope parses the nested message object for assistant messages.
type openaiAssistantEnvelope struct {
	ID      string               `json:"id,omitempty"`
	Content []openaiContentBlock `json:"content"`
}

func convertAssistantMessage(msg StreamMessage) *OpenAIMessage {
	// Try to parse the nested message for full content blocks.
	if len(msg.Message) > 0 {
		var env openaiAssistantEnvelope
		if err := json.Unmarshal(msg.Message, &env); err == nil && len(env.Content) > 0 {
			return buildAssistantFromBlocks(env.Content)
		}
	}

	// Fallback to top-level parsed fields (legacy flat format).
	return buildAssistantFromFlat(msg)
}

func buildAssistantFromBlocks(blocks []openaiContentBlock) *OpenAIMessage {
	m := &OpenAIMessage{Role: "assistant"}

	var textParts []string
	var thinkingParts []string

	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			argsStr := "{}"
			if len(block.Input) > 0 {
				argsStr = string(block.Input)
			}
			m.ToolCalls = append(m.ToolCalls, OpenAIToolCall{
				ID:   block.ID,
				Type: "function",
				Function: OpenAIFunction{
					Name:      block.Name,
					Arguments: argsStr,
				},
			})
		case "thinking":
			if block.Thinking != "" {
				thinkingParts = append(thinkingParts, block.Thinking)
				m.ThinkingBlocks = append(m.ThinkingBlocks, ThinkingBlock{
					Type:      "thinking",
					Thinking:  block.Thinking,
					Signature: block.Signature,
				})
			}
		}
	}

	if len(textParts) > 0 {
		joined := joinStrings(textParts)
		m.Content = &joined
	}
	if len(thinkingParts) > 0 {
		m.ReasoningContent = joinStrings(thinkingParts)
	}

	// If no text and no thinking, ensure content is null when tool_calls present.
	if m.Content == nil && len(m.ToolCalls) == 0 && m.ReasoningContent == "" && len(m.ThinkingBlocks) == 0 {
		return nil
	}

	return m
}

func buildAssistantFromFlat(msg StreamMessage) *OpenAIMessage {
	m := &OpenAIMessage{Role: "assistant"}

	switch msg.Subtype {
	case SubtypeText:
		if msg.Text != "" {
			m.Content = &msg.Text
		}
	case SubtypeToolUse:
		argsStr := "{}"
		if len(msg.ToolArgs) > 0 {
			argsStr = string(msg.ToolArgs)
		}
		m.ToolCalls = append(m.ToolCalls, OpenAIToolCall{
			ID:   msg.ToolID,
			Type: "function",
			Function: OpenAIFunction{
				Name:      msg.ToolName,
				Arguments: argsStr,
			},
		})
	default:
		return nil
	}

	return m
}

func extractMessageID(msg StreamMessage) string {
	if len(msg.Message) == 0 {
		return ""
	}
	var env struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(msg.Message, &env); err != nil {
		return ""
	}
	return env.ID
}

// mergeAssistantMessage merges content from src into dst (consolidation).
func mergeAssistantMessage(dst *OpenAIMessage, src *OpenAIMessage) {
	// Merge text content.
	if src.Content != nil {
		if dst.Content == nil {
			dst.Content = src.Content
		} else {
			merged := *dst.Content + *src.Content
			dst.Content = &merged
		}
	}

	// Merge tool calls.
	dst.ToolCalls = append(dst.ToolCalls, src.ToolCalls...)

	// Merge thinking.
	if src.ReasoningContent != "" {
		if dst.ReasoningContent == "" {
			dst.ReasoningContent = src.ReasoningContent
		} else {
			dst.ReasoningContent += src.ReasoningContent
		}
	}
	dst.ThinkingBlocks = append(dst.ThinkingBlocks, src.ThinkingBlocks...)
}

// --- User message conversion ---

// userContentBlock parses content blocks from user messages
// (tool_result and text types).
type userContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type userMessageEnvelope struct {
	Content json.RawMessage `json:"content"`
}

func convertUserMessage(msg StreamMessage) []OpenAIMessage {
	// If it's a synthetic user message with Text set directly (injected prompt).
	if msg.Text != "" && len(msg.Message) == 0 {
		return []OpenAIMessage{{Role: "user", Content: strPtr(msg.Text)}}
	}

	if len(msg.Message) == 0 {
		return nil
	}

	var env userMessageEnvelope
	if err := json.Unmarshal(msg.Message, &env); err != nil {
		return nil
	}

	// content can be a string or an array of blocks.
	// Try array first, then fall back to string.
	var blocks []userContentBlock
	if err := json.Unmarshal(env.Content, &blocks); err != nil {
		// Try as a plain string.
		var s string
		if err := json.Unmarshal(env.Content, &s); err == nil && s != "" {
			return []OpenAIMessage{{Role: "user", Content: &s}}
		}
		return nil
	}

	var result []OpenAIMessage
	var textParts []string

	for _, block := range blocks {
		switch block.Type {
		case "tool_result":
			content := extractToolResultContent(block.Content)
			if content == "" {
				content = block.Text
			}
			result = append(result, OpenAIMessage{
				Role:       "tool",
				Content:    &content,
				ToolCallID: block.ToolUseID,
			})
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		}
	}

	// If there were text blocks (no tool results mixed), emit a single user message.
	if len(textParts) > 0 {
		joined := joinStrings(textParts)
		result = append([]OpenAIMessage{{Role: "user", Content: &joined}}, result...)
	}

	return result
}

// --- Synthetic user message injection ---

// syntheticUserMessage creates a StreamMessage representing the user's input
// prompt. The Claude CLI does not echo user input on stdout, so this must be
// injected into liveMessages before writing the prompt to stdin.
func syntheticUserMessage(prompt string) StreamMessage {
	userContent := []map[string]string{{"type": "text", "text": prompt}}
	raw, _ := json.Marshal(map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": userContent,
		},
	})
	return StreamMessage{
		Type: MessageTypeUser,
		Text: prompt,
		Raw:  json.RawMessage(raw),
	}
}

// --- Helpers ---

// extractToolResultContent extracts text from a tool_result content field,
// which can be either a JSON string or an array of content blocks
// (e.g. [{"type":"text","text":"..."},{"type":"tool_reference","tool_name":"..."}]).
func extractToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try as a plain string first (built-in tools like Bash, Glob, Read).
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try as an array of content blocks (MCP tools, ToolSearch).
	var blocks []struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		ToolName string `json:"tool_name,omitempty"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "tool_reference":
			if b.ToolName != "" {
				parts = append(parts, b.ToolName)
			}
		}
	}
	return joinStrings(parts)
}

func strPtr(s string) *string {
	return &s
}

func joinStrings(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	buf := make([]byte, 0, total)
	for _, p := range parts {
		buf = append(buf, p...)
	}
	return string(buf)
}

// collectOpenAIMessages builds an OpenAIMessagesInfo from StreamMessages,
// applying offset filtering on the converted output. Total always reflects
// the full converted message count.
func collectOpenAIMessages(status ProcessStatus, msgs []StreamMessage, offset int) OpenAIMessagesInfo {
	openaiMsgs, meta := ToOpenAIMessages(msgs)
	total := len(openaiMsgs)

	if offset >= len(openaiMsgs) {
		return OpenAIMessagesInfo{
			Messages: []OpenAIMessage{},
			Metadata: meta,
			Total:    total,
		}
	}
	if offset > 0 {
		openaiMsgs = openaiMsgs[offset:]
	}

	return OpenAIMessagesInfo{
		Messages: openaiMsgs,
		Metadata: meta,
		Total:    total,
	}
}

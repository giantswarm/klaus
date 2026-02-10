package claude

import "encoding/json"

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
	Type    MessageType    `json:"type"`
	Subtype MessageSubtype `json:"subtype,omitempty"`
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
	ProcessStatusStarting ProcessStatus = "starting"
	ProcessStatusIdle     ProcessStatus = "idle"
	ProcessStatusBusy     ProcessStatus = "busy"
	ProcessStatusStopped  ProcessStatus = "stopped"
	ProcessStatusError    ProcessStatus = "error"
)

type StatusInfo struct {
	Status       ProcessStatus `json:"status"`
	SessionID    string        `json:"session_id,omitempty"`
	ErrorMessage string        `json:"error,omitempty"`
	TotalCost    float64       `json:"total_cost_usd,omitempty"`
}

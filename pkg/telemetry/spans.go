package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Tracer returns a named tracer from the global provider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// Span names used across the Klaus packages. Centralised here so a2a,
// claude, and mcp packages all reference the same strings.
const (
	SpanA2ATaskReceived       = "a2a.task.received"
	SpanClaudeSubprocessStart = "claude.subprocess.start"
	SpanClaudeTurnComplete    = "claude.turn.complete"
	SpanMCPToolCall           = "mcp.tool.call"
	SpanClaudeSubprocessExit  = "claude.subprocess.exit"
)

// Attribute keys for span attributes. Using attribute.Key provides type-safe
// value constructors (e.g. AttrModel.String(v), AttrInputTokens.Int64(v)).
var (
	AttrContextID     = attribute.Key("a2a.context_id")
	AttrSessionID     = attribute.Key("claude.session_id")
	AttrMessageLength = attribute.Key("a2a.message_length")
	AttrModel         = attribute.Key("claude.model")
	AttrResume        = attribute.Key("claude.resume")
	AttrTurnIndex     = attribute.Key("claude.turn_index")
	AttrStopReason    = attribute.Key("claude.stop_reason")
	AttrInputTokens   = attribute.Key("claude.input_tokens")
	AttrOutputTokens  = attribute.Key("claude.output_tokens")
	AttrToolName      = attribute.Key("mcp.tool_name")
	AttrServerName    = attribute.Key("mcp.server_name")
	AttrDurationMS    = attribute.Key("mcp.duration_ms")
	AttrExitCode      = attribute.Key("claude.exit_code")
	AttrRetryCount    = attribute.Key("claude.retry_count")
)

package telemetry

import (
	"go.opentelemetry.io/otel"
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

// Attribute key names for span attributes.
const (
	AttrContextID     = "a2a.context_id"
	AttrSessionID     = "claude.session_id"
	AttrMessageLength = "a2a.message_length"
	AttrModel         = "claude.model"
	AttrResume        = "claude.resume"
	AttrTurnIndex     = "claude.turn_index"
	AttrStopReason    = "claude.stop_reason"
	AttrInputTokens   = "claude.input_tokens"
	AttrOutputTokens  = "claude.output_tokens"
	AttrToolName      = "mcp.tool_name"
	AttrServerName    = "mcp.server_name"
	AttrDurationMS    = "mcp.duration_ms"
	AttrExitCode      = "claude.exit_code"
	AttrRetryCount    = "claude.retry_count"
)

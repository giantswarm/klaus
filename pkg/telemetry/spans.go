package telemetry

// Span names used across the Klaus packages.
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

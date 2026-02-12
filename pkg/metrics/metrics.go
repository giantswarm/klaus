// Package metrics provides Prometheus metrics for the klaus server.
//
// These metrics cover the server-side view of prompt handling, process
// lifecycle, and cost tracking. They complement the Claude Code CLI's native
// OpenTelemetry telemetry (claude_code.* namespace) with server-level
// observability in the klaus_* namespace.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "klaus"

// String constants mirroring claude.MessageType / claude.MessageSubtype so
// that this package can stay free of circular imports on pkg/claude.
const (
	messageTypeAssistant = "assistant"
	messageTypeResult    = "result"
	subtypeToolUse       = "tool_use"
)

// PromptsTotal counts the number of prompt invocations.
var PromptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Name:      "prompts_total",
	Help:      "Total number of prompt invocations.",
}, []string{"status", "mode"})

// PromptDurationSeconds tracks the end-to-end duration of prompt execution.
var PromptDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: namespace,
	Name:      "prompt_duration_seconds",
	Help:      "Duration of prompt execution in seconds.",
	Buckets:   prometheus.ExponentialBuckets(1, 2, 12), // 1s to ~68m
}, []string{"status", "mode"})

// ProcessStatusGauge is a gauge indicating the current process state.
// Only the label matching the active status is set to 1; all others are 0.
var ProcessStatusGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: namespace,
	Name:      "process_status",
	Help:      "Current process status (1 for active status, 0 for others).",
}, []string{"status"})

// SessionCostUSDTotal tracks cumulative cost from stream-json total_cost_usd.
var SessionCostUSDTotal = promauto.NewCounter(prometheus.CounterOpts{
	Namespace: namespace,
	Name:      "session_cost_usd_total",
	Help:      "Cumulative session cost in USD.",
})

// MessagesTotal counts messages processed from the Claude subprocess.
var MessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Name:      "messages_total",
	Help:      "Total number of stream-json messages processed.",
}, []string{"type"})

// ToolCallsTotal counts tool invocations by the Claude subprocess.
// The tool_name label values come from the Claude CLI's finite built-in tool
// set (Bash, Read, Edit, etc.). If user-defined tools are ever forwarded
// through this path, consider constraining or bucketing the label to avoid
// unbounded cardinality.
var ToolCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Name:      "tool_calls_total",
	Help:      "Total number of tool calls made by the Claude agent.",
}, []string{"tool_name"})

// ProcessRestartsTotal counts automatic restarts of the persistent subprocess.
var ProcessRestartsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Namespace: namespace,
	Name:      "process_restarts_total",
	Help:      "Total number of automatic persistent process restarts.",
})

// AllStatuses is the complete list of process status labels used by the
// ProcessStatusGauge. It must match claude.AllProcessStatuses -- a cross-
// package test in sync_test.go enforces this at test time.
var AllStatuses = []string{"starting", "idle", "busy", "completed", "stopped", "error"}

// SetProcessStatus sets the process status gauge, setting the given status to 1
// and all others to 0.
func SetProcessStatus(status string) {
	for _, s := range AllStatuses {
		if s == status {
			ProcessStatusGauge.WithLabelValues(s).Set(1)
		} else {
			ProcessStatusGauge.WithLabelValues(s).Set(0)
		}
	}
}

// RecordStreamMessage records per-message Prometheus metrics from a parsed
// stream-json message. The caller passes the raw string values of the message
// fields to avoid a circular import on pkg/claude.
func RecordStreamMessage(msgType, subtype, toolName string) {
	MessagesTotal.WithLabelValues(msgType).Inc()
	if msgType == messageTypeAssistant && subtype == subtypeToolUse {
		ToolCallsTotal.WithLabelValues(toolName).Inc()
	}
}

// RecordCost adds a cost increment to the cumulative cost counter. For
// persistent-mode processes where TotalCost is a running total, the caller
// must compute the delta before calling this function.
func RecordCost(delta float64) {
	if delta > 0 {
		SessionCostUSDTotal.Add(delta)
	}
}

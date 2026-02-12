package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestSetProcessStatus(t *testing.T) {
	tests := []struct {
		name      string
		setStatus string
	}{
		{name: "idle", setStatus: "idle"},
		{name: "busy", setStatus: "busy"},
		{name: "starting", setStatus: "starting"},
		{name: "completed", setStatus: "completed"},
		{name: "stopped", setStatus: "stopped"},
		{name: "error", setStatus: "error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetProcessStatus(tc.setStatus)

			for _, s := range allStatuses {
				gauge, err := ProcessStatus.GetMetricWithLabelValues(s)
				if err != nil {
					t.Fatalf("failed to get metric for status %q: %v", s, err)
				}
				var m dto.Metric
				if err := gauge.Write(&m); err != nil {
					t.Fatalf("failed to write metric for status %q: %v", s, err)
				}
				got := m.GetGauge().GetValue()
				if s == tc.setStatus {
					if got != 1 {
						t.Errorf("status %q: expected 1, got %f", s, got)
					}
				} else {
					if got != 0 {
						t.Errorf("status %q: expected 0, got %f", s, got)
					}
				}
			}
		})
	}
}

func TestMetricsRegistered(t *testing.T) {
	// Verify all metrics are registered with the default Prometheus registry.
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	wantNames := map[string]bool{
		"klaus_prompts_total":           false,
		"klaus_prompt_duration_seconds": false,
		"klaus_process_status":          false,
		"klaus_session_cost_usd_total":  false,
		"klaus_messages_total":          false,
		"klaus_tool_calls_total":        false,
		"klaus_process_restarts_total":  false,
	}

	// Initialize at least one series so they appear in the gather output.
	PromptsTotal.WithLabelValues("test", "test")
	PromptDurationSeconds.WithLabelValues("test", "test").Observe(1.0)
	MessagesTotal.WithLabelValues("test")
	ToolCallsTotal.WithLabelValues("test")
	SessionCostUSDTotal.Add(0)
	ProcessRestartsTotal.Inc()
	SetProcessStatus("idle")

	metricFamilies, err = prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	for _, mf := range metricFamilies {
		if _, ok := wantNames[mf.GetName()]; ok {
			wantNames[mf.GetName()] = true
		}
	}

	for name, found := range wantNames {
		if !found {
			t.Errorf("metric %q not found in default registry", name)
		}
	}
}

func TestPromptsTotal(t *testing.T) {
	// Verify the counter can be incremented without panic.
	PromptsTotal.WithLabelValues("started", "async").Inc()
	PromptsTotal.WithLabelValues("completed", "blocking").Inc()
	PromptsTotal.WithLabelValues("error", "async").Inc()
}

func TestSessionCostUSDTotal(t *testing.T) {
	// Verify the counter can be incremented without panic.
	SessionCostUSDTotal.Add(0.05)
	SessionCostUSDTotal.Add(0.10)
}

func TestMessagesTotal(t *testing.T) {
	// Verify counters can be incremented for all message types without panic.
	MessagesTotal.WithLabelValues("system").Inc()
	MessagesTotal.WithLabelValues("assistant").Inc()
	MessagesTotal.WithLabelValues("result").Inc()
}

func TestToolCallsTotal(t *testing.T) {
	// Verify the counter can be incremented with various tool names without panic.
	ToolCallsTotal.WithLabelValues("Bash").Inc()
	ToolCallsTotal.WithLabelValues("Edit").Inc()
	ToolCallsTotal.WithLabelValues("Read").Inc()
}

func TestProcessRestartsTotal(t *testing.T) {
	// Verify the counter can be incremented without panic.
	ProcessRestartsTotal.Inc()
}

func TestPromptDurationSeconds(t *testing.T) {
	// Verify the histogram can record observations without panic.
	PromptDurationSeconds.WithLabelValues("completed", "blocking").Observe(5.0)
	PromptDurationSeconds.WithLabelValues("error", "blocking").Observe(1.0)
}

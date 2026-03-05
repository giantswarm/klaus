package metrics

import (
	"fmt"
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

			for _, s := range AllStatuses {
				gauge, err := ProcessStatusGauge.GetMetricWithLabelValues(s)
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
	// Initialise at least one series per metric so they appear in the gather
	// output (counters/histograms without observations are not reported).
	PromptsTotal.WithLabelValues("test", "test")
	PromptDurationSeconds.WithLabelValues("test", "test").Observe(1.0)
	MessagesTotal.WithLabelValues("test")
	ToolCallsTotal.WithLabelValues("test")
	SessionCostUSDTotal.Add(0)
	ProcessRestartsTotal.Inc()
	SetProcessStatus("idle")

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

func TestRecordStreamMessage(t *testing.T) {
	// Record an assistant text message.
	RecordStreamMessage("assistant", "text", "")
	assertCounterValue(t, MessagesTotal, "assistant", 1)

	// Record an assistant tool_use message -- should also bump ToolCallsTotal.
	RecordStreamMessage("assistant", "tool_use", "Bash")
	assertCounterValue(t, MessagesTotal, "assistant", 2)
	assertToolCallValue(t, "Bash", 1)

	// Record a result message.
	RecordStreamMessage("result", "", "")
	assertCounterValue(t, MessagesTotal, "result", 1)
}

func TestRecordCost(t *testing.T) {
	// Capture the counter before our additions.
	before := readCounter(t, SessionCostUSDTotal)

	RecordCost(0.05)
	RecordCost(0.10)

	after := readCounter(t, SessionCostUSDTotal)
	delta := after - before
	if delta < 0.14 || delta > 0.16 {
		t.Errorf("expected cumulative cost delta ~0.15, got %f", delta)
	}

	// Negative or zero deltas must be ignored.
	RecordCost(0)
	RecordCost(-1.0)

	afterNoop := readCounter(t, SessionCostUSDTotal)
	if afterNoop != after {
		t.Errorf("expected no change for non-positive delta, got %f -> %f", after, afterNoop)
	}
}

func TestPromptDurationSeconds(t *testing.T) {
	PromptDurationSeconds.WithLabelValues("completed", "blocking").Observe(5.0)
	PromptDurationSeconds.WithLabelValues("error", "blocking").Observe(1.0)

	// Verify the histogram was actually recorded by gathering metrics and
	// checking the sample count.
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range metricFamilies {
		if mf.GetName() != "klaus_prompt_duration_seconds" {
			continue
		}
		found = true
		// Sum up sample counts across all label combinations.
		var totalCount uint64
		for _, m := range mf.GetMetric() {
			totalCount += m.GetHistogram().GetSampleCount()
		}
		if totalCount < 2 {
			t.Errorf("expected at least 2 observations, got %d", totalCount)
		}
		break
	}
	if !found {
		t.Error("klaus_prompt_duration_seconds not found in gathered metrics")
	}
}

func TestSafeToolName_CardinalityGuard(t *testing.T) {
	// Reset knownToolNames for this test to get deterministic behaviour.
	toolNamesMu.Lock()
	saved := knownToolNames
	knownToolNames = make(map[string]struct{})
	toolNamesMu.Unlock()

	defer func() {
		toolNamesMu.Lock()
		knownToolNames = saved
		toolNamesMu.Unlock()
	}()

	// Fill up to the limit with unique tool names.
	for i := 0; i < maxToolNameLabels; i++ {
		name := fmt.Sprintf("Tool_%d", i)
		got := safeToolName(name)
		if got != name {
			t.Errorf("safeToolName(%q) = %q, want %q (under limit)", name, got, name)
		}
	}

	// The next new tool name should be bucketed as "other".
	got := safeToolName("OverflowTool")
	if got != "other" {
		t.Errorf("safeToolName(OverflowTool) = %q, want %q (over limit)", got, "other")
	}

	// An already-known tool should still return its real name.
	got = safeToolName("Tool_0")
	if got != "Tool_0" {
		t.Errorf("safeToolName(Tool_0) = %q, want %q (already known)", got, "Tool_0")
	}
}

// --- helpers -----------------------------------------------------------------

// assertCounterValue checks the counter value for a given label on a CounterVec.
// Because tests share the default registry, the assertion uses >=.
func assertCounterValue(t *testing.T, vec *prometheus.CounterVec, label string, wantAtLeast float64) {
	t.Helper()
	c, err := vec.GetMetricWithLabelValues(label)
	if err != nil {
		t.Fatalf("failed to get counter for label %q: %v", label, err)
	}
	var m dto.Metric
	if err := c.(prometheus.Metric).Write(&m); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	got := m.GetCounter().GetValue()
	if got < wantAtLeast {
		t.Errorf("counter{%q}: expected >= %f, got %f", label, wantAtLeast, got)
	}
}

func assertToolCallValue(t *testing.T, toolName string, wantAtLeast float64) {
	t.Helper()
	c, err := ToolCallsTotal.GetMetricWithLabelValues(toolName)
	if err != nil {
		t.Fatalf("failed to get counter for tool %q: %v", toolName, err)
	}
	var m dto.Metric
	if err := c.(prometheus.Metric).Write(&m); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	got := m.GetCounter().GetValue()
	if got < wantAtLeast {
		t.Errorf("tool_calls_total{%q}: expected >= %f, got %f", toolName, wantAtLeast, got)
	}
}

func readCounter(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("failed to read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

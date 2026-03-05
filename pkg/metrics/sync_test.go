package metrics_test

import (
	"testing"

	"github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/metrics"
)

// TestAllStatusesMatchProcessStatuses ensures the metrics package's AllStatuses
// list stays in sync with the canonical claude.AllProcessStatuses. A mismatch
// means the SetProcessStatus gauge will silently ignore a new status.
func TestAllStatusesMatchProcessStatuses(t *testing.T) {
	if len(metrics.AllStatuses) != len(claude.AllProcessStatuses) {
		t.Fatalf("metrics.AllStatuses has %d entries but claude.AllProcessStatuses has %d",
			len(metrics.AllStatuses), len(claude.AllProcessStatuses))
	}

	for i, s := range metrics.AllStatuses {
		if s != string(claude.AllProcessStatuses[i]) {
			t.Errorf("mismatch at index %d: metrics=%q claude=%q", i, s, claude.AllProcessStatuses[i])
		}
	}
}

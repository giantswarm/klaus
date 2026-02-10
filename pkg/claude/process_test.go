package claude

import (
	"encoding/json"
	"testing"
)

func TestNewProcess_InitialState(t *testing.T) {
	process := NewProcess(DefaultOptions())

	status := process.Status()
	if status.Status != ProcessStatusIdle {
		t.Errorf("expected initial status %q, got %q", ProcessStatusIdle, status.Status)
	}
	if status.SessionID != "" {
		t.Errorf("expected empty session ID, got %q", status.SessionID)
	}
	if status.ErrorMessage != "" {
		t.Errorf("expected empty error message, got %q", status.ErrorMessage)
	}
}

func TestNewProcess_DonePreClosed(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Done channel should be immediately readable (pre-closed).
	select {
	case <-process.Done():
		// Expected.
	default:
		t.Error("expected Done() to be immediately readable for new process")
	}
}

func TestProcess_MarshalStatus(t *testing.T) {
	process := NewProcess(DefaultOptions())

	data, err := process.MarshalStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var info StatusInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}

	if info.Status != ProcessStatusIdle {
		t.Errorf("expected status %q, got %q", ProcessStatusIdle, info.Status)
	}
}

func TestProcess_StopWhenNotRunning(t *testing.T) {
	process := NewProcess(DefaultOptions())

	// Stop on an idle process should be a no-op.
	if err := process.Stop(); err != nil {
		t.Errorf("unexpected error stopping idle process: %v", err)
	}
}

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/giantswarm/klaus/pkg/claude"
)

type mockPrompter struct {
	status claude.StatusInfo
}

func (m *mockPrompter) Run(_ context.Context, _ string) (<-chan claude.StreamMessage, error) {
	ch := make(chan claude.StreamMessage)
	close(ch)
	return ch, nil
}

func (m *mockPrompter) RunWithOptions(_ context.Context, _ string, _ *claude.RunOptions) (<-chan claude.StreamMessage, error) {
	ch := make(chan claude.StreamMessage)
	close(ch)
	return ch, nil
}

func (m *mockPrompter) RunSyncWithOptions(_ context.Context, _ string, _ *claude.RunOptions) (string, []claude.StreamMessage, error) {
	return "", nil, nil
}

func (m *mockPrompter) Submit(_ context.Context, _ string, _ *claude.RunOptions) error {
	return nil
}

func (m *mockPrompter) Status() claude.StatusInfo {
	return m.status
}

func (m *mockPrompter) Stop() error {
	return nil
}

func (m *mockPrompter) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (m *mockPrompter) ResultDetail() claude.ResultDetailInfo {
	return claude.ResultDetailInfo{}
}

func (m *mockPrompter) MarshalStatus() ([]byte, error) {
	return json.Marshal(m.status)
}

func TestHandleHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	handleHealthz(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "ok") {
		t.Errorf("expected body to contain %q, got %q", "ok", body)
	}
}

func TestHandleReadyz(t *testing.T) {
	tests := []struct {
		name       string
		status     claude.ProcessStatus
		wantStatus int
	}{
		{
			name:       "ready when idle",
			status:     claude.ProcessStatusIdle,
			wantStatus: http.StatusOK,
		},
		{
			name:       "ready when busy",
			status:     claude.ProcessStatusBusy,
			wantStatus: http.StatusOK,
		},
		{
			name:       "ready when completed",
			status:     claude.ProcessStatusCompleted,
			wantStatus: http.StatusOK,
		},
		{
			name:       "not ready when starting",
			status:     claude.ProcessStatusStarting,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "not ready when stopped",
			status:     claude.ProcessStatusStopped,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "not ready when errored",
			status:     claude.ProcessStatusError,
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			w := httptest.NewRecorder()
			process := &mockPrompter{status: claude.StatusInfo{Status: tc.status}}

			handleReadyz(process)(w, req)

			resp := w.Result()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("expected status %d, got %d", tc.wantStatus, resp.StatusCode)
			}
		})
	}
}

func TestHandleRoot(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "klaus") {
		t.Errorf("expected body to contain %q, got %q", "klaus", body)
	}
}

func TestHandleRoot_UnknownPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 for unknown path, got %d", resp.StatusCode)
	}
}

func TestHandleStatus(t *testing.T) {
	process := claude.NewProcess(claude.DefaultOptions())

	handler := handleStatus(process, ModeSingleShot, "")
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type %q, got %q", "application/json", contentType)
	}

	var status statusResponse
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Name != "klaus" {
		t.Errorf("expected name %q, got %q", "klaus", status.Name)
	}
	if status.Agent.Status != claude.ProcessStatusIdle {
		t.Errorf("expected agent status %q, got %q", claude.ProcessStatusIdle, status.Agent.Status)
	}
	if status.Owner != "" {
		t.Errorf("expected empty owner when not configured, got %q", status.Owner)
	}
}

func TestHandleStatus_PersistentMode(t *testing.T) {
	process := claude.NewProcess(claude.DefaultOptions())

	handler := handleStatus(process, ModePersistent, "")
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	var status statusResponse
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Mode != ModePersistent {
		t.Errorf("expected mode %q, got %q", ModePersistent, status.Mode)
	}
}

func TestHandleStatus_WithOwner(t *testing.T) {
	process := claude.NewProcess(claude.DefaultOptions())

	handler := handleStatus(process, ModeSingleShot, "owner@example.com")
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	var status statusResponse
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Owner != "owner@example.com" {
		t.Errorf("expected owner %q, got %q", "owner@example.com", status.Owner)
	}
}

func TestRegisterOperationalRoutes(t *testing.T) {
	process := claude.NewProcess(claude.DefaultOptions())
	mux := http.NewServeMux()

	registerOperationalRoutes(mux, process, ModeSingleShot, "")

	paths := []string{"/healthz", "/readyz", "/status", "/", "/metrics"}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Result().StatusCode != http.StatusOK {
			t.Errorf("path %s: expected status 200, got %d", path, w.Result().StatusCode)
		}
	}
}

func TestHandleMetrics(t *testing.T) {
	process := claude.NewProcess(claude.DefaultOptions())
	mux := http.NewServeMux()

	registerOperationalRoutes(mux, process, ModeSingleShot, "")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	// Prometheus metrics endpoint should contain standard Go runtime metrics.
	if !strings.Contains(body, "go_goroutines") {
		t.Error("expected /metrics to contain go_goroutines metric")
	}
}

func TestRegisterOperationalRoutes_UnknownPath(t *testing.T) {
	process := claude.NewProcess(claude.DefaultOptions())
	mux := http.NewServeMux()

	registerOperationalRoutes(mux, process, ModeSingleShot, "")

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 for unknown path, got %d", w.Result().StatusCode)
	}
}

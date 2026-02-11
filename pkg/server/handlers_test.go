package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/giantswarm/klaus/pkg/claude"
)

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
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	handleReadyz(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
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

	handler := handleStatus(process, ModeSingleShot)
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
}

func TestRegisterOperationalRoutes(t *testing.T) {
	process := claude.NewProcess(claude.DefaultOptions())
	mux := http.NewServeMux()

	registerOperationalRoutes(mux, process, ModeSingleShot)

	paths := []string{"/healthz", "/readyz", "/status", "/"}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Result().StatusCode != http.StatusOK {
			t.Errorf("path %s: expected status 200, got %d", path, w.Result().StatusCode)
		}
	}
}

func TestRegisterOperationalRoutes_UnknownPath(t *testing.T) {
	process := claude.NewProcess(claude.DefaultOptions())
	mux := http.NewServeMux()

	registerOperationalRoutes(mux, process, ModeSingleShot)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 for unknown path, got %d", w.Result().StatusCode)
	}
}

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	claudepkg "github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/project"
)

type statusResponse struct {
	Name    string               `json:"name"`
	Version string               `json:"version"`
	Build   string               `json:"build"`
	Commit  string               `json:"commit"`
	Agent   claudepkg.StatusInfo `json:"agent"`
	Mode    string               `json:"mode"`
	// Owner is intentionally exposed on the unauthenticated /status endpoint
	// for observability (e.g. confirming which identity owns this instance).
	Owner string `json:"owner,omitempty"`
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "ok")
}

// handleReadyz reports whether the Claude process is ready to accept traffic.
// It returns 503 when the process is starting, stopped, or in an error state.
func handleReadyz(process claudepkg.Prompter) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		status := process.Status().Status
		switch status {
		case claudepkg.ProcessStatusStarting, claudepkg.ProcessStatusError, claudepkg.ProcessStatusStopped:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintln(w, "not ready")
			return
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "ok")
		}
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	// Go's ServeMux uses "/" as a catch-all. Return 404 for unmatched paths
	// to avoid masking routing issues and confusing monitoring.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", project.Name, project.Version())
}

func handleStatus(process claudepkg.Prompter, mode string, ownerSubject string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := statusResponse{
			Name:    project.Name,
			Version: project.Version(),
			Build:   project.BuildTimestamp(),
			Commit:  project.GitSHA(),
			Agent:   process.Status(),
			Mode:    mode,
			Owner:   ownerSubject,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("failed to encode status response", "error", err)
		}
	}
}

// handleBusy reports whether the Claude subprocess is currently busy.
// Returns 200 OK when idle/completed, 409 Conflict when busy.
// This is the operator contract used by the A2A layer to signal to callers
// that the process cannot accept new requests at this time.
func handleBusy(process claudepkg.Prompter) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if process.Status().Status == claudepkg.ProcessStatusBusy {
			w.WriteHeader(http.StatusConflict)
			_, _ = fmt.Fprintln(w, "busy")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "idle")
	}
}

func registerOperationalRoutes(mux *http.ServeMux, process claudepkg.Prompter, mode string, ownerSubject string, a2aHandler http.Handler) {
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/healthz/busy", handleBusy(process))
	mux.HandleFunc("/readyz", handleReadyz(process))
	mux.HandleFunc("/status", handleStatus(process, mode, ownerSubject))
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", rootHandler(a2aHandler))
}

// rootHandler returns a catch-all handler that dispatches POST / to a2aHandler
// when set. kagent constructs agent URLs as http://{name}.{ns}:8080 with no
// path, so POST / must reach the A2A JSON-RPC handler. Any other path falls
// through to handleRoot (which returns 404 for unmatched paths).
func rootHandler(a2aHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a2aHandler != nil && r.Method == http.MethodPost && r.URL.Path == "/" {
			a2aHandler.ServeHTTP(w, r)
			return
		}
		handleRoot(w, r)
	})
}

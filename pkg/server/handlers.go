package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	claudepkg "github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/project"
)

// statusResponse is the JSON structure returned by the /status endpoint.
type statusResponse struct {
	Name    string              `json:"name"`
	Version string              `json:"version"`
	Build   string              `json:"build"`
	Commit  string              `json:"commit"`
	Agent   claudepkg.StatusInfo `json:"agent"`
}

// handleHealthz responds with "ok" for liveness probes.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

// handleReadyz responds with "ok" for readiness probes.
func handleReadyz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

// handleRoot responds with the application name and version.
func handleRoot(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprintf(w, "%s %s\n", project.Name, project.Version())
}

// handleStatus returns a JSON status response including process information.
func handleStatus(process *claudepkg.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := statusResponse{
			Name:    project.Name,
			Version: project.Version(),
			Build:   project.BuildTimestamp(),
			Commit:  project.GitSHA(),
			Agent:   process.Status(),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("Failed to encode status response: %v", err)
		}
	}
}

// registerOperationalRoutes registers /healthz, /readyz, /status, and / on the given mux.
func registerOperationalRoutes(mux *http.ServeMux, process *claudepkg.Process) {
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/readyz", handleReadyz)
	mux.HandleFunc("/status", handleStatus(process))
	mux.HandleFunc("/", handleRoot)
}

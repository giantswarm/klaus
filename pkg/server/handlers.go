package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

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
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

// handleReadyz returns readiness status. Currently identical to healthz.
// TODO: check Claude process health (e.g., not in error state) to properly
// signal readiness to Kubernetes for load balancing.
func handleReadyz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	// Go's ServeMux uses "/" as a catch-all. Return 404 for unmatched paths
	// to avoid masking routing issues and confusing monitoring.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	fmt.Fprintf(w, "%s %s\n", project.Name, project.Version())
}

func handleStatus(process claudepkg.Prompter, mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := statusResponse{
			Name:    project.Name,
			Version: project.Version(),
			Build:   project.BuildTimestamp(),
			Commit:  project.GitSHA(),
			Agent:   process.Status(),
			Mode:    mode,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("Failed to encode status response: %v", err)
		}
	}
}

func registerOperationalRoutes(mux *http.ServeMux, process claudepkg.Prompter, mode string) {
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/readyz", handleReadyz)
	mux.HandleFunc("/status", handleStatus(process, mode))
	mux.HandleFunc("/", handleRoot)
}

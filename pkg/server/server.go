package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	claudepkg "github.com/giantswarm/klaus/pkg/claude"
	mcppkg "github.com/giantswarm/klaus/pkg/mcp"
	"github.com/giantswarm/klaus/pkg/project"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Server is the main HTTP server for Klaus. It serves:
//   - /mcp -- Streamable HTTP MCP endpoint
//   - /healthz -- Liveness probe
//   - /readyz -- Readiness probe
//   - /status -- JSON status endpoint
type Server struct {
	httpServer *http.Server
	mcpServer  *mcpserver.StreamableHTTPServer
	process    *claudepkg.Process
}

// New creates a new Klaus HTTP server.
func New(process *claudepkg.Process, port string) *Server {
	mcpSrv := mcppkg.NewServer(process)

	mux := http.NewServeMux()

	s := &Server{
		process:   process,
		mcpServer: mcpSrv,
	}

	// MCP endpoint -- delegates to the StreamableHTTPServer handler.
	mux.Handle("/mcp", mcpSrv)

	// Operational endpoints.
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/", s.handleRoot)

	s.httpServer = &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return s
}

// Start begins serving HTTP requests. It blocks until the server is shut down.
func (s *Server) Start() error {
	log.Printf("Starting %s on %s", project.Name, s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server and the MCP server.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down server...")

	// Shutdown MCP server first (closes SSE connections).
	if err := s.mcpServer.Shutdown(ctx); err != nil {
		log.Printf("MCP server shutdown error: %v", err)
	}

	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	// Ready if the server is running. We don't require the claude process
	// to be active since it only starts when a prompt is received.
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	type statusResponse struct {
		Name    string               `json:"name"`
		Version string               `json:"version"`
		Build   string               `json:"build"`
		Commit  string               `json:"commit"`
		Agent   claudepkg.StatusInfo `json:"agent"`
	}

	resp := statusResponse{
		Name:    project.Name,
		Version: project.Version(),
		Build:   project.BuildTimestamp(),
		Commit:  project.GitSHA(),
		Agent:   s.process.Status(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode status response: %v", err)
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprintf(w, "%s %s\n", project.Name, project.Version())
}

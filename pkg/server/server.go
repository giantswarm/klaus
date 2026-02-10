package server

import (
	"context"
	"log"
	"net/http"

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
}

// New creates a new Klaus HTTP server.
func New(process *claudepkg.Process, port string) *Server {
	mcpSrv := mcppkg.NewServer(process)

	mux := http.NewServeMux()

	s := &Server{
		mcpServer: mcpSrv,
	}

	// MCP endpoint -- delegates to the StreamableHTTPServer handler.
	mux.Handle("/mcp", mcpSrv)

	// Operational endpoints.
	registerOperationalRoutes(mux, process)

	s.httpServer = &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
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


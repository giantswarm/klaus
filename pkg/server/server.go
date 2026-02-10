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

// Server wraps the MCP and operational HTTP endpoints.
type Server struct {
	httpServer *http.Server
	mcpServer  *mcpserver.StreamableHTTPServer
}

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
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
	}

	return s
}

// Start blocks, serving HTTP requests until Shutdown is called.
func (s *Server) Start() error {
	log.Printf("Starting %s on %s", project.Name, s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully drains MCP sessions, then stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down server...")

	// Shutdown MCP server first (closes SSE connections).
	if err := s.mcpServer.Shutdown(ctx); err != nil {
		log.Printf("MCP server shutdown error: %v", err)
	}

	return s.httpServer.Shutdown(ctx)
}

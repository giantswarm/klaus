package server

import (
	"context"
	"log"
	"log/slog"
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

// ProcessMode describes the operating mode of the claude process.
const (
	ModeSingleShot = "single-shot"
	ModePersistent = "persistent"
)

// Config holds non-OAuth server-level configuration.
type Config struct {
	// Port is the HTTP listen port (e.g. "8080").
	Port string
	// Mode is the operating mode (ModeSingleShot or ModePersistent).
	Mode string
	// OwnerSubject restricts MCP access to the configured owner identity
	// by matching the JWT sub or email claim. When empty, no owner
	// validation is performed (backward-compatible).
	OwnerSubject string
}

// NewServer creates a Server that serves MCP and operational endpoints.
// The serverCtx controls the lifetime of background goroutines; it should
// be cancelled during server shutdown to ensure drain goroutines are cleaned up.
func NewServer(serverCtx context.Context, process claudepkg.Prompter, cfg Config) *Server {
	mcpSrv := mcppkg.NewServer(serverCtx, process)

	mux := http.NewServeMux()

	s := &Server{
		mcpServer: mcpSrv,
	}

	// MCP endpoint -- delegates to the StreamableHTTPServer handler.
	// Owner middleware is applied when OwnerSubject is configured.
	mux.Handle("/mcp", OwnerMiddleware(cfg.OwnerSubject, slog.Default())(mcpSrv))

	// Operational endpoints (bypass owner validation).
	registerOperationalRoutes(mux, process, cfg.Mode, cfg.OwnerSubject)

	s.httpServer = &http.Server{
		Addr:              ":" + cfg.Port,
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

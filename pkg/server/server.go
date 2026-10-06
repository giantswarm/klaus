package server

import (
	"context"
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
	ModeAgent = "agent"
	ModeChat  = "chat"
)

// Config holds non-OAuth server-level configuration.
type Config struct {
	// Port is the HTTP listen port (e.g. "8080").
	Port string
	// Mode is the operating mode (ModeAgent or ModeChat).
	Mode string
	// OwnerSubject restricts MCP access to the configured owner identity
	// by matching the verified token's sub or email claim. When empty, any
	// caller that Verifier accepts is allowed.
	OwnerSubject string
	// Verifier verifies the bearer token on /mcp and /v1/chat/completions.
	// When nil the endpoints are unauthenticated; the caller must have opted in.
	Verifier TokenVerifier
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

	protect := func(h http.Handler) http.Handler {
		h = OwnerMiddleware(cfg.OwnerSubject, slog.Default())(h)
		if cfg.Verifier != nil {
			h = VerifyTokenMiddleware(cfg.Verifier, slog.Default())(h)
		}
		return h
	}

	// MCP endpoint -- delegates to the StreamableHTTPServer handler.
	mux.Handle("/mcp", protect(mcpSrv))

	// Chat endpoint -- OpenAI-compatible, same protection as /mcp.
	mux.Handle("/v1/chat/completions", protect(handleChatCompletions(process)))

	// Operational endpoints (no authentication).
	registerOperationalRoutes(mux, process, cfg.Mode)

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
	slog.Info("starting server", "name", project.Name, "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully drains MCP sessions, then stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("shutting down server")

	// Shutdown MCP server first (closes SSE connections).
	if err := s.mcpServer.Shutdown(ctx); err != nil {
		slog.Error("MCP server shutdown error", "error", err)
	}

	return s.httpServer.Shutdown(ctx)
}

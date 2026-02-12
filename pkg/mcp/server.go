package mcp

import (
	"context"

	claudepkg "github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/project"

	"github.com/mark3labs/mcp-go/server"
)

// NewServer returns a StreamableHTTPServer that serves MCP at /mcp.
// The serverCtx controls the lifetime of background goroutines; it should
// be cancelled during server shutdown.
func NewServer(serverCtx context.Context, process claudepkg.Prompter) *server.StreamableHTTPServer {
	mcpServer := NewMCPServer(serverCtx, process)

	httpServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithEndpointPath("/mcp"),
	)

	return httpServer
}

// NewMCPServer returns the raw MCPServer with tools registered, for use
// when wrapping with custom middleware (e.g. OAuth). The serverCtx controls
// the lifetime of background goroutines; it should be cancelled during
// server shutdown.
func NewMCPServer(serverCtx context.Context, process claudepkg.Prompter) *server.MCPServer {
	mcpServer := server.NewMCPServer(
		project.Name,
		project.Version(),
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions("Klaus wraps a Claude Code agent. Use the 'prompt' tool to send tasks."),
	)

	RegisterTools(serverCtx, mcpServer, process)

	return mcpServer
}

package mcp

import (
	claudepkg "github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/project"

	"github.com/mark3labs/mcp-go/server"
)

// NewServer returns a StreamableHTTPServer that serves MCP at /mcp.
func NewServer(process *claudepkg.Process) *server.StreamableHTTPServer {
	mcpServer := NewMCPServer(process)

	httpServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithEndpointPath("/mcp"),
	)

	return httpServer
}

// NewMCPServer returns the raw MCPServer with tools registered, for use
// when wrapping with custom middleware (e.g. OAuth).
func NewMCPServer(process *claudepkg.Process) *server.MCPServer {
	mcpServer := server.NewMCPServer(
		project.Name,
		project.Version(),
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions("Klaus wraps a Claude Code agent. Use the 'prompt' tool to send tasks."),
	)

	RegisterTools(mcpServer, process)

	return mcpServer
}

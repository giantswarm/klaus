package mcp

import (
	claudepkg "github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/project"

	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates a configured MCP server with all Klaus tools registered.
// The returned StreamableHTTPServer serves the MCP protocol over HTTP at /mcp.
func NewServer(process *claudepkg.Process) *server.StreamableHTTPServer {
	mcpServer := NewMCPServer(process)

	httpServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithEndpointPath("/mcp"),
	)

	return httpServer
}

// NewMCPServer creates a configured MCPServer with all Klaus tools registered.
// Use this when you need the raw MCPServer (e.g., for OAuth-wrapped endpoints).
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

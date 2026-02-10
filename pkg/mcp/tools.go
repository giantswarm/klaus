package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	claudepkg "github.com/giantswarm/klaus/pkg/claude"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterTools(s *server.MCPServer, process *claudepkg.Process) {
	s.AddTools(
		promptTool(process),
		statusTool(process),
		stopTool(process),
	)
}

func promptTool(process *claudepkg.Process) server.ServerTool {
	tool := mcp.NewTool("prompt",
		mcp.WithDescription("Send a prompt to the Claude Code agent and receive the response. "+
			"The agent will autonomously read files, run commands, and edit code to complete the task."),
		mcp.WithString("message",
			mcp.Required(),
			mcp.Description("The prompt or task description to send to the Claude agent"),
		),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		message, err := request.RequireString("message")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if strings.TrimSpace(message) == "" {
			return mcp.NewToolResultError("message must not be empty"), nil
		}

		result, messages, err := process.RunSync(ctx, message)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("claude execution failed: %v", err)), nil
		}

		// Build a structured response including cost info.
		response := struct {
			Result       string  `json:"result"`
			MessageCount int     `json:"message_count"`
			TotalCost    float64 `json:"total_cost_usd,omitempty"`
		}{
			Result:       result,
			MessageCount: len(messages),
		}

		// Extract cost from the last result message.
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Type == claudepkg.MessageTypeResult {
				response.TotalCost = messages[i].TotalCost
				break
			}
		}

		data, err := json.Marshal(response)
		if err != nil {
			return mcp.NewToolResultText(result), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}

	return server.ServerTool{Tool: tool, Handler: handler}
}

func statusTool(process *claudepkg.Process) server.ServerTool {
	tool := mcp.NewTool("status",
		mcp.WithDescription("Get the current status of the Claude Code agent (starting, idle, busy, stopped, error)"),
	)

	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		data, err := process.MarshalStatus()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal status: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}

	return server.ServerTool{Tool: tool, Handler: handler}
}

func stopTool(process *claudepkg.Process) server.ServerTool {
	tool := mcp.NewTool("stop",
		mcp.WithDescription("Stop the currently running Claude Code agent task"),
	)

	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := process.Stop(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to stop agent: %v", err)), nil
		}
		return mcp.NewToolResultText("agent stopped"), nil
	}

	return server.ServerTool{Tool: tool, Handler: handler}
}

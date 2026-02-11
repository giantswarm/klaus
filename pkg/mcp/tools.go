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
		mcp.WithString("session_id",
			mcp.Description("Optional session UUID to use or resume a specific conversation"),
		),
		mcp.WithString("agent",
			mcp.Description("Optional named agent persona to use for this prompt (must be defined in server config)"),
		),
		mcp.WithString("json_schema",
			mcp.Description("Optional JSON Schema to constrain the output format"),
		),
		mcp.WithNumber("max_budget_usd",
			mcp.Description("Optional per-invocation spending cap in USD (0 = no limit)"),
		),
		mcp.WithString("effort",
			mcp.Description("Optional effort level: low, medium, or high"),
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

		// Build per-run overrides from optional parameters.
		var runOpts claudepkg.RunOptions

		if v, err := optionalString(request, "session_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if v != "" {
			runOpts.SessionID = v
		}

		if v, err := optionalString(request, "agent"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if v != "" {
			runOpts.ActiveAgent = v
		}

		if v, err := optionalString(request, "json_schema"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if v != "" {
			runOpts.JSONSchema = v
		}

		if v, err := optionalFloat(request, "max_budget_usd"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if v > 0 {
			runOpts.MaxBudgetUSD = v
		}

		if v, err := optionalString(request, "effort"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if v != "" {
			runOpts.Effort = v
		}

		result, messages, err := process.RunSyncWithOptions(ctx, message, &runOpts)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("claude execution failed: %v", err)), nil
		}

		// Build a structured response including cost info.
		response := struct {
			Result       string  `json:"result"`
			MessageCount int     `json:"message_count"`
			TotalCost    float64 `json:"total_cost_usd,omitempty"`
			SessionID    string  `json:"session_id,omitempty"`
		}{
			Result:       result,
			MessageCount: len(messages),
		}

		// Extract cost and session ID from messages.
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Type == claudepkg.MessageTypeResult {
				response.TotalCost = messages[i].TotalCost
				break
			}
		}
		for _, msg := range messages {
			if msg.Type == claudepkg.MessageTypeSystem && msg.SessionID != "" {
				response.SessionID = msg.SessionID
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
		mcp.WithDescription("Get the current status of the Claude Code agent including progress information "+
			"(status, session_id, cost, message_count, tool_call_count, last_message, last_tool_name)"),
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

// optionalString extracts an optional string parameter from the request.
func optionalString(request mcp.CallToolRequest, key string) (string, error) {
	args := request.GetArguments()
	v, ok := args[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("parameter %q must be a string", key)
	}
	return s, nil
}

// optionalFloat extracts an optional float parameter from the request.
func optionalFloat(request mcp.CallToolRequest, key string) (float64, error) {
	args := request.GetArguments()
	v, ok := args[key]
	if !ok || v == nil {
		return 0, nil
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("parameter %q must be a number", key)
	}
	return f, nil
}

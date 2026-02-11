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

func RegisterTools(s *server.MCPServer, process claudepkg.Prompter) {
	s.AddTools(
		promptTool(process),
		statusTool(process),
		stopTool(process),
	)
}

func promptTool(process claudepkg.Prompter) server.ServerTool {
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
		mcp.WithString("resume",
			mcp.Description("Optional session ID to resume a previous conversation"),
		),
		mcp.WithBoolean("continue",
			mcp.Description("Optional: continue the most recent conversation in the working directory"),
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
		mcp.WithBoolean("fork_session",
			mcp.Description("Optional: fork the session when resuming, creating a new session ID"),
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

		if v, err := optionalString(request, "resume"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if v != "" {
			runOpts.Resume = v
		}

		if v, err := optionalBool(request, "continue"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if v {
			runOpts.ContinueSession = true
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
			if err := claudepkg.ValidateEffort(v); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			runOpts.Effort = v
		}

		if v, err := optionalBool(request, "fork_session"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if v {
			runOpts.ForkSession = true
		}

		// Extract progress token for streaming progress notifications.
		var progressToken mcp.ProgressToken
		if request.Params.Meta != nil {
			progressToken = request.Params.Meta.ProgressToken
		}

		// Use the streaming Run method so we can send progress notifications.
		ch, err := process.RunWithOptions(ctx, message, &runOpts)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("claude execution failed: %v", err)), nil
		}

		mcpServer := server.ServerFromContext(ctx)

		var messages []claudepkg.StreamMessage
		var progressCount float64

	loop:
		for {
			select {
			case <-ctx.Done():
				_ = process.Stop()
				return mcp.NewToolResultError(fmt.Sprintf("cancelled: %v", ctx.Err())), nil
			case msg, ok := <-ch:
				if !ok {
					break loop
				}
				messages = append(messages, msg)

				// Send progress notification if client requested it.
				if progressToken != nil && mcpServer != nil {
					progressMsg := progressMessage(msg)
					if progressMsg != "" {
						progressCount++
						_ = mcpServer.SendNotificationToClient(
							ctx,
							"notifications/progress",
							map[string]any{
								"progressToken": progressToken,
								"progress":      progressCount,
								"message":       progressMsg,
							},
						)
					}
				}
			}
		}

		resultText := claudepkg.CollectResultText(messages)

		// Build a structured response including cost info.
		response := struct {
			Result       string  `json:"result"`
			MessageCount int     `json:"message_count"`
			TotalCost    float64 `json:"total_cost_usd,omitempty"`
			SessionID    string  `json:"session_id,omitempty"`
		}{
			Result:       resultText,
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
			return mcp.NewToolResultText(resultText), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}

	return server.ServerTool{Tool: tool, Handler: handler}
}

// progressMessage returns a human-readable progress message for a stream message,
// or empty string if the message isn't worth reporting.
func progressMessage(msg claudepkg.StreamMessage) string {
	switch msg.Type {
	case claudepkg.MessageTypeSystem:
		if msg.SessionID != "" {
			return fmt.Sprintf("Session started: %s", msg.SessionID)
		}
	case claudepkg.MessageTypeAssistant:
		if msg.Subtype == claudepkg.SubtypeToolUse {
			return fmt.Sprintf("Using tool: %s", msg.ToolName)
		}
		if msg.Subtype == claudepkg.SubtypeText && msg.Text != "" {
			return fmt.Sprintf("Assistant: %s", claudepkg.Truncate(msg.Text, 100))
		}
	case claudepkg.MessageTypeResult:
		return "Task completed"
	}
	return ""
}

func statusTool(process claudepkg.Prompter) server.ServerTool {
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

func stopTool(process claudepkg.Prompter) server.ServerTool {
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

// optionalBool extracts an optional boolean parameter from the request.
func optionalBool(request mcp.CallToolRequest, key string) (bool, error) {
	args := request.GetArguments()
	v, ok := args[key]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("parameter %q must be a boolean", key)
	}
	return b, nil
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

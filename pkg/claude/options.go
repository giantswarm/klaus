package claude

import (
	"fmt"
	"strings"
)

// Options holds configuration for spawning a Claude CLI subprocess.
type Options struct {
	// Model is the Claude model to use (e.g. "claude-sonnet-4-20250514").
	Model string

	// SystemPrompt is an optional system prompt to pass to Claude.
	SystemPrompt string

	// AppendSystemPrompt is additional text appended to the default system prompt.
	AppendSystemPrompt string

	// AllowedTools restricts which tools Claude can use.
	// Empty means all tools are allowed.
	AllowedTools []string

	// DisallowedTools prevents Claude from using specific tools.
	DisallowedTools []string

	// MaxTurns limits the number of agentic turns Claude will take.
	// 0 means no limit.
	MaxTurns int

	// MCPConfigPath is the path to an MCP configuration file that Claude
	// will use to connect to MCP servers.
	MCPConfigPath string

	// WorkDir is the working directory for the Claude subprocess.
	// Defaults to the current directory if empty.
	WorkDir string

	// PermissionMode controls how Claude handles tool permissions.
	// Defaults to "accept-all" for headless operation.
	PermissionMode string
}

// DefaultOptions returns Options with sensible defaults for headless operation.
func DefaultOptions() Options {
	return Options{
		PermissionMode: "accept-all",
		MaxTurns:       0,
	}
}

// args builds the CLI argument list for the claude command.
func (o Options) args() []string {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
	}

	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}

	if o.SystemPrompt != "" {
		args = append(args, "--system-prompt", o.SystemPrompt)
	}

	if o.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", o.AppendSystemPrompt)
	}

	if o.MaxTurns > 0 {
		args = append(args, "--max-turns", intToStr(o.MaxTurns))
	}

	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}

	if o.MCPConfigPath != "" {
		args = append(args, "--mcp-config", o.MCPConfigPath)
	}

	if len(o.AllowedTools) > 0 {
		args = append(args, "--allowedTools", joinStrings(o.AllowedTools))
	}

	if len(o.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", joinStrings(o.DisallowedTools))
	}

	return args
}

func intToStr(i int) string {
	return fmt.Sprintf("%d", i)
}

func joinStrings(s []string) string {
	return strings.Join(s, ",")
}

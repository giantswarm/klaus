package claude

import (
	"strconv"
	"strings"
)

// Options configures how a Claude CLI subprocess is spawned.
type Options struct {
	Model              string   // e.g. "claude-sonnet-4-20250514"
	SystemPrompt       string   // overrides the default system prompt
	AppendSystemPrompt string   // appended to the default system prompt
	AllowedTools       []string // restrict tools; empty = all allowed
	DisallowedTools    []string // explicitly blocked tools
	MaxTurns           int      // 0 = unlimited agentic turns
	MCPConfigPath      string   // path to MCP config file
	WorkDir            string   // working directory (default: cwd)
	PermissionMode     string   // default "accept-all" for headless
}

// DefaultOptions returns sensible defaults for headless operation.
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
		args = append(args, "--max-turns", strconv.Itoa(o.MaxTurns))
	}

	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}

	if o.MCPConfigPath != "" {
		args = append(args, "--mcp-config", o.MCPConfigPath)
	}

	if len(o.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(o.AllowedTools, ","))
	}

	if len(o.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(o.DisallowedTools, ","))
	}

	return args
}

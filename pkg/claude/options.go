package claude

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// AgentConfig defines a named agent persona for Claude Code.
type AgentConfig struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// Options configures how a Claude CLI subprocess is spawned.
type Options struct {
	// Model selects the Claude model (e.g. "claude-sonnet-4-20250514", "sonnet", "opus").
	Model string
	// SystemPrompt overrides the default system prompt entirely.
	SystemPrompt string
	// AppendSystemPrompt is appended to the default system prompt.
	AppendSystemPrompt string
	// AllowedTools restricts tool access; empty means all allowed.
	// Supports patterns like "Bash(git:*)" and "Edit".
	AllowedTools []string
	// DisallowedTools explicitly blocks specific tools.
	DisallowedTools []string
	// Tools controls the base set of built-in tools available.
	// Use "default" for all tools, "" to disable all, or specific names like "Bash,Edit,Read".
	Tools []string
	// MaxTurns limits agentic turns per prompt; 0 means unlimited.
	MaxTurns int
	// MCPConfigPath is a path to an MCP servers configuration file.
	MCPConfigPath string
	// StrictMCPConfig when true only uses MCP servers from MCPConfigPath,
	// ignoring user, project, and local MCP configurations.
	StrictMCPConfig bool
	// WorkDir is the working directory for the Claude subprocess.
	WorkDir string
	// PermissionMode controls how Claude handles tool permissions.
	// Valid values: "default", "acceptEdits", "bypassPermissions", "dontAsk", "plan", "delegate".
	PermissionMode string

	// MaxBudgetUSD caps the maximum dollar spend per invocation; 0 means no limit.
	MaxBudgetUSD float64
	// Effort controls the effort level: "low", "medium", "high"; empty means default.
	Effort string
	// FallbackModel specifies a model to use when the primary is overloaded.
	FallbackModel string

	// SessionID uses a specific UUID for the conversation.
	SessionID string
	// Resume resumes a previous conversation by session ID.
	Resume string
	// ContinueSession continues the most recent conversation in the working directory.
	ContinueSession bool
	// ForkSession creates a new session ID when resuming.
	ForkSession bool
	// NoSessionPersistence disables saving sessions to disk.
	NoSessionPersistence bool

	// Agents defines named agent personas with descriptions and prompts.
	Agents map[string]AgentConfig
	// ActiveAgent selects which named agent to use for the session.
	ActiveAgent string

	// JSONSchema constrains the output to conform to a JSON Schema.
	JSONSchema string
	// IncludePartialMessages emits partial message chunks during streaming.
	IncludePartialMessages bool

	// SettingsFile is a path to a settings JSON file or inline JSON string.
	SettingsFile string
	// SettingSources controls which setting sources are loaded (comma-separated: "user,project,local").
	SettingSources string

	// PluginDirs are directories to load plugins from.
	PluginDirs []string
}

// DefaultOptions returns sensible defaults for headless operation.
func DefaultOptions() Options {
	return Options{
		PermissionMode:       "bypassPermissions",
		NoSessionPersistence: true,
		MaxTurns:             0,
	}
}

// ValidPermissionModes lists all valid permission mode values for Claude Code.
var ValidPermissionModes = []string{
	"default",
	"acceptEdits",
	"bypassPermissions",
	"dontAsk",
	"plan",
	"delegate",
}

// ValidatePermissionMode checks whether the given mode is a valid Claude Code permission mode.
func ValidatePermissionMode(mode string) error {
	for _, valid := range ValidPermissionModes {
		if mode == valid {
			return nil
		}
	}
	return fmt.Errorf("invalid permission mode %q; valid modes: %s", mode, strings.Join(ValidPermissionModes, ", "))
}

// ValidEffortLevels lists all valid effort level values for Claude Code.
var ValidEffortLevels = []string{
	"low",
	"medium",
	"high",
}

// ValidateEffort checks whether the given effort level is valid.
// An empty string is allowed (uses the CLI default).
func ValidateEffort(effort string) error {
	if effort == "" {
		return nil
	}
	for _, valid := range ValidEffortLevels {
		if effort == valid {
			return nil
		}
	}
	return fmt.Errorf("invalid effort level %q; valid levels: %s", effort, strings.Join(ValidEffortLevels, ", "))
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

	if o.FallbackModel != "" {
		args = append(args, "--fallback-model", o.FallbackModel)
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

	// When using bypassPermissions, the --dangerously-skip-permissions flag is required.
	if o.PermissionMode == "bypassPermissions" {
		args = append(args, "--dangerously-skip-permissions")
	}

	if o.MCPConfigPath != "" {
		args = append(args, "--mcp-config", o.MCPConfigPath)
	}

	if o.StrictMCPConfig {
		args = append(args, "--strict-mcp-config")
	}

	if len(o.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(o.AllowedTools, ","))
	}

	if len(o.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(o.DisallowedTools, ","))
	}

	if len(o.Tools) > 0 {
		args = append(args, "--tools", strings.Join(o.Tools, ","))
	}

	// Operational controls.
	if o.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", o.MaxBudgetUSD))
	}

	if o.Effort != "" {
		args = append(args, "--effort", o.Effort)
	}

	// Session management.
	if o.SessionID != "" {
		args = append(args, "--session-id", o.SessionID)
	}

	if o.Resume != "" {
		args = append(args, "--resume", o.Resume)
	}

	if o.ContinueSession {
		args = append(args, "--continue")
	}

	if o.ForkSession {
		args = append(args, "--fork-session")
	}

	if o.NoSessionPersistence {
		args = append(args, "--no-session-persistence")
	}

	// Agent definitions.
	if len(o.Agents) > 0 {
		data, err := json.Marshal(o.Agents)
		if err == nil {
			args = append(args, "--agents", string(data))
		}
	}

	if o.ActiveAgent != "" {
		args = append(args, "--agent", o.ActiveAgent)
	}

	// Structured output.
	if o.JSONSchema != "" {
		args = append(args, "--json-schema", o.JSONSchema)
	}

	if o.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}

	// Settings.
	if o.SettingsFile != "" {
		args = append(args, "--settings", o.SettingsFile)
	}

	if o.SettingSources != "" {
		args = append(args, "--setting-sources", o.SettingSources)
	}

	// Plugins.
	for _, dir := range o.PluginDirs {
		args = append(args, "--plugin-dir", dir)
	}

	return args
}

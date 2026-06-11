package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AgentConfig defines a custom subagent for Claude Code.
//
// Subagents are specialized AI assistants that the main Claude Code agent can
// delegate work to via the built-in "Task" tool. Each subagent runs in its own
// context window with a custom system prompt (Prompt), specific tool access
// (Tools/DisallowedTools), and independent permissions. When the main agent
// encounters a task matching a subagent's Description, it delegates to that
// subagent, which works independently and returns results.
//
// Subagent definitions are passed to the CLI via the --agents flag as a JSON
// map (highest priority, session-scoped). They can also be defined as markdown
// files in .claude/agents/ directories (loaded via --add-dir).
//
// This is distinct from agent selection (--agent / ActiveAgent), which changes
// the top-level agent persona for the entire session. When running as the main
// agent via --agent, that agent can spawn subagents defined here via Task.
//
// See https://code.claude.com/docs/en/sub-agents for full documentation.
//
// The Description and Prompt fields are the minimum required.
type AgentConfig struct {
	Description     string         `json:"description"`
	Prompt          string         `json:"prompt"`
	Tools           []string       `json:"tools,omitempty"`
	DisallowedTools []string       `json:"disallowedTools,omitempty"`
	Model           string         `json:"model,omitempty"`
	PermissionMode  string         `json:"permissionMode,omitempty"`
	MaxTurns        int            `json:"maxTurns,omitempty"`
	Skills          []string       `json:"skills,omitempty"`
	McpServers      map[string]any `json:"mcpServers,omitempty"`
	Hooks           map[string]any `json:"hooks,omitempty"`
	Memory          string         `json:"memory,omitempty"`
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

	// Agents defines custom subagents that the main agent can delegate to via
	// the "Task" tool. Passed to the CLI as --agents JSON. Each subagent runs
	// in its own context with its own prompt, tools, and model.
	// See https://code.claude.com/docs/en/sub-agents
	Agents map[string]AgentConfig
	// ActiveAgent selects which agent runs as the top-level agent for the
	// session (--agent flag). This changes who handles the prompt, not which
	// subagents are available. The selected agent can still spawn subagents
	// defined in Agents or discovered via AddDirs.
	ActiveAgent string

	// JSONSchema constrains the output to conform to a JSON Schema.
	JSONSchema string

	// SettingsFile is a path to a settings JSON file or inline JSON string.
	SettingsFile string
	// SettingSources controls which setting sources are loaded (comma-separated: "user,project,local").
	SettingSources string

	// ResultDir overrides the directory where session results are persisted.
	// When empty, ResultStorePath determines the default location outside
	// the workspace (e.g. $HOME/.klaus/results/).
	ResultDir string

	// PluginDirs are directories to load plugins from.
	PluginDirs []string
	// AddDirs are directories to search for .claude/ subdirectories containing
	// skills (.claude/skills/) and subagent definitions (.claude/agents/).
	// Subagents found via AddDirs have lower priority than those in Agents (--agents).
	// See https://code.claude.com/docs/en/sub-agents#choose-the-subagent-scope
	AddDirs []string

	// RetryMaxAttempts is the maximum number of times the persistent subprocess
	// watchdog will restart with --resume on unexpected exit. 0 selects the
	// default of 3.
	RetryMaxAttempts int
	// RetryBaseBackoff is the initial backoff duration before the first restart
	// attempt. Each subsequent attempt doubles the duration.
	// Defaults to 2s.
	RetryBaseBackoff time.Duration

	// ResumeSessionID, when set, passes --resume <id> at PersistentProcess
	// startup. Use this to continue a named session after a pod restart without
	// requiring a first-turn warm-up. Only applied via PersistentArgs; the
	// single-shot Process uses RunOptions.Resume instead.
	ResumeSessionID string
}

// Permission modes recognised by the Claude Code CLI.
const (
	PermissionModeDefault  = "default"
	PermissionModeAccept   = "acceptEdits"
	PermissionModeBypass   = "bypassPermissions"
	PermissionModeDontAsk  = "dontAsk"
	PermissionModePlan     = "plan"
	PermissionModeDelegate = "delegate"
)

// Effort level values recognised by the Claude Code CLI.
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
)

// streamJSONFormat is the CLI value for stream-json input/output formatting.
const streamJSONFormat = "stream-json"

// DefaultOptions returns sensible defaults for headless operation.
func DefaultOptions() Options {
	return Options{
		PermissionMode:       PermissionModeBypass,
		NoSessionPersistence: true,
		MaxTurns:             0,
	}
}

// mcpServerNames parses the MCP config file and returns sorted server names.
// These are used to auto-generate --allowedTools patterns so that MCP tools
// are not blocked by Claude's permission system.
func (o Options) mcpServerNames() []string {
	if o.MCPConfigPath == "" {
		return nil
	}
	data, err := os.ReadFile(o.MCPConfigPath)
	if err != nil {
		return nil
	}
	var cfg struct {
		McpServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.McpServers))
	for name := range cfg.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidPermissionModes lists all valid permission mode values for Claude Code.
var ValidPermissionModes = []string{
	PermissionModeDefault,
	PermissionModeAccept,
	PermissionModeBypass,
	PermissionModeDontAsk,
	PermissionModePlan,
	PermissionModeDelegate,
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
	EffortLow,
	EffortMedium,
	EffortHigh,
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

// baseArgs returns the CLI arguments shared by both single-shot and persistent modes.
// This includes model, prompts, permissions, MCP config, tools, operational controls,
// agents, structured output, settings, and plugins. It does NOT include mode-specific
// prefixes or session management flags (which differ between modes).
func (o Options) baseArgs() []string {
	var args []string

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
	if o.PermissionMode == PermissionModeBypass {
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

	for _, name := range o.mcpServerNames() {
		args = append(args, "--allowedTools", fmt.Sprintf("mcp__%s__*", name))
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

	// Additional directories.
	for _, dir := range o.AddDirs {
		args = append(args, "--add-dir", dir)
	}

	return args
}

// args builds the full CLI argument list for single-shot mode.
func (o Options) args() []string {
	args := []string{
		"--print",
		"--output-format", streamJSONFormat,
		"--verbose",
		"--include-partial-messages",
	}
	args = append(args, o.baseArgs()...)

	// Session management (per-subprocess, only for single-shot mode).
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

	return args
}

// PersistentArgs builds the CLI argument list for persistent (bidirectional stream-json) mode.
// It shares the base arguments with single-shot mode but adds --input-format stream-json
// and --replay-user-messages, and omits session management flags since the persistent
// subprocess maintains a single long-running session.
//
// When ResumeSessionID is set, --resume <id> is appended so the subprocess
// continues an existing session from startup (e.g. after a pod restart).
func (o Options) PersistentArgs() []string {
	args := []string{
		"--print",
		"--input-format", streamJSONFormat,
		"--output-format", streamJSONFormat,
		"--replay-user-messages",
		"--verbose",
		"--include-partial-messages",
	}
	args = append(args, o.baseArgs()...)
	if o.ResumeSessionID != "" {
		args = append(args, "--resume", o.ResumeSessionID)
	}
	return args
}

// persistentRestartArgs returns the CLI argument list for restarting a persistent
// subprocess after an unexpected exit. It strips ResumeSessionID (the pod-startup
// resume) and appends --resume <sessionID> (the live session) so that exactly one
// --resume flag is present. Callers must only invoke this when sessionID is
// non-empty; a cold start must use PersistentArgs instead.
func (o Options) persistentRestartArgs(sessionID string) []string {
	o2 := o
	o2.ResumeSessionID = ""
	args := o2.PersistentArgs()
	return append(args, "--resume", sessionID)
}

// retryConfig returns the effective retry settings, applying defaults when the
// Options fields are zero.
func (o Options) retryConfig() (maxAttempts int, baseBackoff time.Duration) {
	maxAttempts = o.RetryMaxAttempts
	if maxAttempts == 0 {
		maxAttempts = defaultRetryMaxAttempts
	}
	baseBackoff = o.RetryBaseBackoff
	if baseBackoff == 0 {
		baseBackoff = defaultRetryBaseBackoff
	}
	return maxAttempts, baseBackoff
}

const (
	defaultRetryMaxAttempts = 3
	defaultRetryBaseBackoff = 2 * time.Second
)

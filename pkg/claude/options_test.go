package claude

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.PermissionMode != "bypassPermissions" {
		t.Errorf("expected PermissionMode %q, got %q", "bypassPermissions", opts.PermissionMode)
	}
	if opts.MaxTurns != 0 {
		t.Errorf("expected MaxTurns 0, got %d", opts.MaxTurns)
	}
	if !opts.NoSessionPersistence {
		t.Error("expected NoSessionPersistence to be true by default")
	}
}

func TestArgs_Minimal(t *testing.T) {
	opts := DefaultOptions()
	args := opts.args()

	assertContains(t, args, "--print")
	assertContains(t, args, "--output-format")
	assertContains(t, args, "stream-json")
	assertContains(t, args, "--verbose")
	assertContains(t, args, "--permission-mode")
	assertContains(t, args, "bypassPermissions")
	assertContains(t, args, "--dangerously-skip-permissions")
	assertContains(t, args, "--no-session-persistence")

	// Model should not appear when empty.
	assertNotContains(t, args, "--model")
	// Optional flags should not appear when empty.
	assertNotContains(t, args, "--max-budget-usd")
	assertNotContains(t, args, "--effort")
	assertNotContains(t, args, "--fallback-model")
	assertNotContains(t, args, "--json-schema")
	assertNotContains(t, args, "--strict-mcp-config")
	assertNotContains(t, args, "--session-id")
	assertNotContains(t, args, "--resume")
	assertNotContains(t, args, "--continue")
	assertNotContains(t, args, "--fork-session")
	assertNotContains(t, args, "--agents")
	assertNotContains(t, args, "--agent")
	assertNotContains(t, args, "--settings")
	assertNotContains(t, args, "--setting-sources")
	assertNotContains(t, args, "--tools")
	assertNotContains(t, args, "--plugin-dir")
	assertNotContains(t, args, "--add-dir")
	assertNotContains(t, args, "--include-partial-messages")
}

func TestArgs_AllOptions(t *testing.T) {
	opts := Options{
		Model:              "claude-sonnet-4-20250514",
		SystemPrompt:       "You are helpful.",
		AppendSystemPrompt: "Be concise.",
		AllowedTools:       []string{"read", "write"},
		DisallowedTools:    []string{"exec"},
		Tools:              []string{"Bash", "Edit", "Read"},
		MaxTurns:           5,
		MCPConfigPath:      "/etc/mcp.json",
		StrictMCPConfig:    true,
		PermissionMode:     "acceptEdits",
		MaxBudgetUSD:       10.50,
		Effort:             "high",
		FallbackModel:      "sonnet",
		SessionID:          "abc-123",
		JSONSchema:         `{"type":"object"}`,
		SettingsFile:       "/etc/settings.json",
		SettingSources:     "user,project",
		PluginDirs:         []string{"/plugins/a", "/plugins/b"},
		AddDirs:            []string{"/skills/a", "/skills/b"},
		Agents: map[string]AgentConfig{
			"reviewer": {Description: "Reviews code", Prompt: "You review"},
		},
		ActiveAgent:            "reviewer",
		IncludePartialMessages: true,
		NoSessionPersistence:   true,
	}
	args := opts.args()

	assertContainsSequence(t, args, "--model", "claude-sonnet-4-20250514")
	assertContainsSequence(t, args, "--system-prompt", "You are helpful.")
	assertContainsSequence(t, args, "--append-system-prompt", "Be concise.")
	assertContainsSequence(t, args, "--max-turns", "5")
	assertContainsSequence(t, args, "--permission-mode", "acceptEdits")
	assertContainsSequence(t, args, "--mcp-config", "/etc/mcp.json")
	assertContains(t, args, "--strict-mcp-config")
	assertContainsSequence(t, args, "--allowedTools", "read,write")
	assertContainsSequence(t, args, "--disallowedTools", "exec")
	assertContainsSequence(t, args, "--tools", "Bash,Edit,Read")
	assertContainsSequence(t, args, "--max-budget-usd", "10.50")
	assertContainsSequence(t, args, "--effort", "high")
	assertContainsSequence(t, args, "--fallback-model", "sonnet")
	assertContainsSequence(t, args, "--session-id", "abc-123")
	assertContainsSequence(t, args, "--json-schema", `{"type":"object"}`)
	assertContainsSequence(t, args, "--settings", "/etc/settings.json")
	assertContainsSequence(t, args, "--setting-sources", "user,project")
	assertContainsSequence(t, args, "--agent", "reviewer")
	assertContains(t, args, "--agents")
	assertContains(t, args, "--include-partial-messages")
	assertContains(t, args, "--no-session-persistence")

	// Plugin dirs should appear as separate --plugin-dir flags.
	pluginDirCount := 0
	for _, a := range args {
		if a == "--plugin-dir" {
			pluginDirCount++
		}
	}
	if pluginDirCount != 2 {
		t.Errorf("expected 2 --plugin-dir flags, got %d", pluginDirCount)
	}

	// Add dirs should appear as separate --add-dir flags.
	addDirCount := 0
	for _, a := range args {
		if a == "--add-dir" {
			addDirCount++
		}
	}
	if addDirCount != 2 {
		t.Errorf("expected 2 --add-dir flags, got %d", addDirCount)
	}

	// acceptEdits should NOT add --dangerously-skip-permissions.
	assertNotContains(t, args, "--dangerously-skip-permissions")
}

func TestArgs_BypassPermissionsAddsDangerousFlag(t *testing.T) {
	opts := Options{
		PermissionMode: "bypassPermissions",
	}
	args := opts.args()

	assertContainsSequence(t, args, "--permission-mode", "bypassPermissions")
	assertContains(t, args, "--dangerously-skip-permissions")
}

func TestArgs_NonBypassPermissionsDoesNotAddDangerousFlag(t *testing.T) {
	for _, mode := range []string{"default", "acceptEdits", "dontAsk", "plan", "delegate"} {
		t.Run(mode, func(t *testing.T) {
			opts := Options{PermissionMode: mode}
			args := opts.args()

			assertContainsSequence(t, args, "--permission-mode", mode)
			assertNotContains(t, args, "--dangerously-skip-permissions")
		})
	}
}

func TestArgs_ZeroMaxTurns(t *testing.T) {
	opts := Options{MaxTurns: 0}
	args := opts.args()

	assertNotContains(t, args, "--max-turns")
}

func TestArgs_SessionManagement(t *testing.T) {
	// Resume
	opts := Options{Resume: "sess-456"}
	args := opts.args()
	assertContainsSequence(t, args, "--resume", "sess-456")
	assertNotContains(t, args, "--continue")

	// Continue
	opts = Options{ContinueSession: true}
	args = opts.args()
	assertContains(t, args, "--continue")
	assertNotContains(t, args, "--resume")

	// Fork session
	opts = Options{Resume: "sess-789", ForkSession: true}
	args = opts.args()
	assertContainsSequence(t, args, "--resume", "sess-789")
	assertContains(t, args, "--fork-session")
}

func TestValidatePermissionMode(t *testing.T) {
	// Valid modes should not error.
	for _, mode := range ValidPermissionModes {
		if err := ValidatePermissionMode(mode); err != nil {
			t.Errorf("expected mode %q to be valid, got error: %v", mode, err)
		}
	}

	// Invalid modes should error.
	invalidModes := []string{"accept-all", "bypass", "skip", "", "BYPASSPERMISSIONS"}
	for _, mode := range invalidModes {
		if err := ValidatePermissionMode(mode); err == nil {
			t.Errorf("expected mode %q to be invalid, but got no error", mode)
		}
	}
}

func TestValidateEffort(t *testing.T) {
	// Empty string should be valid (uses default).
	if err := ValidateEffort(""); err != nil {
		t.Errorf("expected empty effort to be valid, got error: %v", err)
	}

	// Valid levels should not error.
	for _, level := range ValidEffortLevels {
		if err := ValidateEffort(level); err != nil {
			t.Errorf("expected effort %q to be valid, got error: %v", level, err)
		}
	}

	// Invalid levels should error.
	invalidLevels := []string{"hig", "LOW", "Medium", "extreme", "auto"}
	for _, level := range invalidLevels {
		if err := ValidateEffort(level); err == nil {
			t.Errorf("expected effort %q to be invalid, but got no error", level)
		}
	}
}

func TestArgs_ExtendedAgentConfig(t *testing.T) {
	opts := Options{
		Agents: map[string]AgentConfig{
			"reviewer": {
				Description: "Reviews code",
				Prompt:      "You review",
				Tools:       []string{"Read", "Grep"},
				Model:       "haiku",
				MaxTurns:    5,
			},
		},
	}
	args := opts.args()

	agentsJSON := findFlagValue(t, args, "--agents")
	var parsed map[string]AgentConfig
	if err := json.Unmarshal([]byte(agentsJSON), &parsed); err != nil {
		t.Fatalf("failed to unmarshal agents JSON: %v", err)
	}

	reviewer, ok := parsed["reviewer"]
	if !ok {
		t.Fatal("expected agents to contain 'reviewer'")
	}
	if reviewer.Description != "Reviews code" {
		t.Errorf("expected description %q, got %q", "Reviews code", reviewer.Description)
	}
	if len(reviewer.Tools) != 2 || reviewer.Tools[0] != "Read" || reviewer.Tools[1] != "Grep" {
		t.Errorf("expected tools [Read, Grep], got %v", reviewer.Tools)
	}
	if reviewer.Model != "haiku" {
		t.Errorf("expected model %q, got %q", "haiku", reviewer.Model)
	}
	if reviewer.MaxTurns != 5 {
		t.Errorf("expected maxTurns 5, got %d", reviewer.MaxTurns)
	}
}

func TestArgs_AgentConfigBackwardCompatibility(t *testing.T) {
	// Existing AgentConfig with only Description and Prompt should still work
	// and omit empty optional fields from JSON.
	opts := Options{
		Agents: map[string]AgentConfig{
			"simple": {Description: "Simple agent", Prompt: "You are simple."},
		},
	}
	args := opts.args()

	agentsJSON := findFlagValue(t, args, "--agents")
	var parsed map[string]AgentConfig
	if err := json.Unmarshal([]byte(agentsJSON), &parsed); err != nil {
		t.Fatalf("failed to unmarshal agents JSON: %v", err)
	}

	simple, ok := parsed["simple"]
	if !ok {
		t.Fatal("expected agents to contain 'simple'")
	}
	if simple.Description != "Simple agent" {
		t.Errorf("expected description %q, got %q", "Simple agent", simple.Description)
	}
	// Optional fields must be zero-valued.
	if len(simple.Tools) != 0 {
		t.Errorf("expected empty tools, got %v", simple.Tools)
	}
	if simple.Model != "" {
		t.Errorf("expected empty model, got %q", simple.Model)
	}
	if simple.MaxTurns != 0 {
		t.Errorf("expected zero maxTurns, got %d", simple.MaxTurns)
	}

	// Verify omitempty works: the raw JSON should not contain optional keys.
	if strings.Contains(agentsJSON, `"tools"`) {
		t.Errorf("expected agents JSON to omit empty tools, got %s", agentsJSON)
	}
	if strings.Contains(agentsJSON, `"model"`) {
		t.Errorf("expected agents JSON to omit empty model, got %s", agentsJSON)
	}
}

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("expected args to contain %q, got %v", want, args)
}

func assertNotContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			t.Errorf("expected args NOT to contain %q, got %v", want, args)
			return
		}
	}
}

func assertContainsSequence(t *testing.T, args []string, key, value string) {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return
		}
	}
	t.Errorf("expected args to contain %q %q, got %v", key, value, args)
}

func findFlagValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	t.Fatalf("flag %q not found in args %v", flag, args)
	return ""
}

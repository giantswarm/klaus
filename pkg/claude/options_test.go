package claude

import (
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
	// Optional flags should not appear when not set.
	assertNotContains(t, args, "--max-budget-usd")
	assertNotContains(t, args, "--effort")
	assertNotContains(t, args, "--fallback-model")
	assertNotContains(t, args, "--strict-mcp-config")
	assertNotContains(t, args, "--json-schema")
	assertNotContains(t, args, "--include-partial-messages")
	assertNotContains(t, args, "--agents")
	assertNotContains(t, args, "--agent")
	assertNotContains(t, args, "--tools")
	assertNotContains(t, args, "--settings")
	assertNotContains(t, args, "--setting-sources")
	assertNotContains(t, args, "--plugin-dir")
	assertNotContains(t, args, "--session-id")
	assertNotContains(t, args, "--resume")
	assertNotContains(t, args, "--continue")
	assertNotContains(t, args, "--fork-session")
}

func TestArgs_AllOptions(t *testing.T) {
	opts := Options{
		Model:                  "claude-sonnet-4-20250514",
		SystemPrompt:           "You are helpful.",
		AppendSystemPrompt:     "Be concise.",
		AllowedTools:           []string{"read", "write"},
		DisallowedTools:        []string{"exec"},
		Tools:                  []string{"Bash", "Edit", "Read"},
		MaxTurns:               5,
		MCPConfigPath:          "/etc/mcp.json",
		StrictMCPConfig:        true,
		PermissionMode:         "acceptEdits",
		MaxBudgetUSD:           10.50,
		Effort:                 "high",
		FallbackModel:          "sonnet",
		SessionID:              "abc-123",
		JSONSchema:             `{"type":"object"}`,
		IncludePartialMessages: true,
		SettingsFile:           "/etc/settings.json",
		SettingSources:         "user,project",
		PluginDirs:             []string{"/plugins/a", "/plugins/b"},
		Agents: map[string]AgentConfig{
			"reviewer": {Description: "Reviews code", Prompt: "You review code."},
		},
		ActiveAgent: "reviewer",
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
	assertContains(t, args, "--include-partial-messages")
	assertContainsSequence(t, args, "--settings", "/etc/settings.json")
	assertContainsSequence(t, args, "--setting-sources", "user,project")
	assertContainsSequence(t, args, "--agent", "reviewer")
	assertContains(t, args, "--agents")

	// acceptEdits should NOT add --dangerously-skip-permissions.
	assertNotContains(t, args, "--dangerously-skip-permissions")

	// Plugin dirs should appear twice (one per dir).
	count := 0
	for _, a := range args {
		if a == "--plugin-dir" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 --plugin-dir flags, got %d", count)
	}
}

func TestArgs_BypassPermissionsAddsDangerousFlag(t *testing.T) {
	opts := Options{PermissionMode: "bypassPermissions"}
	args := opts.args()

	assertContainsSequence(t, args, "--permission-mode", "bypassPermissions")
	assertContains(t, args, "--dangerously-skip-permissions")
}

func TestArgs_NonBypassPermissionsNoDangerousFlag(t *testing.T) {
	modes := []string{"acceptEdits", "dontAsk", "plan", "delegate", "default"}
	for _, mode := range modes {
		opts := Options{PermissionMode: mode}
		args := opts.args()
		assertNotContains(t, args, "--dangerously-skip-permissions")
	}
}

func TestArgs_ZeroMaxTurns(t *testing.T) {
	opts := Options{MaxTurns: 0}
	args := opts.args()

	assertNotContains(t, args, "--max-turns")
}

func TestArgs_SessionManagement(t *testing.T) {
	opts := Options{
		Resume:          "session-abc",
		ContinueSession: true,
		ForkSession:     true,
	}
	args := opts.args()

	assertContainsSequence(t, args, "--resume", "session-abc")
	assertContains(t, args, "--continue")
	assertContains(t, args, "--fork-session")
}

func TestArgs_NoSessionPersistence(t *testing.T) {
	opts := Options{NoSessionPersistence: true}
	args := opts.args()
	assertContains(t, args, "--no-session-persistence")

	opts2 := Options{NoSessionPersistence: false}
	args2 := opts2.args()
	assertNotContains(t, args2, "--no-session-persistence")
}

func TestValidatePermissionMode(t *testing.T) {
	// Valid modes.
	for _, mode := range ValidPermissionModes {
		if err := ValidatePermissionMode(mode); err != nil {
			t.Errorf("expected mode %q to be valid, got error: %v", mode, err)
		}
	}

	// Invalid modes.
	invalidModes := []string{"accept-all", "ask", "none", ""}
	for _, mode := range invalidModes {
		if err := ValidatePermissionMode(mode); err == nil {
			t.Errorf("expected mode %q to be invalid, got nil error", mode)
		}
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

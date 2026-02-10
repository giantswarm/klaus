package claude

import (
	"testing"
)

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.PermissionMode != "accept-all" {
		t.Errorf("expected PermissionMode %q, got %q", "accept-all", opts.PermissionMode)
	}
	if opts.MaxTurns != 0 {
		t.Errorf("expected MaxTurns 0, got %d", opts.MaxTurns)
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
	assertContains(t, args, "accept-all")

	// Model should not appear when empty.
	assertNotContains(t, args, "--model")
}

func TestArgs_AllOptions(t *testing.T) {
	opts := Options{
		Model:              "claude-sonnet-4-20250514",
		SystemPrompt:       "You are helpful.",
		AppendSystemPrompt: "Be concise.",
		AllowedTools:       []string{"read", "write"},
		DisallowedTools:    []string{"exec"},
		MaxTurns:           5,
		MCPConfigPath:      "/etc/mcp.json",
		PermissionMode:     "ask",
	}
	args := opts.args()

	assertContainsSequence(t, args, "--model", "claude-sonnet-4-20250514")
	assertContainsSequence(t, args, "--system-prompt", "You are helpful.")
	assertContainsSequence(t, args, "--append-system-prompt", "Be concise.")
	assertContainsSequence(t, args, "--max-turns", "5")
	assertContainsSequence(t, args, "--permission-mode", "ask")
	assertContainsSequence(t, args, "--mcp-config", "/etc/mcp.json")
	assertContainsSequence(t, args, "--allowedTools", "read,write")
	assertContainsSequence(t, args, "--disallowedTools", "exec")
}

func TestArgs_ZeroMaxTurns(t *testing.T) {
	opts := Options{MaxTurns: 0}
	args := opts.args()

	assertNotContains(t, args, "--max-turns")
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

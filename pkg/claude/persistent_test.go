package claude

import (
	"encoding/json"
	"testing"
)

func TestNewPersistentProcess_InitialState(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	status := process.Status()
	if status.Status != ProcessStatusIdle {
		t.Errorf("expected initial status %q, got %q", ProcessStatusIdle, status.Status)
	}
	if status.SessionID != "" {
		t.Errorf("expected empty session ID, got %q", status.SessionID)
	}
	if status.ErrorMessage != "" {
		t.Errorf("expected empty error message, got %q", status.ErrorMessage)
	}
	if status.MessageCount != 0 {
		t.Errorf("expected 0 message count, got %d", status.MessageCount)
	}
	if status.ToolCallCount != 0 {
		t.Errorf("expected 0 tool call count, got %d", status.ToolCallCount)
	}
	if status.LastMessage != "" {
		t.Errorf("expected empty last message, got %q", status.LastMessage)
	}
	if status.LastToolName != "" {
		t.Errorf("expected empty last tool name, got %q", status.LastToolName)
	}
}

func TestNewPersistentProcess_DonePreClosed(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// Done channel should be immediately readable (pre-closed).
	select {
	case <-process.Done():
		// Expected.
	default:
		t.Error("expected Done() to be immediately readable for new process")
	}
}

func TestPersistentProcess_MarshalStatus(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	data, err := process.MarshalStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var info StatusInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}

	if info.Status != ProcessStatusIdle {
		t.Errorf("expected status %q, got %q", ProcessStatusIdle, info.Status)
	}
}

func TestPersistentProcess_StopWhenNotRunning(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// Stop on an idle process should be a no-op.
	if err := process.Stop(); err != nil {
		t.Errorf("unexpected error stopping idle process: %v", err)
	}
}

func TestPersistentProcess_ImplementsPrompter(t *testing.T) {
	// Compile-time check that PersistentProcess implements Prompter.
	var _ Prompter = (*PersistentProcess)(nil)
}

func TestPersistentProcess_PersistentArgs(t *testing.T) {
	opts := Options{
		Model:              "claude-sonnet-4-20250514",
		SystemPrompt:       "Be helpful.",
		AppendSystemPrompt: "Be concise.",
		AllowedTools:       []string{"read", "write"},
		DisallowedTools:    []string{"exec"},
		Tools:              []string{"Bash", "Edit"},
		MaxTurns:           5,
		MCPConfigPath:      "/etc/mcp.json",
		StrictMCPConfig:    true,
		PermissionMode:     "bypassPermissions",
		MaxBudgetUSD:       10.50,
		Effort:             "high",
		FallbackModel:      "sonnet",
		JSONSchema:         `{"type":"object"}`,
		SettingsFile:       "/etc/settings.json",
		SettingSources:     "user",
		PluginDirs:         []string{"/plugins/a"},
		Agents: map[string]AgentConfig{
			"reviewer": {Description: "Reviews", Prompt: "Review"},
		},
		ActiveAgent:            "reviewer",
		IncludePartialMessages: true,
		NoSessionPersistence:   true,
	}

	args := opts.PersistentArgs()

	// Persistent mode uses --input-format stream-json with --replay-user-messages.
	assertContainsSequence(t, args, "--input-format", "stream-json")
	assertContainsSequence(t, args, "--output-format", "stream-json")
	assertContains(t, args, "--replay-user-messages")
	assertContains(t, args, "--print")
	assertContains(t, args, "--verbose")

	// Standard options should be present.
	assertContainsSequence(t, args, "--model", "claude-sonnet-4-20250514")
	assertContainsSequence(t, args, "--system-prompt", "Be helpful.")
	assertContainsSequence(t, args, "--append-system-prompt", "Be concise.")
	assertContainsSequence(t, args, "--max-turns", "5")
	assertContainsSequence(t, args, "--permission-mode", "bypassPermissions")
	assertContains(t, args, "--dangerously-skip-permissions")
	assertContainsSequence(t, args, "--mcp-config", "/etc/mcp.json")
	assertContains(t, args, "--strict-mcp-config")
	assertContainsSequence(t, args, "--allowedTools", "read,write")
	assertContainsSequence(t, args, "--disallowedTools", "exec")
	assertContainsSequence(t, args, "--tools", "Bash,Edit")
	assertContainsSequence(t, args, "--max-budget-usd", "10.50")
	assertContainsSequence(t, args, "--effort", "high")
	assertContainsSequence(t, args, "--fallback-model", "sonnet")
	assertContainsSequence(t, args, "--json-schema", `{"type":"object"}`)
	assertContainsSequence(t, args, "--settings", "/etc/settings.json")
	assertContainsSequence(t, args, "--setting-sources", "user")
	assertContainsSequence(t, args, "--agent", "reviewer")
	assertContains(t, args, "--agents")
	assertContains(t, args, "--include-partial-messages")
	assertContains(t, args, "--no-session-persistence")
	assertContainsSequence(t, args, "--plugin-dir", "/plugins/a")

	// Session management flags should NOT be in persistent args
	// (they are per-subprocess and persistent mode maintains one subprocess).
	assertNotContains(t, args, "--session-id")
	assertNotContains(t, args, "--resume")
	assertNotContains(t, args, "--continue")
	assertNotContains(t, args, "--fork-session")
}

func TestPersistentProcess_PersistentArgs_Minimal(t *testing.T) {
	args := DefaultOptions().PersistentArgs()

	assertContainsSequence(t, args, "--input-format", "stream-json")
	assertContainsSequence(t, args, "--output-format", "stream-json")
	assertContains(t, args, "--replay-user-messages")
	assertContains(t, args, "--print")
	assertContains(t, args, "--verbose")
	assertContains(t, args, "--permission-mode")
	assertContains(t, args, "bypassPermissions")
	assertContains(t, args, "--dangerously-skip-permissions")
	assertContains(t, args, "--no-session-persistence")

	assertNotContains(t, args, "--model")
	assertNotContains(t, args, "--max-budget-usd")
	assertNotContains(t, args, "--effort")
}

func TestStdinMessage_JSON(t *testing.T) {
	msg := stdinMessage{
		Type: "user",
		Message: stdinMessageContent{
			Role:    "user",
			Content: "hello world",
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"type":"user","message":{"role":"user","content":"hello world"}}`
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}
}

func TestRunOptions_IgnoredFields(t *testing.T) {
	t.Run("nil returns empty", func(t *testing.T) {
		var ro *RunOptions
		if fields := ro.ignoredFields(); len(fields) != 0 {
			t.Errorf("expected empty, got %v", fields)
		}
	})

	t.Run("zero value returns empty", func(t *testing.T) {
		ro := &RunOptions{}
		if fields := ro.ignoredFields(); len(fields) != 0 {
			t.Errorf("expected empty, got %v", fields)
		}
	})

	t.Run("non-zero fields listed", func(t *testing.T) {
		ro := &RunOptions{
			SessionID:    "sess-1",
			ActiveAgent:  "reviewer",
			MaxBudgetUSD: 5.0,
		}
		fields := ro.ignoredFields()
		if len(fields) != 3 {
			t.Errorf("expected 3 fields, got %d: %v", len(fields), fields)
		}
	})
}

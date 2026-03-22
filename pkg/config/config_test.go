package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_FromYAMLFile(t *testing.T) {
	// Clear env vars that would override YAML values in this test environment.
	for _, key := range []string{
		"CLAUDE_MODEL", "CLAUDE_SYSTEM_PROMPT", "CLAUDE_APPEND_SYSTEM_PROMPT",
		"CLAUDE_MAX_TURNS", "CLAUDE_PERMISSION_MODE", "CLAUDE_MCP_CONFIG",
		"CLAUDE_STRICT_MCP_CONFIG", "CLAUDE_WORKSPACE", "CLAUDE_MAX_BUDGET_USD",
		"CLAUDE_EFFORT", "CLAUDE_FALLBACK_MODEL", "CLAUDE_JSON_SCHEMA",
		"CLAUDE_SETTINGS_FILE", "CLAUDE_SETTING_SOURCES", "CLAUDE_TOOLS",
		"CLAUDE_ALLOWED_TOOLS", "CLAUDE_DISALLOWED_TOOLS", "CLAUDE_PLUGIN_DIRS",
		"CLAUDE_ADD_DIRS", "CLAUDE_AGENTS", "CLAUDE_ACTIVE_AGENT",
		"CLAUDE_INCLUDE_PARTIAL_MESSAGES", "CLAUDE_NO_SESSION_PERSISTENCE",
		"CLAUDE_PERSISTENT_MODE", "PORT", "KLAUS_OWNER_SUBJECT",
		"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "DEX_ISSUER_URL",
		"DEX_CLIENT_ID", "DEX_CLIENT_SECRET", "DEX_CONNECTOR_ID", "DEX_CA_FILE",
		"OAUTH_ENCRYPTION_KEY", "TLS_CERT_FILE", "TLS_KEY_FILE",
	} {
		t.Setenv(key, "")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `
claude:
  model: "claude-sonnet-4-20250514"
  systemPrompt: "You are helpful."
  appendSystemPrompt: "Be concise."
  maxTurns: 5
  permissionMode: "acceptEdits"
  mcpConfigPath: "/etc/mcp.json"
  strictMcpConfig: true
  workspace: "/work"
  maxBudgetUSD: 10.5
  effort: "high"
  fallbackModel: "sonnet"
  jsonSchema: '{"type":"object"}'
  settingsFile: "/etc/settings.json"
  settingSources: "user,project"
  tools:
    - "Bash"
    - "Edit"
  allowedTools:
    - "Read"
    - "Write"
  disallowedTools:
    - "exec"
  pluginDirs:
    - "/plugins/a"
  addDirs:
    - "/skills/a"
  agents: '{"reviewer":{"description":"Reviews code","prompt":"You review"}}'
  activeAgent: "reviewer"
  noSessionPersistence: true
  persistentMode: true
server:
  port: "9090"
  ownerSubject: "user@example.com"
oauth:
  enabled: true
  baseURL: "https://klaus.example.com"
  provider: "dex"
  google:
    clientID: "goog-id"
    clientSecret: "goog-secret"
  dex:
    issuerURL: "https://dex.example.com"
    clientID: "dex-id"
    clientSecret: "dex-secret"
    connectorID: "github"
    caFile: "/etc/ca.pem"
  security:
    encryptionKey: ""
    registrationToken: "reg-token"
    allowPublicRegistration: false
    maxClientsPerIP: 20
    enableCIMD: false
  tls:
    certFile: "/etc/tls/cert.pem"
    keyFile: "/etc/tls/key.pem"
  disableStreaming: true
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Claude settings.
	assertEqual(t, "claude.model", "claude-sonnet-4-20250514", cfg.Claude.Model)
	assertEqual(t, "claude.systemPrompt", "You are helpful.", cfg.Claude.SystemPrompt)
	assertEqual(t, "claude.appendSystemPrompt", "Be concise.", cfg.Claude.AppendSystemPrompt)
	assertEqualInt(t, "claude.maxTurns", 5, cfg.Claude.MaxTurns)
	assertEqual(t, "claude.permissionMode", "acceptEdits", cfg.Claude.PermissionMode)
	assertEqual(t, "claude.mcpConfigPath", "/etc/mcp.json", cfg.Claude.MCPConfigPath)
	assertEqualBool(t, "claude.strictMcpConfig", true, cfg.Claude.StrictMCPConfig)
	assertEqual(t, "claude.workspace", "/work", cfg.Claude.Workspace)
	assertEqualFloat(t, "claude.maxBudgetUSD", 10.5, cfg.Claude.MaxBudgetUSD)
	assertEqual(t, "claude.effort", "high", cfg.Claude.Effort)
	assertEqual(t, "claude.fallbackModel", "sonnet", cfg.Claude.FallbackModel)
	assertEqualBool(t, "claude.noSessionPersistence", true, cfg.Claude.NoSessionPersistence)
	assertEqualBool(t, "claude.persistentMode", true, cfg.Claude.PersistentMode)
	assertEqual(t, "claude.activeAgent", "reviewer", cfg.Claude.ActiveAgent)
	assertEqualSlice(t, "claude.tools", []string{"Bash", "Edit"}, cfg.Claude.Tools)
	assertEqualSlice(t, "claude.allowedTools", []string{"Read", "Write"}, cfg.Claude.AllowedTools)
	assertEqualSlice(t, "claude.disallowedTools", []string{"exec"}, cfg.Claude.DisallowedTools)
	assertEqualSlice(t, "claude.pluginDirs", []string{"/plugins/a"}, cfg.Claude.PluginDirs)
	assertEqualSlice(t, "claude.addDirs", []string{"/skills/a"}, cfg.Claude.AddDirs)

	// Server settings.
	assertEqual(t, "server.port", "9090", cfg.Server.Port)
	assertEqual(t, "server.ownerSubject", "user@example.com", cfg.Server.OwnerSubject)

	// OAuth settings.
	assertEqualBool(t, "oauth.enabled", true, cfg.OAuth.Enabled)
	assertEqual(t, "oauth.baseURL", "https://klaus.example.com", cfg.OAuth.BaseURL)
	assertEqual(t, "oauth.provider", "dex", cfg.OAuth.Provider)
	assertEqual(t, "oauth.google.clientID", "goog-id", cfg.OAuth.Google.ClientID)
	assertEqual(t, "oauth.dex.issuerURL", "https://dex.example.com", cfg.OAuth.Dex.IssuerURL)
	assertEqual(t, "oauth.dex.clientID", "dex-id", cfg.OAuth.Dex.ClientID)
	assertEqual(t, "oauth.dex.connectorID", "github", cfg.OAuth.Dex.ConnectorID)
	assertEqual(t, "oauth.security.registrationToken", "reg-token", cfg.OAuth.Security.RegistrationToken)
	assertEqualInt(t, "oauth.security.maxClientsPerIP", 20, cfg.OAuth.Security.MaxClientsPerIP)
	assertEqualBool(t, "oauth.tls.certFile", true, cfg.OAuth.TLS.CertFile != "")
	assertEqualBool(t, "oauth.disableStreaming", true, cfg.OAuth.DisableStreaming)
}

func TestLoad_FileNotFound_EnvOnly(t *testing.T) {
	t.Setenv("CLAUDE_MODEL", "opus")
	t.Setenv("PORT", "3000")
	t.Setenv("KLAUS_OWNER_SUBJECT", "admin@example.com")

	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("Load with missing file: %v", err)
	}

	assertEqual(t, "claude.model", "opus", cfg.Claude.Model)
	assertEqual(t, "server.port", "3000", cfg.Server.Port)
	assertEqual(t, "server.ownerSubject", "admin@example.com", cfg.Server.OwnerSubject)
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `
claude:
  model: "from-yaml"
  maxTurns: 3
server:
  port: "9090"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Env vars override YAML values.
	t.Setenv("CLAUDE_MODEL", "from-env")
	t.Setenv("CLAUDE_MAX_TURNS", "10")
	t.Setenv("PORT", "7777")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	assertEqual(t, "claude.model", "from-env", cfg.Claude.Model)
	assertEqualInt(t, "claude.maxTurns", 10, cfg.Claude.MaxTurns)
	assertEqual(t, "server.port", "7777", cfg.Server.Port)
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")

	if err := os.WriteFile(path, []byte("{{not: valid: yaml:"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := Config{
		Claude: ClaudeConfig{
			PermissionMode: "bypassPermissions",
			Effort:         "high",
			MaxTurns:       5,
			MaxBudgetUSD:   10.0,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidate_EmptyOptionalFields(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected empty config to be valid, got: %v", err)
	}
}

func TestValidate_InvalidPermissionMode(t *testing.T) {
	cfg := Config{Claude: ClaudeConfig{PermissionMode: "invalid"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid permission mode")
	}
}

func TestValidate_InvalidEffort(t *testing.T) {
	cfg := Config{Claude: ClaudeConfig{Effort: "extreme"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid effort")
	}
}

func TestValidate_NegativeMaxTurns(t *testing.T) {
	cfg := Config{Claude: ClaudeConfig{MaxTurns: -1}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative maxTurns")
	}
}

func TestValidate_NegativeMaxBudget(t *testing.T) {
	cfg := Config{Claude: ClaudeConfig{MaxBudgetUSD: -5.0}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative maxBudgetUSD")
	}
}

func TestValidate_InvalidEncryptionKey(t *testing.T) {
	cfg := Config{
		OAuth: OAuthFileConfig{
			Security: SecurityFileConfig{
				EncryptionKey: "not-base64!!!",
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid encryption key")
	}
}

func TestValidate_EncryptionKeyWrongLength(t *testing.T) {
	// 16 bytes instead of 32.
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	cfg := Config{
		OAuth: OAuthFileConfig{
			Security: SecurityFileConfig{
				EncryptionKey: short,
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for short encryption key")
	}
}

func TestValidate_ValidEncryptionKey(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := Config{
		OAuth: OAuthFileConfig{
			Security: SecurityFileConfig{
				EncryptionKey: key,
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid encryption key, got: %v", err)
	}
}

func TestDecodeEncryptionKey(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)

	cfg := Config{
		OAuth: OAuthFileConfig{
			Security: SecurityFileConfig{
				EncryptionKey: encoded,
			},
		},
	}

	key, err := cfg.DecodeEncryptionKey()
	if err != nil {
		t.Fatalf("DecodeEncryptionKey: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(key))
	}
}

func TestDecodeEncryptionKey_Empty(t *testing.T) {
	cfg := Config{}
	key, err := cfg.DecodeEncryptionKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != nil {
		t.Errorf("expected nil key, got %v", key)
	}
}

func TestEnableCIMD_Default(t *testing.T) {
	cfg := Config{}
	if !cfg.EnableCIMD() {
		t.Error("expected EnableCIMD to default to true")
	}
}

func TestEnableCIMD_ExplicitFalse(t *testing.T) {
	f := false
	cfg := Config{OAuth: OAuthFileConfig{Security: SecurityFileConfig{EnableCIMD: &f}}}
	if cfg.EnableCIMD() {
		t.Error("expected EnableCIMD to be false when explicitly set")
	}
}

func TestEnableCIMD_ExplicitTrue(t *testing.T) {
	tr := true
	cfg := Config{OAuth: OAuthFileConfig{Security: SecurityFileConfig{EnableCIMD: &tr}}}
	if !cfg.EnableCIMD() {
		t.Error("expected EnableCIMD to be true when explicitly set")
	}
}

func TestEffectivePort(t *testing.T) {
	tests := []struct {
		name     string
		flagPort string
		cfgPort  string
		want     string
	}{
		{"flag overrides all", "3000", "9090", "3000"},
		{"config port used", "", "9090", "9090"},
		{"default 8080", "", "", "8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Server: ServerConfig{Port: tt.cfgPort}}
			got := cfg.EffectivePort(tt.flagPort)
			if got != tt.want {
				t.Errorf("EffectivePort(%q) = %q, want %q", tt.flagPort, got, tt.want)
			}
		})
	}
}

func TestEnvOverride_CSVFields(t *testing.T) {
	t.Setenv("CLAUDE_TOOLS", "Bash,Edit,Read")
	t.Setenv("CLAUDE_ALLOWED_TOOLS", "Read,Write")
	t.Setenv("CLAUDE_PLUGIN_DIRS", "/a,/b")

	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	assertEqualSlice(t, "tools", []string{"Bash", "Edit", "Read"}, cfg.Claude.Tools)
	assertEqualSlice(t, "allowedTools", []string{"Read", "Write"}, cfg.Claude.AllowedTools)
	assertEqualSlice(t, "pluginDirs", []string{"/a", "/b"}, cfg.Claude.PluginDirs)
}

func TestEnvOverride_BoolFields(t *testing.T) {
	t.Setenv("CLAUDE_STRICT_MCP_CONFIG", "true")
	t.Setenv("CLAUDE_NO_SESSION_PERSISTENCE", "yes")
	t.Setenv("CLAUDE_PERSISTENT_MODE", "TRUE")

	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	assertEqualBool(t, "strictMcpConfig", true, cfg.Claude.StrictMCPConfig)
	assertEqualBool(t, "noSessionPersistence", true, cfg.Claude.NoSessionPersistence)
	assertEqualBool(t, "persistentMode", true, cfg.Claude.PersistentMode)
}

func TestEnvOverride_OAuthFields(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "env-goog-id")
	t.Setenv("DEX_ISSUER_URL", "https://dex.env.com")
	t.Setenv("OAUTH_ENCRYPTION_KEY", "env-key")
	t.Setenv("TLS_CERT_FILE", "/env/cert.pem")
	t.Setenv("TLS_KEY_FILE", "/env/key.pem")

	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	assertEqual(t, "google.clientID", "env-goog-id", cfg.OAuth.Google.ClientID)
	assertEqual(t, "dex.issuerURL", "https://dex.env.com", cfg.OAuth.Dex.IssuerURL)
	assertEqual(t, "security.encryptionKey", "env-key", cfg.OAuth.Security.EncryptionKey)
	assertEqual(t, "tls.certFile", "/env/cert.pem", cfg.OAuth.TLS.CertFile)
	assertEqual(t, "tls.keyFile", "/env/key.pem", cfg.OAuth.TLS.KeyFile)
}

func TestParseBool(t *testing.T) {
	trueCases := []string{"true", "TRUE", "True", "1", "yes", "YES", " true ", " 1 "}
	for _, tc := range trueCases {
		if !parseBool(tc) {
			t.Errorf("expected parseBool(%q) to be true", tc)
		}
	}

	falseCases := []string{"false", "FALSE", "0", "no", "", "invalid", " false "}
	for _, tc := range falseCases {
		if parseBool(tc) {
			t.Errorf("expected parseBool(%q) to be false", tc)
		}
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := Config{
		Claude: ClaudeConfig{
			PermissionMode: "invalid",
			Effort:         "extreme",
			MaxTurns:       -1,
			MaxBudgetUSD:   -5.0,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation errors")
	}
	// All four errors should be present.
	errStr := err.Error()
	for _, want := range []string{"permissionMode", "effort", "maxTurns", "maxBudgetUSD"} {
		if !strings.Contains(errStr, want) {
			t.Errorf("expected error to mention %q, got: %s", want, errStr)
		}
	}
}

// --- test helpers ---

func assertEqual(t *testing.T, field, want, got string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %q, got %q", field, want, got)
	}
}

func assertEqualInt(t *testing.T, field string, want, got int) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %d, got %d", field, want, got)
	}
}

func assertEqualFloat(t *testing.T, field string, want, got float64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %f, got %f", field, want, got)
	}
}

func assertEqualBool(t *testing.T, field string, want, got bool) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %v, got %v", field, want, got)
	}
}

func assertEqualSlice(t *testing.T, field string, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("%s: want %v, got %v", field, want, got)
		return
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("%s[%d]: want %q, got %q", field, i, want[i], got[i])
		}
	}
}

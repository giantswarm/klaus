// Package config provides structured configuration for the klaus server.
//
// Configuration is loaded from a YAML file (default /etc/klaus/config.yaml)
// with environment variable overrides for backward compatibility with existing
// deployments that use env vars (e.g. klausctl-created instances).
//
// The config struct clearly separates settings consumed by klaus itself from
// settings passed through to the Claude Code subprocess.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/klaus/pkg/claude"
)

const (
	// DefaultConfigPath is the well-known location for the klaus config file.
	DefaultConfigPath = "/etc/klaus/config.yaml"

	// maxConfigFileSize is the maximum allowed size for a config file (1 MiB).
	maxConfigFileSize = 1 << 20
)

// Config is the top-level configuration for the klaus server.
type Config struct {
	// Claude holds settings passed to the Claude Code subprocess.
	Claude ClaudeConfig `yaml:"claude"`

	// Server holds settings consumed by the klaus HTTP server itself.
	Server ServerConfig `yaml:"server"`

	// OAuth holds OAuth 2.1 authentication settings.
	OAuth OAuthFileConfig `yaml:"oauth"`
}

// ClaudeConfig holds settings that are forwarded to the Claude Code CLI.
// These map directly to claude CLI flags and are not consumed by klaus itself.
type ClaudeConfig struct {
	// Model selects the Claude model (e.g. "claude-sonnet-4-20250514", "sonnet", "opus").
	Model string `yaml:"model"`
	// SystemPrompt overrides the default system prompt entirely.
	SystemPrompt string `yaml:"systemPrompt"`
	// AppendSystemPrompt is appended to the default system prompt.
	AppendSystemPrompt string `yaml:"appendSystemPrompt"`
	// MaxTurns limits agentic turns per prompt; 0 means unlimited.
	MaxTurns int `yaml:"maxTurns"`
	// PermissionMode controls how Claude handles tool permissions.
	// Valid values: "default", "acceptEdits", "bypassPermissions", "dontAsk", "plan", "delegate".
	PermissionMode string `yaml:"permissionMode"`
	// MCPConfigPath is a path to an MCP servers configuration file.
	MCPConfigPath string `yaml:"mcpConfigPath"`
	// StrictMCPConfig when true only uses MCP servers from MCPConfigPath.
	StrictMCPConfig bool `yaml:"strictMcpConfig"`
	// Workspace is the working directory for the Claude subprocess.
	Workspace string `yaml:"workspace"`
	// MaxBudgetUSD caps the maximum dollar spend per invocation; 0 means no limit.
	MaxBudgetUSD float64 `yaml:"maxBudgetUSD"`
	// Effort controls the effort level: "low", "medium", "high"; empty means default.
	Effort string `yaml:"effort"`
	// FallbackModel specifies a model to use when the primary is overloaded.
	FallbackModel string `yaml:"fallbackModel"`
	// JSONSchema constrains the output to conform to a JSON Schema.
	JSONSchema string `yaml:"jsonSchema"`
	// SettingsFile is a path to a settings JSON file or inline JSON string.
	SettingsFile string `yaml:"settingsFile"`
	// SettingSources controls which setting sources are loaded (comma-separated: "user,project,local").
	SettingSources string `yaml:"settingSources"`
	// Tools controls the base set of built-in tools available.
	Tools []string `yaml:"tools"`
	// AllowedTools restricts tool access; empty means all allowed.
	AllowedTools []string `yaml:"allowedTools"`
	// DisallowedTools explicitly blocks specific tools.
	DisallowedTools []string `yaml:"disallowedTools"`
	// PluginDirs are directories to load plugins from.
	PluginDirs []string `yaml:"pluginDirs"`
	// AddDirs are directories to search for .claude/ subdirectories.
	AddDirs []string `yaml:"addDirs"`
	// Agents defines custom subagents as a raw JSON string.
	// Parsed into map[string]AgentConfig by the caller.
	Agents string `yaml:"agents"`
	// ActiveAgent selects which agent runs as the top-level agent.
	ActiveAgent string `yaml:"activeAgent"`
	// NoSessionPersistence disables saving sessions to disk.
	NoSessionPersistence bool `yaml:"noSessionPersistence"`
	// PersistentMode uses persistent subprocess mode (bidirectional stream-json).
	PersistentMode bool `yaml:"persistentMode"`
}

// ServerConfig holds settings consumed by the klaus server process itself
// (not forwarded to the Claude subprocess).
type ServerConfig struct {
	// Port is the HTTP listen port (default "8080").
	Port string `yaml:"port"`
	// OwnerSubject restricts /mcp to the configured identity.
	OwnerSubject string `yaml:"ownerSubject"`
}

// OAuthFileConfig mirrors the OAuth flags for YAML configuration.
type OAuthFileConfig struct {
	// Enabled turns on OAuth 2.1 authentication for the MCP endpoint.
	Enabled bool `yaml:"enabled"`
	// BaseURL is the server base URL (e.g., https://klaus.example.com).
	BaseURL string `yaml:"baseURL"`
	// Provider specifies the OAuth provider: "dex" or "google".
	Provider string `yaml:"provider"`

	// Google holds Google-specific OAuth credentials.
	Google GoogleConfig `yaml:"google"`
	// Dex holds Dex-specific OIDC credentials.
	Dex DexConfig `yaml:"dex"`

	// Security holds token encryption and registration settings.
	Security SecurityFileConfig `yaml:"security"`
	// TLS holds optional TLS certificate paths for HTTPS.
	TLS TLSFileConfig `yaml:"tls"`
	// DisableStreaming disables streaming for streamable-http transport.
	DisableStreaming bool `yaml:"disableStreaming"`
}

// GoogleConfig holds Google OAuth provider settings.
type GoogleConfig struct {
	ClientID     string `yaml:"clientID"`
	ClientSecret string `yaml:"clientSecret"`
}

// DexConfig holds Dex OIDC provider settings.
type DexConfig struct {
	IssuerURL    string `yaml:"issuerURL"`
	ClientID     string `yaml:"clientID"`
	ClientSecret string `yaml:"clientSecret"`
	ConnectorID  string `yaml:"connectorID"`
	CAFile       string `yaml:"caFile"`
}

// SecurityFileConfig holds OAuth security settings in YAML.
type SecurityFileConfig struct {
	// EncryptionKey is a base64-encoded AES-256 key for token encryption (32 bytes decoded).
	EncryptionKey                    string   `yaml:"encryptionKey"`
	RegistrationToken                string   `yaml:"registrationToken"`
	AllowPublicRegistration          bool     `yaml:"allowPublicRegistration"`
	AllowInsecureAuthWithoutState    bool     `yaml:"allowInsecureAuthWithoutState"`
	MaxClientsPerIP                  int      `yaml:"maxClientsPerIP"`
	EnableCIMD                       *bool    `yaml:"enableCIMD"`
	CIMDAllowPrivateIPs              bool     `yaml:"cimdAllowPrivateIPs"`
	TrustedPublicRegistrationSchemes []string `yaml:"trustedPublicRegistrationSchemes"`
	DisableStrictSchemeMatching      bool     `yaml:"disableStrictSchemeMatching"`
}

// TLSFileConfig holds TLS certificate paths.
type TLSFileConfig struct {
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

// Load reads configuration from a YAML file, then applies environment variable
// overrides for backward compatibility. If the file does not exist, a zero-value
// Config is used as the base (all settings come from env vars).
func Load(path string) (Config, error) {
	var cfg Config

	f, err := os.Open(path) //#nosec G304 -- operator-provided config path
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("reading config file %s: %w", path, err)
		}
		// File doesn't exist -- env-only mode (backward compat).
	} else {
		defer f.Close()
		limited := io.LimitReader(f, maxConfigFileSize+1)
		data, readErr := io.ReadAll(limited)
		if readErr != nil {
			return cfg, fmt.Errorf("reading config file %s: %w", path, readErr)
		}
		if len(data) > maxConfigFileSize {
			return cfg, fmt.Errorf("config file %s exceeds maximum size of %d bytes", path, maxConfigFileSize)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parsing config file %s: %w", path, err)
		}
	}

	// Apply environment variable overrides.
	applyEnvOverrides(&cfg)

	return cfg, nil
}

// applyEnvOverrides layers environment variables on top of the YAML config.
// Env vars only override when set to a non-empty value, preserving YAML values
// when the env var is absent.
func applyEnvOverrides(cfg *Config) {
	// Claude subprocess settings.
	envOverrideString(&cfg.Claude.Model, "CLAUDE_MODEL")
	envOverrideString(&cfg.Claude.SystemPrompt, "CLAUDE_SYSTEM_PROMPT")
	envOverrideString(&cfg.Claude.AppendSystemPrompt, "CLAUDE_APPEND_SYSTEM_PROMPT")
	envOverrideInt(&cfg.Claude.MaxTurns, "CLAUDE_MAX_TURNS")
	envOverrideString(&cfg.Claude.PermissionMode, "CLAUDE_PERMISSION_MODE")
	envOverrideString(&cfg.Claude.MCPConfigPath, "CLAUDE_MCP_CONFIG")
	envOverrideBool(&cfg.Claude.StrictMCPConfig, "CLAUDE_STRICT_MCP_CONFIG")
	envOverrideString(&cfg.Claude.Workspace, "CLAUDE_WORKSPACE")
	envOverrideFloat64(&cfg.Claude.MaxBudgetUSD, "CLAUDE_MAX_BUDGET_USD")
	envOverrideString(&cfg.Claude.Effort, "CLAUDE_EFFORT")
	envOverrideString(&cfg.Claude.FallbackModel, "CLAUDE_FALLBACK_MODEL")
	envOverrideString(&cfg.Claude.JSONSchema, "CLAUDE_JSON_SCHEMA")
	envOverrideString(&cfg.Claude.SettingsFile, "CLAUDE_SETTINGS_FILE")
	envOverrideString(&cfg.Claude.SettingSources, "CLAUDE_SETTING_SOURCES")
	envOverrideCSV(&cfg.Claude.Tools, "CLAUDE_TOOLS")
	envOverrideCSV(&cfg.Claude.AllowedTools, "CLAUDE_ALLOWED_TOOLS")
	envOverrideCSV(&cfg.Claude.DisallowedTools, "CLAUDE_DISALLOWED_TOOLS")
	envOverrideCSV(&cfg.Claude.PluginDirs, "CLAUDE_PLUGIN_DIRS")
	envOverrideCSV(&cfg.Claude.AddDirs, "CLAUDE_ADD_DIRS")
	envOverrideString(&cfg.Claude.Agents, "CLAUDE_AGENTS")
	envOverrideString(&cfg.Claude.ActiveAgent, "CLAUDE_ACTIVE_AGENT")
	envOverrideBool(&cfg.Claude.NoSessionPersistence, "CLAUDE_NO_SESSION_PERSISTENCE")
	envOverrideBool(&cfg.Claude.PersistentMode, "CLAUDE_PERSISTENT_MODE")

	// Server settings.
	envOverrideString(&cfg.Server.Port, "PORT")
	envOverrideString(&cfg.Server.OwnerSubject, "KLAUS_OWNER_SUBJECT")

	// OAuth settings.
	envOverrideString(&cfg.OAuth.Google.ClientID, "GOOGLE_CLIENT_ID")
	envOverrideString(&cfg.OAuth.Google.ClientSecret, "GOOGLE_CLIENT_SECRET")
	envOverrideString(&cfg.OAuth.Dex.IssuerURL, "DEX_ISSUER_URL")
	envOverrideString(&cfg.OAuth.Dex.ClientID, "DEX_CLIENT_ID")
	envOverrideString(&cfg.OAuth.Dex.ClientSecret, "DEX_CLIENT_SECRET")
	envOverrideString(&cfg.OAuth.Dex.ConnectorID, "DEX_CONNECTOR_ID")
	envOverrideString(&cfg.OAuth.Dex.CAFile, "DEX_CA_FILE")
	envOverrideString(&cfg.OAuth.Security.EncryptionKey, "OAUTH_ENCRYPTION_KEY")
	envOverrideString(&cfg.OAuth.TLS.CertFile, "TLS_CERT_FILE")
	envOverrideString(&cfg.OAuth.TLS.KeyFile, "TLS_KEY_FILE")
}

// Validate checks that the loaded configuration is internally consistent.
// It returns all validation errors joined together.
func (c *Config) Validate() error {
	var errs []error

	// Reuse validators from the claude package to avoid duplicate valid-value lists.
	if c.Claude.PermissionMode != "" {
		if err := claude.ValidatePermissionMode(c.Claude.PermissionMode); err != nil {
			errs = append(errs, fmt.Errorf("claude.permissionMode: %w", err))
		}
	}
	if err := claude.ValidateEffort(c.Claude.Effort); err != nil {
		errs = append(errs, fmt.Errorf("claude.effort: %w", err))
	}
	if c.Claude.MaxTurns < 0 {
		errs = append(errs, fmt.Errorf("claude.maxTurns must be >= 0, got %d", c.Claude.MaxTurns))
	}
	if c.Claude.MaxBudgetUSD < 0 {
		errs = append(errs, fmt.Errorf("claude.maxBudgetUSD must be >= 0, got %f", c.Claude.MaxBudgetUSD))
	}

	// Validate encryption key format if set.
	if c.OAuth.Security.EncryptionKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(c.OAuth.Security.EncryptionKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("oauth.security.encryptionKey must be base64 encoded: %w", err))
		} else if len(decoded) != 32 {
			errs = append(errs, fmt.Errorf("oauth.security.encryptionKey must decode to exactly 32 bytes, got %d", len(decoded)))
		}
	}

	return errors.Join(errs...)
}

// DecodeEncryptionKey decodes the base64-encoded encryption key.
// Returns nil if no key is configured. Callers should call Validate first.
func (c *Config) DecodeEncryptionKey() ([]byte, error) {
	if c.OAuth.Security.EncryptionKey == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(c.OAuth.Security.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decoding encryption key: %w", err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("encryption key must be exactly 32 bytes, got %d", len(decoded))
	}
	return decoded, nil
}

// EnableCIMD returns the effective CIMD setting. Defaults to true when not
// explicitly set in the config file (matching the CLI flag default).
func (c *Config) EnableCIMD() bool {
	if c.OAuth.Security.EnableCIMD == nil {
		return true
	}
	return *c.OAuth.Security.EnableCIMD
}

// EffectivePort returns the configured port, defaulting to "8080".
func (c *Config) EffectivePort(flagPort string) string {
	if flagPort != "" {
		return flagPort
	}
	if c.Server.Port != "" {
		return c.Server.Port
	}
	return "8080"
}

// --- env override helpers ---

func envOverrideString(target *string, key string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}

func envOverrideBool(target *bool, key string) {
	if v := os.Getenv(key); v != "" {
		*target = parseBool(v)
	}
}

func envOverrideInt(target *int, key string) {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("WARNING: ignoring invalid integer for %s=%q: %v", key, v, err)
			return
		}
		*target = n
	}
}

func envOverrideFloat64(target *float64, key string) {
	if v := os.Getenv(key); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			log.Printf("WARNING: ignoring invalid float for %s=%q: %v", key, v, err)
			return
		}
		*target = f
	}
}

func envOverrideCSV(target *[]string, key string) {
	if v := os.Getenv(key); v != "" {
		*target = strings.Split(v, ",")
	}
}

// parseBool returns true for "true", "1", "yes" (case-insensitive).
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

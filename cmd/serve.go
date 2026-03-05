package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/config"
	"github.com/giantswarm/klaus/pkg/server"
)

const (
	// defaultSOULPath is the well-known location where klausctl mounts the
	// personality SOUL.md file (read-only). If present, its content is appended
	// to the system prompt so that personality identity definitions take effect.
	defaultSOULPath = "/etc/klaus/SOUL.md"

	// maxSOULFileSize is the maximum allowed size for a SOUL.md file (64 KiB).
	// Personality files are short markdown documents; anything larger is likely
	// a misconfiguration and could cause issues with CLI argument limits.
	maxSOULFileSize = 64 * 1024
)

// newServeCmd creates the Cobra command for starting the klaus server.
func newServeCmd() *cobra.Command {
	var (
		port       string
		configPath string

		// OAuth options (CLI flags override config file values).
		enableOAuth                      bool
		oauthBaseURL                     string
		oauthProvider                    string
		googleClientID                   string
		googleClientSecret               string
		dexIssuerURL                     string
		dexClientID                      string
		dexClientSecret                  string
		dexConnectorID                   string
		dexCAFile                        string
		disableStreaming                 bool
		registrationToken                string
		allowPublicRegistration          bool
		allowInsecureAuthWithoutState    bool
		maxClientsPerIP                  int
		oauthEncryptionKey               string
		enableCIMD                       bool
		cimdAllowPrivateIPs              bool
		trustedPublicRegistrationSchemes []string
		disableStrictSchemeMatching      bool
		tlsCertFile                      string
		tlsKeyFile                       string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the klaus server",
		Long: `Start the klaus HTTP server that wraps claude-code and exposes
MCP protocol endpoints for AI agent orchestration.

The server provides:
  - /mcp    -- Streamable HTTP MCP endpoint
  - /healthz -- Liveness probe
  - /readyz  -- Readiness probe
  - /status  -- JSON status endpoint

With OAuth enabled (--enable-oauth), the /mcp endpoint requires OAuth 2.1
Bearer token authentication. Additional endpoints are exposed:
  - /.well-known/oauth-authorization-server
  - /.well-known/oauth-protected-resource
  - /oauth/register, /oauth/authorize, /oauth/token, /oauth/callback

Configuration is loaded from a YAML file (default /etc/klaus/config.yaml)
with environment variable overrides for backward compatibility. See
pkg/config for the full Config struct and supported fields.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load structured config: YAML file -> env var overrides.
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// CLI flags override config file + env values for OAuth settings.
			// Only override when the flag was explicitly set by the user.
			// This must happen BEFORE validation so that flag overrides are
			// included in the validation pass.
			applyOAuthFlagOverrides(cmd, &cfg,
				enableOAuth, oauthBaseURL, oauthProvider,
				googleClientID, googleClientSecret,
				dexIssuerURL, dexClientID, dexClientSecret, dexConnectorID, dexCAFile,
				disableStreaming, registrationToken,
				allowPublicRegistration, allowInsecureAuthWithoutState,
				maxClientsPerIP, oauthEncryptionKey,
				enableCIMD, cimdAllowPrivateIPs,
				trustedPublicRegistrationSchemes, disableStrictSchemeMatching,
				tlsCertFile, tlsKeyFile,
			)

			// Validate after all overrides (YAML -> env -> flags) are applied.
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("config validation: %w", err)
			}

			// Build the OAuthConfig from the unified config struct.
			encryptionKey, err := cfg.DecodeEncryptionKey()
			if err != nil {
				return fmt.Errorf("OAuth encryption key must be base64 encoded (use: openssl rand -base64 32): %w", err)
			}

			oauthConfig := server.OAuthConfig{
				BaseURL:  cfg.OAuth.BaseURL,
				Provider: cfg.OAuth.Provider,
				Google: server.GoogleOAuthConfig{
					ClientID:     cfg.OAuth.Google.ClientID,
					ClientSecret: cfg.OAuth.Google.ClientSecret,
				},
				Dex: server.DexOAuthConfig{
					IssuerURL:    cfg.OAuth.Dex.IssuerURL,
					ClientID:     cfg.OAuth.Dex.ClientID,
					ClientSecret: cfg.OAuth.Dex.ClientSecret,
					ConnectorID:  cfg.OAuth.Dex.ConnectorID,
					CAFile:       cfg.OAuth.Dex.CAFile,
				},
				Security: server.SecurityConfig{
					EncryptionKey:                    encryptionKey,
					RegistrationAccessToken:          cfg.OAuth.Security.RegistrationToken,
					AllowPublicClientRegistration:    cfg.OAuth.Security.AllowPublicRegistration,
					AllowInsecureAuthWithoutState:    cfg.OAuth.Security.AllowInsecureAuthWithoutState,
					MaxClientsPerIP:                  cfg.OAuth.Security.MaxClientsPerIP,
					EnableCIMD:                       cfg.EnableCIMD(),
					CIMDAllowPrivateIPs:              cfg.OAuth.Security.CIMDAllowPrivateIPs,
					TrustedPublicRegistrationSchemes: cfg.OAuth.Security.TrustedPublicRegistrationSchemes,
					DisableStrictSchemeMatching:      cfg.OAuth.Security.DisableStrictSchemeMatching,
				},
				TLS: server.TLSConfig{
					CertFile: cfg.OAuth.TLS.CertFile,
					KeyFile:  cfg.OAuth.TLS.KeyFile,
				},
				DisableStreaming: cfg.OAuth.DisableStreaming,
			}

			return runServe(port, cfg, enableOAuth || cfg.OAuth.Enabled, oauthConfig)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", config.DefaultConfigPath, "Path to YAML config file")
	cmd.Flags().StringVar(&port, "port", "", "HTTP server port (overrides config file and PORT env var, default: 8080)")

	// OAuth flags (override config file values when explicitly set).
	cmd.Flags().BoolVar(&enableOAuth, "enable-oauth", false, "Enable OAuth 2.1 authentication for the MCP endpoint")
	cmd.Flags().StringVar(&oauthBaseURL, "oauth-base-url", "", "OAuth base URL (e.g., https://klaus.example.com)")
	cmd.Flags().StringVar(&oauthProvider, "oauth-provider", server.OAuthProviderDex, fmt.Sprintf("OAuth provider: %s or %s", server.OAuthProviderDex, server.OAuthProviderGoogle))
	cmd.Flags().StringVar(&googleClientID, "google-client-id", "", "Google OAuth Client ID (or GOOGLE_CLIENT_ID env)")
	cmd.Flags().StringVar(&googleClientSecret, "google-client-secret", "", "Google OAuth Client Secret (or GOOGLE_CLIENT_SECRET env)")
	cmd.Flags().StringVar(&dexIssuerURL, "dex-issuer-url", "", "Dex OIDC issuer URL (or DEX_ISSUER_URL env)")
	cmd.Flags().StringVar(&dexClientID, "dex-client-id", "", "Dex OAuth Client ID (or DEX_CLIENT_ID env)")
	cmd.Flags().StringVar(&dexClientSecret, "dex-client-secret", "", "Dex OAuth Client Secret (or DEX_CLIENT_SECRET env)")
	cmd.Flags().StringVar(&dexConnectorID, "dex-connector-id", "", "Dex connector ID to bypass connector selection (optional)")
	cmd.Flags().StringVar(&dexCAFile, "dex-ca-file", "", "CA certificate file for Dex TLS verification (optional)")
	cmd.Flags().BoolVar(&disableStreaming, "disable-streaming", false, "Disable streaming for streamable-http transport")
	cmd.Flags().StringVar(&registrationToken, "registration-token", "", "OAuth client registration access token")
	cmd.Flags().BoolVar(&allowPublicRegistration, "allow-public-registration", false, "Allow unauthenticated OAuth client registration (NOT RECOMMENDED for production)")
	cmd.Flags().BoolVar(&allowInsecureAuthWithoutState, "allow-insecure-auth-without-state", false, "Allow authorization requests without state parameter")
	cmd.Flags().IntVar(&maxClientsPerIP, "max-clients-per-ip", 10, "Maximum OAuth clients per IP address")
	cmd.Flags().StringVar(&oauthEncryptionKey, "oauth-encryption-key", "", "AES-256 encryption key for token encryption (base64, or OAUTH_ENCRYPTION_KEY env)")
	cmd.Flags().BoolVar(&enableCIMD, "enable-cimd", true, "Enable Client ID Metadata Documents (MCP 2025-11-25)")
	cmd.Flags().BoolVar(&cimdAllowPrivateIPs, "cimd-allow-private-ips", false, "Allow CIMD metadata URLs to resolve to private IPs")
	cmd.Flags().StringSliceVar(&trustedPublicRegistrationSchemes, "trusted-public-registration-schemes", nil, "URI schemes allowed for unauthenticated client registration (e.g., cursor,vscode)")
	cmd.Flags().BoolVar(&disableStrictSchemeMatching, "disable-strict-scheme-matching", false, "Allow mixed redirect URI schemes with trusted scheme registration")
	cmd.Flags().StringVar(&tlsCertFile, "tls-cert-file", "", "TLS certificate file for HTTPS (PEM format)")
	cmd.Flags().StringVar(&tlsKeyFile, "tls-key-file", "", "TLS private key file for HTTPS (PEM format)")

	return cmd
}

// runServe contains the main server logic, now driven by the structured Config.
func runServe(portFlag string, cfg config.Config, enableOAuth bool, oauthConfig server.OAuthConfig) error {
	// Build Claude options from config struct.
	opts := claude.DefaultOptions()

	if cfg.Claude.Model != "" {
		opts.Model = cfg.Claude.Model
	}
	if cfg.Claude.SystemPrompt != "" {
		opts.SystemPrompt = cfg.Claude.SystemPrompt
	}
	if cfg.Claude.AppendSystemPrompt != "" {
		opts.AppendSystemPrompt = cfg.Claude.AppendSystemPrompt
	}
	if cfg.Claude.MaxTurns > 0 {
		opts.MaxTurns = cfg.Claude.MaxTurns
	}
	if cfg.Claude.PermissionMode != "" {
		opts.PermissionMode = cfg.Claude.PermissionMode
	}
	if cfg.Claude.MCPConfigPath != "" {
		opts.MCPConfigPath = cfg.Claude.MCPConfigPath
	}
	if cfg.Claude.StrictMCPConfig {
		opts.StrictMCPConfig = true
	}
	if cfg.Claude.Workspace != "" {
		opts.WorkDir = cfg.Claude.Workspace
	}
	if cfg.Claude.MaxBudgetUSD > 0 {
		opts.MaxBudgetUSD = cfg.Claude.MaxBudgetUSD
	}
	if cfg.Claude.Effort != "" {
		opts.Effort = cfg.Claude.Effort
	}
	if cfg.Claude.FallbackModel != "" {
		opts.FallbackModel = cfg.Claude.FallbackModel
	}
	if cfg.Claude.JSONSchema != "" {
		opts.JSONSchema = cfg.Claude.JSONSchema
	}
	if cfg.Claude.SettingsFile != "" {
		opts.SettingsFile = cfg.Claude.SettingsFile
	}
	if cfg.Claude.SettingSources != "" {
		opts.SettingSources = cfg.Claude.SettingSources
	}
	if len(cfg.Claude.Tools) > 0 {
		opts.Tools = cfg.Claude.Tools
	}
	if len(cfg.Claude.AllowedTools) > 0 {
		opts.AllowedTools = cfg.Claude.AllowedTools
	}
	if len(cfg.Claude.DisallowedTools) > 0 {
		opts.DisallowedTools = cfg.Claude.DisallowedTools
	}
	if len(cfg.Claude.PluginDirs) > 0 {
		opts.PluginDirs = cfg.Claude.PluginDirs
	}
	if len(cfg.Claude.AddDirs) > 0 {
		opts.AddDirs = cfg.Claude.AddDirs
	}

	// Subagent definitions.
	if cfg.Claude.Agents != "" {
		var agents map[string]claude.AgentConfig
		if err := json.Unmarshal([]byte(cfg.Claude.Agents), &agents); err != nil {
			log.Printf("WARNING: failed to parse claude.agents: %v", err)
		} else {
			opts.Agents = agents
		}
	}
	if cfg.Claude.ActiveAgent != "" {
		opts.ActiveAgent = cfg.Claude.ActiveAgent
	}
	if cfg.Claude.IncludePartialMessages {
		opts.IncludePartialMessages = true
	}
	if cfg.Claude.NoSessionPersistence {
		opts.NoSessionPersistence = true
	}

	// Load personality SOUL.md if mounted by klausctl.
	if soul, err := loadSOULFile(defaultSOULPath); err != nil {
		log.Printf("WARNING: failed to load personality from %s: %v", defaultSOULPath, err)
	} else if soul != "" {
		if opts.AppendSystemPrompt != "" {
			opts.AppendSystemPrompt += "\n\n"
		}
		opts.AppendSystemPrompt += soul
		log.Printf("Loaded personality from %s (%d bytes)", defaultSOULPath, len(soul))
	}

	// Create the Claude process manager.
	// Note: permissionMode and effort are already validated by cfg.Validate()
	// using the canonical validators from the claude package.
	var process claude.Prompter
	if cfg.Claude.PersistentMode {
		log.Println("Starting in persistent mode (bidirectional stream-json)")
		process = claude.NewPersistentProcess(opts)
	} else {
		process = claude.NewProcess(opts)
	}

	// Owner-based access control.
	if cfg.Server.OwnerSubject != "" {
		log.Printf("Owner-based access control enabled (subject: %s)", cfg.Server.OwnerSubject)
	}

	// Determine listen port: flag > config > default.
	listenPort := cfg.EffectivePort(portFlag)

	// Wait for interrupt signal for graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	mode := server.ModeSingleShot
	if cfg.Claude.PersistentMode {
		mode = server.ModePersistent
	}

	srvCfg := server.Config{
		Port:         listenPort,
		Mode:         mode,
		OwnerSubject: cfg.Server.OwnerSubject,
	}

	if enableOAuth {
		return runWithOAuth(serverCtx, process, srvCfg, oauthConfig, quit)
	}
	return runWithoutOAuth(serverCtx, process, srvCfg, quit)
}

func runWithOAuth(serverCtx context.Context, process claude.Prompter, cfg server.Config, config server.OAuthConfig, quit chan os.Signal) error {
	if err := config.Validate(); err != nil {
		return err
	}

	// Warn about insecure configuration.
	if config.Security.AllowPublicClientRegistration {
		log.Println("WARNING: Public client registration is enabled - this allows unlimited client registration")
	}
	if config.Security.AllowInsecureAuthWithoutState {
		log.Println("WARNING: State parameter is optional - this weakens CSRF protection")
	}
	if len(config.Security.EncryptionKey) == 0 {
		log.Println("WARNING: OAuth encryption key not set - tokens will be stored unencrypted")
	}

	oauthSrv, err := server.NewOAuthServer(serverCtx, process, config, cfg.OwnerSubject)
	if err != nil {
		return fmt.Errorf("failed to create OAuth server: %w", err)
	}

	addr := ":" + cfg.Port
	return runServerLifecycle(
		func() error { return oauthSrv.Start(addr, cfg.Mode, config) },
		oauthSrv.Shutdown,
		process,
		quit,
	)
}

func runWithoutOAuth(serverCtx context.Context, process claude.Prompter, cfg server.Config, quit chan os.Signal) error {
	srv := server.NewServer(serverCtx, process, cfg)
	return runServerLifecycle(srv.Start, srv.Shutdown, process, quit)
}

// runServerLifecycle runs a server in a goroutine, waits for a shutdown signal
// or server error, stops the Claude process, and gracefully shuts down.
func runServerLifecycle(
	startFn func() error,
	shutdownFn func(context.Context) error,
	process claude.Prompter,
	quit <-chan os.Signal,
) error {
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		if err := startFn(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverDone <- err
		}
	}()

	select {
	case <-quit:
		log.Println("Shutdown signal received...")
	case err := <-serverDone:
		if err != nil {
			return fmt.Errorf("HTTP server error: %w", err)
		}
	}

	// Stop any running Claude process.
	if err := process.Stop(); err != nil {
		log.Printf("Error stopping Claude process: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), server.DefaultShutdownTimeout)
	defer cancel()

	if err := shutdownFn(ctx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	log.Println("Server exited")
	return nil
}

// applyOAuthFlagOverrides applies CLI flag values to the config struct.
// Only flags that were explicitly set by the user override config/env values.
func applyOAuthFlagOverrides(
	cmd *cobra.Command, cfg *config.Config,
	enableOAuth bool, baseURL, provider string,
	gClientID, gClientSecret string,
	dIssuerURL, dClientID, dClientSecret, dConnectorID, dCAFile string,
	disableStreaming bool, regToken string,
	allowPubReg, allowInsecureAuth bool,
	maxClients int, encKey string,
	eCIMD, cimdPrivIPs bool,
	trustedSchemes []string, disableStrictScheme bool,
	tlsCert, tlsKey string,
) {
	if cmd.Flags().Changed("enable-oauth") {
		cfg.OAuth.Enabled = enableOAuth
	}
	if cmd.Flags().Changed("oauth-base-url") {
		cfg.OAuth.BaseURL = baseURL
	}
	if cmd.Flags().Changed("oauth-provider") {
		cfg.OAuth.Provider = provider
	}
	if cmd.Flags().Changed("google-client-id") {
		cfg.OAuth.Google.ClientID = gClientID
	}
	if cmd.Flags().Changed("google-client-secret") {
		cfg.OAuth.Google.ClientSecret = gClientSecret
	}
	if cmd.Flags().Changed("dex-issuer-url") {
		cfg.OAuth.Dex.IssuerURL = dIssuerURL
	}
	if cmd.Flags().Changed("dex-client-id") {
		cfg.OAuth.Dex.ClientID = dClientID
	}
	if cmd.Flags().Changed("dex-client-secret") {
		cfg.OAuth.Dex.ClientSecret = dClientSecret
	}
	if cmd.Flags().Changed("dex-connector-id") {
		cfg.OAuth.Dex.ConnectorID = dConnectorID
	}
	if cmd.Flags().Changed("dex-ca-file") {
		cfg.OAuth.Dex.CAFile = dCAFile
	}
	if cmd.Flags().Changed("disable-streaming") {
		cfg.OAuth.DisableStreaming = disableStreaming
	}
	if cmd.Flags().Changed("registration-token") {
		cfg.OAuth.Security.RegistrationToken = regToken
	}
	if cmd.Flags().Changed("allow-public-registration") {
		cfg.OAuth.Security.AllowPublicRegistration = allowPubReg
	}
	if cmd.Flags().Changed("allow-insecure-auth-without-state") {
		cfg.OAuth.Security.AllowInsecureAuthWithoutState = allowInsecureAuth
	}
	if cmd.Flags().Changed("max-clients-per-ip") {
		cfg.OAuth.Security.MaxClientsPerIP = maxClients
	}
	if cmd.Flags().Changed("oauth-encryption-key") {
		cfg.OAuth.Security.EncryptionKey = encKey
	}
	if cmd.Flags().Changed("enable-cimd") {
		cfg.OAuth.Security.EnableCIMD = &eCIMD
	}
	if cmd.Flags().Changed("cimd-allow-private-ips") {
		cfg.OAuth.Security.CIMDAllowPrivateIPs = cimdPrivIPs
	}
	if cmd.Flags().Changed("trusted-public-registration-schemes") {
		cfg.OAuth.Security.TrustedPublicRegistrationSchemes = trustedSchemes
	}
	if cmd.Flags().Changed("disable-strict-scheme-matching") {
		cfg.OAuth.Security.DisableStrictSchemeMatching = disableStrictScheme
	}
	if cmd.Flags().Changed("tls-cert-file") {
		cfg.OAuth.TLS.CertFile = tlsCert
	}
	if cmd.Flags().Changed("tls-key-file") {
		cfg.OAuth.TLS.KeyFile = tlsKey
	}
}

// loadSOULFile reads a SOUL.md personality file and returns its content
// with leading/trailing whitespace trimmed. Returns an empty string and
// nil error when the file does not exist. Returns an error if the file
// exceeds maxSOULFileSize.
func loadSOULFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	limited := io.LimitReader(f, maxSOULFileSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if len(data) > maxSOULFileSize {
		return "", fmt.Errorf("SOUL.md exceeds maximum size of %d bytes", maxSOULFileSize)
	}
	return strings.TrimSpace(string(data)), nil
}

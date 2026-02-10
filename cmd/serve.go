package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/server"
)

// newServeCmd creates the Cobra command for starting the klaus server.
func newServeCmd() *cobra.Command {
	var (
		port string

		// OAuth options
		enableOAuth                     bool
		oauthBaseURL                    string
		oauthProvider                   string
		googleClientID                  string
		googleClientSecret              string
		dexIssuerURL                    string
		dexClientID                     string
		dexClientSecret                 string
		dexConnectorID                  string
		dexCAFile                       string
		disableStreaming                bool
		registrationToken               string
		allowPublicRegistration         bool
		allowInsecureAuthWithoutState   bool
		maxClientsPerIP                 int
		oauthEncryptionKey              string
		enableCIMD                      bool
		cimdAllowPrivateIPs             bool
		trustedPublicRegistrationSchemes []string
		disableStrictSchemeMatching      bool
		tlsCertFile                     string
		tlsKeyFile                      string
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

Configuration is primarily via environment variables:
  CLAUDE_MODEL             -- Claude model to use
  CLAUDE_SYSTEM_PROMPT     -- System prompt
  CLAUDE_APPEND_SYSTEM_PROMPT -- Append to system prompt
  CLAUDE_MAX_TURNS         -- Max agentic turns
  CLAUDE_PERMISSION_MODE   -- Permission mode
  CLAUDE_MCP_CONFIG        -- MCP config file path
  CLAUDE_WORKSPACE         -- Working directory
  PORT                     -- HTTP server port (default: 8080)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load TLS paths from environment if not provided via flags.
			loadEnvIfEmpty(&tlsCertFile, "TLS_CERT_FILE")
			loadEnvIfEmpty(&tlsKeyFile, "TLS_KEY_FILE")

			// Load OAuth env vars for flags not explicitly set.
			loadEnvIfEmpty(&googleClientID, "GOOGLE_CLIENT_ID")
			loadEnvIfEmpty(&googleClientSecret, "GOOGLE_CLIENT_SECRET")
			loadEnvIfEmpty(&dexIssuerURL, "DEX_ISSUER_URL")
			loadEnvIfEmpty(&dexClientID, "DEX_CLIENT_ID")
			loadEnvIfEmpty(&dexClientSecret, "DEX_CLIENT_SECRET")
			loadEnvIfEmpty(&dexConnectorID, "DEX_CONNECTOR_ID")
			loadEnvIfEmpty(&dexCAFile, "DEX_CA_FILE")
			loadEnvIfEmpty(&oauthEncryptionKey, "OAUTH_ENCRYPTION_KEY")

			var encryptionKey []byte
			if enableOAuth && oauthEncryptionKey != "" {
				decoded, err := base64.StdEncoding.DecodeString(oauthEncryptionKey)
				if err != nil {
					return fmt.Errorf("OAuth encryption key must be base64 encoded (use: openssl rand -base64 32): %w", err)
				}
				if len(decoded) != 32 {
					return fmt.Errorf("encryption key must be exactly 32 bytes, got %d bytes", len(decoded))
				}
				encryptionKey = decoded
			}

			oauthConfig := server.OAuthConfig{
				BaseURL:  oauthBaseURL,
				Provider: oauthProvider,
				Google: server.GoogleOAuthConfig{
					ClientID:     googleClientID,
					ClientSecret: googleClientSecret,
				},
				Dex: server.DexOAuthConfig{
					IssuerURL:    dexIssuerURL,
					ClientID:     dexClientID,
					ClientSecret: dexClientSecret,
					ConnectorID:  dexConnectorID,
					CAFile:       dexCAFile,
				},
				Security: server.SecurityConfig{
					EncryptionKey:                    encryptionKey,
					RegistrationAccessToken:          registrationToken,
					AllowPublicClientRegistration:    allowPublicRegistration,
					AllowInsecureAuthWithoutState:    allowInsecureAuthWithoutState,
					MaxClientsPerIP:                  maxClientsPerIP,
					EnableCIMD:                       enableCIMD,
					CIMDAllowPrivateIPs:              cimdAllowPrivateIPs,
					TrustedPublicRegistrationSchemes: trustedPublicRegistrationSchemes,
					DisableStrictSchemeMatching:       disableStrictSchemeMatching,
				},
				TLS: server.TLSConfig{
					CertFile: tlsCertFile,
					KeyFile:  tlsKeyFile,
				},
				DisableStreaming: disableStreaming,
				DebugMode:       false,
			}

			return runServe(port, enableOAuth, oauthConfig)
		},
	}

	cmd.Flags().StringVar(&port, "port", "", "HTTP server port (overrides PORT env var, default: 8080)")

	// OAuth flags
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

// runServe contains the main server logic.
func runServe(portFlag string, enableOAuth bool, oauthConfig server.OAuthConfig) error {
	// Build Claude options from environment variables.
	opts := claude.DefaultOptions()

	if v := os.Getenv("CLAUDE_MODEL"); v != "" {
		opts.Model = v
	}
	if v := os.Getenv("CLAUDE_SYSTEM_PROMPT"); v != "" {
		opts.SystemPrompt = v
	}
	if v := os.Getenv("CLAUDE_APPEND_SYSTEM_PROMPT"); v != "" {
		opts.AppendSystemPrompt = v
	}
	if v := os.Getenv("CLAUDE_MAX_TURNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.MaxTurns = n
		}
	}
	if v := os.Getenv("CLAUDE_PERMISSION_MODE"); v != "" {
		opts.PermissionMode = v
	}
	if v := os.Getenv("CLAUDE_MCP_CONFIG"); v != "" {
		opts.MCPConfigPath = v
	}
	if v := os.Getenv("CLAUDE_WORKSPACE"); v != "" {
		opts.WorkDir = v
	}

	// Create the Claude process manager.
	process := claude.NewProcess(opts)

	// Determine listen port: flag > env > default.
	port := portFlag
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	// Wait for interrupt signal for graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	if enableOAuth {
		return runWithOAuth(process, port, oauthConfig, quit)
	}
	return runWithoutOAuth(process, port, quit)
}

func runWithOAuth(process *claude.Process, port string, config server.OAuthConfig, quit chan os.Signal) error {
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

	oauthSrv, err := server.NewOAuthServer(process, config)
	if err != nil {
		return fmt.Errorf("failed to create OAuth server: %w", err)
	}

	addr := ":" + port
	return runServerLifecycle(
		func() error { return oauthSrv.Start(addr, config) },
		oauthSrv.Shutdown,
		process,
		quit,
	)
}

func runWithoutOAuth(process *claude.Process, port string, quit chan os.Signal) error {
	srv := server.New(process, port)
	return runServerLifecycle(srv.Start, srv.Shutdown, process, quit)
}

// runServerLifecycle runs a server in a goroutine, waits for a shutdown signal
// or server error, stops the Claude process, and gracefully shuts down.
func runServerLifecycle(
	startFn func() error,
	shutdownFn func(context.Context) error,
	process *claude.Process,
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

// loadEnvIfEmpty loads an environment variable into a string pointer if it's empty.
func loadEnvIfEmpty(target *string, envKey string) {
	if *target == "" {
		*target = os.Getenv(envKey)
	}
}

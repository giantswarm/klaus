package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	oauth "github.com/giantswarm/mcp-oauth"
	"github.com/giantswarm/mcp-oauth/providers"
	"github.com/giantswarm/mcp-oauth/providers/dex"
	"github.com/giantswarm/mcp-oauth/providers/google"
	"github.com/giantswarm/mcp-oauth/security"
	oauthserver "github.com/giantswarm/mcp-oauth/server"
	"github.com/giantswarm/mcp-oauth/storage"
	"github.com/giantswarm/mcp-oauth/storage/memory"
	mcpserver "github.com/mark3labs/mcp-go/server"

	claudepkg "github.com/giantswarm/klaus/pkg/claude"
	mcppkg "github.com/giantswarm/klaus/pkg/mcp"
	"github.com/giantswarm/klaus/pkg/project"
)

const (
	// OAuthProviderDex is the Dex OIDC provider.
	OAuthProviderDex = "dex"
	// OAuthProviderGoogle is the Google OAuth provider.
	OAuthProviderGoogle = "google"

	// DefaultRefreshTokenTTL is the default TTL for refresh tokens (90 days).
	DefaultRefreshTokenTTL = 90 * 24 * time.Hour

	// DefaultIPRateLimit is the default rate limit per IP (requests/second).
	DefaultIPRateLimit = 10
	// DefaultIPBurst is the default burst size for IP rate limiting.
	DefaultIPBurst = 20
	// DefaultUserRateLimit is the default rate limit for authenticated users.
	DefaultUserRateLimit = 100
	// DefaultUserBurst is the default burst size for authenticated user rate limiting.
	DefaultUserBurst = 200
	// DefaultMaxClientsPerIP is the default max clients per IP address.
	DefaultMaxClientsPerIP = 10

	// DefaultReadHeaderTimeout is the default timeout for reading request headers.
	DefaultReadHeaderTimeout = 10 * time.Second
	// DefaultWriteTimeout is the default timeout for writing responses.
	DefaultWriteTimeout = 120 * time.Second
	// DefaultIdleTimeout is the default idle timeout for keepalive connections.
	DefaultIdleTimeout = 120 * time.Second
	// DefaultShutdownTimeout is the default timeout for graceful shutdown.
	DefaultShutdownTimeout = 30 * time.Second
)

var (
	dexOAuthScopes    = []string{"openid", "profile", "email", "groups", "offline_access"}
	googleOAuthScopes = []string{
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	}
)

// OAuthConfig holds OAuth configuration for the klaus server.
type OAuthConfig struct {
	// BaseURL is the server base URL (e.g., https://klaus.example.com).
	BaseURL string

	// Provider specifies the OAuth provider: "dex" or "google".
	Provider string

	// GoogleClientID is the Google OAuth Client ID.
	GoogleClientID string
	// GoogleClientSecret is the Google OAuth Client Secret.
	GoogleClientSecret string

	// DexIssuerURL is the Dex OIDC issuer URL.
	DexIssuerURL string
	// DexClientID is the Dex OAuth Client ID.
	DexClientID string
	// DexClientSecret is the Dex OAuth Client Secret.
	DexClientSecret string
	// DexConnectorID is the optional Dex connector ID to bypass connector selection.
	DexConnectorID string
	// DexCAFile is the path to a CA certificate file for Dex TLS verification.
	DexCAFile string

	// DisableStreaming disables streaming for streamable-http transport.
	DisableStreaming bool

	// DebugMode enables debug logging.
	DebugMode bool

	// EncryptionKey is the AES-256 key for encrypting tokens at rest (32 bytes).
	EncryptionKey []byte

	// RegistrationAccessToken is the token required for client registration.
	RegistrationAccessToken string

	// AllowPublicClientRegistration allows unauthenticated dynamic client registration.
	AllowPublicClientRegistration bool

	// AllowInsecureAuthWithoutState allows authorization requests without state parameter.
	AllowInsecureAuthWithoutState bool

	// MaxClientsPerIP limits the number of clients registered per IP.
	MaxClientsPerIP int

	// EnableCIMD enables Client ID Metadata Documents per MCP 2025-11-25.
	EnableCIMD bool

	// CIMDAllowPrivateIPs allows CIMD metadata URLs to resolve to private IPs.
	CIMDAllowPrivateIPs bool

	// TrustedPublicRegistrationSchemes lists URI schemes allowed for unauthenticated registration.
	TrustedPublicRegistrationSchemes []string

	// DisableStrictSchemeMatching allows mixed redirect URI schemes.
	DisableStrictSchemeMatching bool

	// TLSCertFile is the path to the TLS certificate file (PEM format).
	TLSCertFile string
	// TLSKeyFile is the path to the TLS private key file (PEM format).
	TLSKeyFile string
}

// OAuthServer wraps the MCP server with OAuth 2.1 authentication.
type OAuthServer struct {
	process      *claudepkg.Process
	oauthServer  *oauth.Server
	oauthHandler *oauth.Handler
	httpServer   *http.Server
}

// NewOAuthServer creates a new OAuth-enabled HTTP server for Klaus.
func NewOAuthServer(process *claudepkg.Process, config OAuthConfig) (*OAuthServer, error) {
	oauthSrv, err := createOAuthServer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create OAuth server: %w", err)
	}

	oauthHandler := oauth.NewHandler(oauthSrv, oauthSrv.Logger)

	return &OAuthServer{
		process:      process,
		oauthServer:  oauthSrv,
		oauthHandler: oauthHandler,
	}, nil
}

// Start starts the OAuth-enabled HTTP server.
func (s *OAuthServer) Start(addr string, config OAuthConfig) error {
	// Validate HTTPS requirement.
	if err := ValidateHTTPSRequirement(config.BaseURL); err != nil {
		return err
	}

	mux := http.NewServeMux()

	// OAuth 2.1 endpoints.
	s.setupOAuthRoutes(mux)

	// MCP endpoint (protected by OAuth).
	if err := s.setupMCPRoutes(mux, config); err != nil {
		return err
	}

	// Health and status endpoints (unprotected).
	registerOperationalRoutes(mux, s.process)

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
	}

	log.Printf("Starting %s with OAuth on %s", project.Name, addr)
	log.Printf("  Base URL: %s", config.BaseURL)
	log.Printf("  MCP endpoint: /mcp (requires OAuth Bearer token)")
	log.Printf("  Health endpoints: /healthz, /readyz")
	log.Printf("  OAuth endpoints:")
	log.Printf("    - Authorization Server Metadata: /.well-known/oauth-authorization-server")
	log.Printf("    - Protected Resource Metadata: /.well-known/oauth-protected-resource")
	log.Printf("    - Client Registration: /oauth/register")
	log.Printf("    - Authorization: /oauth/authorize")
	log.Printf("    - Token: /oauth/token")
	log.Printf("    - Callback: /oauth/callback")
	log.Printf("    - Revoke: /oauth/revoke")
	log.Printf("    - Introspect: /oauth/introspect")

	if config.TLSCertFile != "" && config.TLSKeyFile != "" {
		return s.httpServer.ListenAndServeTLS(config.TLSCertFile, config.TLSKeyFile)
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the OAuth server.
func (s *OAuthServer) Shutdown(ctx context.Context) error {
	if s.oauthServer != nil {
		if err := s.oauthServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown OAuth server: %w", err)
		}
	}
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *OAuthServer) setupOAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-protected-resource", s.oauthHandler.ServeProtectedResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.oauthHandler.ServeAuthorizationServerMetadata)
	mux.HandleFunc("/oauth/register", s.oauthHandler.ServeClientRegistration)
	mux.HandleFunc("/oauth/authorize", s.oauthHandler.ServeAuthorization)
	mux.HandleFunc("/oauth/token", s.oauthHandler.ServeToken)
	mux.HandleFunc("/oauth/callback", s.oauthHandler.ServeCallback)
	mux.HandleFunc("/oauth/revoke", s.oauthHandler.ServeTokenRevocation)
	mux.HandleFunc("/oauth/introspect", s.oauthHandler.ServeTokenIntrospection)
}

func (s *OAuthServer) setupMCPRoutes(mux *http.ServeMux, config OAuthConfig) error {
	// Create MCP server and register tools.
	mcpSrv := mcppkg.NewMCPServer(s.process)

	var httpServer http.Handler
	if config.DisableStreaming {
		httpServer = mcpserver.NewStreamableHTTPServer(mcpSrv,
			mcpserver.WithEndpointPath("/mcp"),
			mcpserver.WithDisableStreaming(true),
		)
	} else {
		httpServer = mcpserver.NewStreamableHTTPServer(mcpSrv,
			mcpserver.WithEndpointPath("/mcp"),
		)
	}

	// Wrap MCP endpoint with OAuth token validation middleware.
	mux.Handle("/mcp", s.oauthHandler.ValidateToken(httpServer))

	return nil
}

// createOAuthServer creates an OAuth server using the mcp-oauth library.
func createOAuthServer(config OAuthConfig) (*oauth.Server, error) {
	var logger *slog.Logger
	if config.DebugMode {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	} else {
		logger = slog.Default()
	}

	redirectURL := config.BaseURL + "/oauth/callback"
	var provider providers.Provider
	var err error

	switch config.Provider {
	case OAuthProviderDex:
		dexConfig := &dex.Config{
			IssuerURL:    config.DexIssuerURL,
			ClientID:     config.DexClientID,
			ClientSecret: config.DexClientSecret,
			RedirectURL:  redirectURL,
			Scopes:       dexOAuthScopes,
		}
		if config.DexConnectorID != "" {
			dexConfig.ConnectorID = config.DexConnectorID
		}
		if config.DexCAFile != "" {
			httpClient, err := createHTTPClientWithCA(config.DexCAFile)
			if err != nil {
				return nil, fmt.Errorf("failed to create HTTP client with CA: %w", err)
			}
			dexConfig.HTTPClient = httpClient
			logger.Info("Using custom CA for Dex TLS verification", "caFile", config.DexCAFile)
		}
		provider, err = dex.NewProvider(dexConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Dex provider: %w", err)
		}
		logger.Info("Using Dex OIDC provider", "issuer", config.DexIssuerURL)

	case OAuthProviderGoogle:
		provider, err = google.NewProvider(&google.Config{
			ClientID:     config.GoogleClientID,
			ClientSecret: config.GoogleClientSecret,
			RedirectURL:  redirectURL,
			Scopes:       googleOAuthScopes,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create Google provider: %w", err)
		}
		logger.Info("Using Google OAuth provider")

	default:
		return nil, fmt.Errorf("unsupported OAuth provider: %s (supported: %s, %s)", config.Provider, OAuthProviderDex, OAuthProviderGoogle)
	}

	// Use in-memory storage (sufficient for single-instance deployments).
	memStore := memory.New()
	var tokenStore storage.TokenStore = memStore
	var clientStore storage.ClientStore = memStore
	var flowStore storage.FlowStore = memStore

	maxClientsPerIP := config.MaxClientsPerIP
	if maxClientsPerIP == 0 {
		maxClientsPerIP = DefaultMaxClientsPerIP
	}

	serverConfig := &oauthserver.Config{
		Issuer:                        config.BaseURL,
		RefreshTokenTTL:               int64(DefaultRefreshTokenTTL.Seconds()),
		AllowRefreshTokenRotation:     true,
		RequirePKCE:                   true,
		AllowPKCEPlain:                false,
		AllowPublicClientRegistration: config.AllowPublicClientRegistration,
		RegistrationAccessToken:       config.RegistrationAccessToken,
		AllowNoStateParameter:         config.AllowInsecureAuthWithoutState,
		MaxClientsPerIP:               maxClientsPerIP,

		// CIMD per MCP 2025-11-25.
		EnableClientIDMetadataDocuments: config.EnableCIMD,
		AllowPrivateIPClientMetadata:    config.CIMDAllowPrivateIPs,

		// Trusted scheme registration.
		TrustedPublicRegistrationSchemes: config.TrustedPublicRegistrationSchemes,
		DisableStrictSchemeMatching:      config.DisableStrictSchemeMatching,

		// Instrumentation.
		Instrumentation: oauthserver.InstrumentationConfig{
			Enabled:        true,
			ServiceName:    "klaus",
			ServiceVersion: project.Version(),
		},
	}

	srv, err := oauth.NewServer(
		provider,
		tokenStore,
		clientStore,
		flowStore,
		serverConfig,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OAuth server: %w", err)
	}

	// Set up encryption if key provided.
	if len(config.EncryptionKey) > 0 {
		encryptor, err := security.NewEncryptor(config.EncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create encryptor: %w", err)
		}
		srv.SetEncryptor(encryptor)
		logger.Info("Token encryption at rest enabled (AES-256-GCM)")
	}

	// Set up audit logging.
	auditor := security.NewAuditor(logger, true)
	srv.SetAuditor(auditor)

	// Set up rate limiting.
	ipRateLimiter := security.NewRateLimiter(DefaultIPRateLimit, DefaultIPBurst, logger)
	srv.SetRateLimiter(ipRateLimiter)

	userRateLimiter := security.NewRateLimiter(DefaultUserRateLimit, DefaultUserBurst, logger)
	srv.SetUserRateLimiter(userRateLimiter)

	clientRegRL := security.NewClientRegistrationRateLimiterWithConfig(
		maxClientsPerIP,
		security.DefaultRegistrationWindow,
		security.DefaultMaxRegistrationEntries,
		logger,
	)
	srv.SetClientRegistrationRateLimiter(clientRegRL)

	return srv, nil
}

// createHTTPClientWithCA creates an HTTP client that trusts the CA in the specified file.
func createHTTPClientWithCA(caFile string) (*http.Client, error) {
	caCert, err := os.ReadFile(caFile) //#nosec G304 -- operator-provided config
	if err != nil {
		return nil, fmt.Errorf("failed to read CA file %s: %w", caFile, err)
	}

	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		caCertPool = x509.NewCertPool()
	}

	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate from %s", caFile)
	}

	tlsConfig := &tls.Config{
		RootCAs:    caCertPool,
		MinVersion: tls.VersionTLS12,
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

// ValidateHTTPSRequirement ensures OAuth 2.1 HTTPS compliance.
// Allows HTTP only for loopback addresses (localhost, 127.0.0.1, ::1).
func ValidateHTTPSRequirement(baseURL string) error {
	if baseURL == "" {
		return fmt.Errorf("base URL cannot be empty")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}

	if u.Scheme == "http" {
		host := u.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return fmt.Errorf("OAuth 2.1 requires HTTPS for production (got: %s). Use HTTPS or localhost for development", baseURL)
		}
	} else if u.Scheme != "https" {
		return fmt.Errorf("invalid URL scheme: %s. Must be http (localhost only) or https", u.Scheme)
	}

	return nil
}

package server

import "testing"

func TestOAuthConfig_Validate(t *testing.T) {
	validDex := OAuthConfig{
		BaseURL:  "https://klaus.example.com",
		Provider: OAuthProviderDex,
		Dex: DexOAuthConfig{
			IssuerURL:    "https://dex.example.com",
			ClientID:     "test-id",
			ClientSecret: "test-secret",
		},
		Security: SecurityConfig{
			RegistrationAccessToken: "token",
		},
	}

	tests := []struct {
		name    string
		mutate  func(c *OAuthConfig)
		wantErr bool
	}{
		{
			name:    "valid dex config",
			mutate:  func(_ *OAuthConfig) {},
			wantErr: false,
		},
		{
			name:    "missing base URL",
			mutate:  func(c *OAuthConfig) { c.BaseURL = "" },
			wantErr: true,
		},
		{
			name:    "missing dex issuer URL",
			mutate:  func(c *OAuthConfig) { c.Dex.IssuerURL = "" },
			wantErr: true,
		},
		{
			name:    "missing dex client ID",
			mutate:  func(c *OAuthConfig) { c.Dex.ClientID = "" },
			wantErr: true,
		},
		{
			name:    "missing dex client secret",
			mutate:  func(c *OAuthConfig) { c.Dex.ClientSecret = "" },
			wantErr: true,
		},
		{
			name: "google missing client ID",
			mutate: func(c *OAuthConfig) {
				c.Provider = OAuthProviderGoogle
				c.Google = GoogleOAuthConfig{ClientSecret: "s"}
			},
			wantErr: true,
		},
		{
			name: "google valid",
			mutate: func(c *OAuthConfig) {
				c.Provider = OAuthProviderGoogle
				c.Google = GoogleOAuthConfig{ClientID: "id", ClientSecret: "s"}
			},
			wantErr: false,
		},
		{
			name:    "unsupported provider",
			mutate:  func(c *OAuthConfig) { c.Provider = "unknown" },
			wantErr: true,
		},
		{
			name:    "TLS cert without key",
			mutate:  func(c *OAuthConfig) { c.TLS.CertFile = "cert.pem" },
			wantErr: true,
		},
		{
			name: "no registration token and no alternatives",
			mutate: func(c *OAuthConfig) {
				c.Security.RegistrationAccessToken = ""
			},
			wantErr: true,
		},
		{
			name: "CIMD as alternative to registration token",
			mutate: func(c *OAuthConfig) {
				c.Security.RegistrationAccessToken = ""
				c.Security.EnableCIMD = true
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validDex // copy
			tt.mutate(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("OAuthConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateHTTPSRequirement(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{
			name:    "valid https",
			baseURL: "https://klaus.example.com",
			wantErr: false,
		},
		{
			name:    "http localhost allowed",
			baseURL: "http://localhost:8080",
			wantErr: false,
		},
		{
			name:    "http 127.0.0.1 allowed",
			baseURL: "http://127.0.0.1:8080",
			wantErr: false,
		},
		{
			name:    "http ::1 allowed",
			baseURL: "http://[::1]:8080",
			wantErr: false,
		},
		{
			name:    "http non-loopback rejected",
			baseURL: "http://example.com",
			wantErr: true,
		},
		{
			name:    "empty URL",
			baseURL: "",
			wantErr: true,
		},
		{
			name:    "invalid scheme",
			baseURL: "ftp://example.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHTTPSRequirement(tt.baseURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHTTPSRequirement(%q) error = %v, wantErr %v", tt.baseURL, err, tt.wantErr)
			}
		})
	}
}

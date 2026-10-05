package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/giantswarm/klaus/pkg/claude"
)

// discardLogger returns a slog.Logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const testAudience = "muster"

// testIssuer is an OIDC issuer (discovery document and JWKS) that signs
// tokens with its own RSA key.
type testIssuer struct {
	url string
	key *rsa.PrivateKey
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	iss := &testIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss.url,
			"jwks_uri":                              iss.url + "/keys",
			"authorization_endpoint":                iss.url + "/auth",
			"token_endpoint":                        iss.url + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: string(jose.RS256), Use: "sig"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	iss.url = srv.URL
	return iss
}

// sign returns a JWT with the issuer's defaults (iss, aud, a future exp)
// overridden by claims, signed with key.
func (i *testIssuer) sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	all := map[string]any{
		"iss": i.url,
		"aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range claims {
		all[k] = v
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Signed(signer).Claims(all).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// unsignedToken is the forgery from giantswarm/giantswarm#38099: an alg:none
// header and the owner's claims, with no signature.
func unsignedToken(claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "none", "typ": "JWT"}) + "." + enc(claims) + "."
}

func newTestVerifier(t *testing.T, iss *testIssuer) *OIDCVerifier {
	t.Helper()
	v, err := NewOIDCVerifier(context.Background(), iss.url, []string{"other-client", testAudience})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVerifyAndOwnerMiddleware(t *testing.T) {
	const owner = "owner@example.com"
	iss := newTestIssuer(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		token string
		want  int
	}{
		{"owner by subject", iss.sign(t, iss.key, map[string]any{"sub": owner}), http.StatusOK},
		{"owner by verified email", iss.sign(t, iss.key, map[string]any{"sub": "u1", "email": owner, "email_verified": true}), http.StatusOK},
		{"unverified email is not the owner", iss.sign(t, iss.key, map[string]any{"sub": "u1", "email": owner}), http.StatusForbidden},
		{"other user", iss.sign(t, iss.key, map[string]any{"sub": "u2", "email": "u2@example.com", "email_verified": true}), http.StatusForbidden},
		{"unsigned token with the owner's claims", unsignedToken(map[string]any{"iss": iss.url, "aud": testAudience, "sub": owner, "email": owner}), http.StatusUnauthorized},
		{"signed by another key", iss.sign(t, otherKey, map[string]any{"sub": owner}), http.StatusUnauthorized},
		{"expired", iss.sign(t, iss.key, map[string]any{"sub": owner, "exp": time.Now().Add(-time.Hour).Unix()}), http.StatusUnauthorized},
		{"untrusted audience", iss.sign(t, iss.key, map[string]any{"sub": owner, "aud": "someone-else"}), http.StatusUnauthorized},
		{"other issuer", iss.sign(t, iss.key, map[string]any{"sub": owner, "iss": "https://evil.example.com"}), http.StatusUnauthorized},
		{"no token", "", http.StatusUnauthorized},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := VerifyTokenMiddleware(newTestVerifier(t, iss), discardLogger())(OwnerMiddleware(owner, discardLogger())(next))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body %q)", w.Code, tc.want, strings.TrimSpace(w.Body.String()))
			}
		})
	}
}

// TestNewServer_RejectsUnsignedOwnerToken is the regression for
// giantswarm/giantswarm#38099: the forged token reached the agent on both
// protected endpoints.
func TestNewServer_RejectsUnsignedOwnerToken(t *testing.T) {
	const owner = "owner@example.com"
	iss := newTestIssuer(t)
	srv := NewServer(t.Context(), claude.NewProcess(claude.DefaultOptions()), Config{
		Mode:         ModeAgent,
		OwnerSubject: owner,
		Verifier:     newTestVerifier(t, iss),
	})
	forged := unsignedToken(map[string]any{"iss": iss.url, "aud": testAudience, "sub": owner, "email": owner})

	for _, path := range []string{"/mcp", "/v1/chat/completions"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+forged)
			w := httptest.NewRecorder()
			srv.httpServer.Handler.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestOwnerMiddleware_NoVerifiedIdentity(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+unsignedToken(map[string]any{"sub": "owner"}))
	w := httptest.NewRecorder()

	OwnerMiddleware("owner", discardLogger())(next).ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d: the owner check must not trust an unverified token", w.Code, http.StatusForbidden)
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name  string
		auth  string
		token string
	}{
		{
			name:  "standard bearer",
			auth:  "Bearer abc123",
			token: "abc123",
		},
		{
			name:  "lowercase bearer",
			auth:  "bearer abc123",
			token: "abc123",
		},
		{
			name:  "empty header",
			auth:  "",
			token: "",
		},
		{
			name:  "basic auth",
			auth:  "Basic abc123",
			token: "",
		},
		{
			name:  "bearer only no token",
			auth:  "Bearer ",
			token: "",
		},
		{
			name:  "too short",
			auth:  "Bear",
			token: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			got := extractBearerToken(req)
			if got != tc.token {
				t.Errorf("extractBearerToken() = %q, want %q", got, tc.token)
			}
		})
	}
}

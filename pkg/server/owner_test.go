package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// discardLogger returns a slog.Logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// buildTestJWT creates an unsigned JWT with the given claims for testing.
// The header and signature are valid placeholders; only the payload matters.
func buildTestJWT(t *testing.T, claims map[string]string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("failed to marshal claims: %v", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + encodedPayload + ".signature"
}

func TestOwnerMiddleware_NoOwnerConfigured(t *testing.T) {
	handler := OwnerMiddleware("", discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when no owner configured, got %d", w.Code)
	}
}

func TestOwnerMiddleware_MatchingSub(t *testing.T) {
	token := buildTestJWT(t, map[string]string{
		"sub":   "user-123",
		"email": "other@example.com",
	})

	handler := OwnerMiddleware("user-123", discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when sub matches owner, got %d", w.Code)
	}
}

func TestOwnerMiddleware_MatchingEmail(t *testing.T) {
	token := buildTestJWT(t, map[string]string{
		"sub":   "other-sub",
		"email": "owner@example.com",
	})

	handler := OwnerMiddleware("owner@example.com", discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when email matches owner, got %d", w.Code)
	}
}

func TestOwnerMiddleware_NonMatchingClaims(t *testing.T) {
	token := buildTestJWT(t, map[string]string{
		"sub":   "other-user",
		"email": "other@example.com",
	})

	handler := OwnerMiddleware("owner@example.com", discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not have been called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when claims don't match, got %d", w.Code)
	}
}

func TestOwnerMiddleware_NoToken(t *testing.T) {
	handler := OwnerMiddleware("owner@example.com", discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not have been called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when no token and owner configured, got %d", w.Code)
	}
}

func TestOwnerMiddleware_MalformedJWT(t *testing.T) {
	handler := OwnerMiddleware("owner@example.com", discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not have been called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for malformed JWT, got %d", w.Code)
	}
}

func TestOwnerMiddleware_InvalidBase64Payload(t *testing.T) {
	handler := OwnerMiddleware("owner@example.com", discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not have been called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer header.!!!invalid-base64!!!.signature")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for invalid base64 payload, got %d", w.Code)
	}
}

func TestOwnerMiddleware_NonBearerAuth(t *testing.T) {
	handler := OwnerMiddleware("owner@example.com", discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not have been called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-Bearer auth, got %d", w.Code)
	}
}

func TestOwnerMiddleware_CaseInsensitiveBearer(t *testing.T) {
	token := buildTestJWT(t, map[string]string{
		"sub": "user-123",
	})

	handler := OwnerMiddleware("user-123", discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with lowercase 'bearer' prefix, got %d", w.Code)
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

func TestDecodeJWTClaims(t *testing.T) {
	tests := []struct {
		name string
		// claims builds a JWT via buildTestJWT when non-nil; rawToken is used otherwise.
		claims    map[string]string
		rawToken  string
		wantSub   string
		wantEmail string
		wantErr   bool
	}{
		{
			name:      "valid token with sub and email",
			claims:    map[string]string{"sub": "user-1", "email": "user@test.com"},
			wantSub:   "user-1",
			wantEmail: "user@test.com",
		},
		{
			name:    "valid token with sub only",
			claims:  map[string]string{"sub": "user-2"},
			wantSub: "user-2",
		},
		{
			name:     "malformed - single segment",
			rawToken: "onlyone",
			wantErr:  true,
		},
		{
			name:     "invalid base64 payload",
			rawToken: "header.!!!.sig",
			wantErr:  true,
		},
		{
			name:     "invalid JSON payload",
			rawToken: "header." + base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".sig",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token := tc.rawToken
			if tc.claims != nil {
				token = buildTestJWT(t, tc.claims)
			}

			claims, err := decodeJWTClaims(token)
			if (err != nil) != tc.wantErr {
				t.Fatalf("decodeJWTClaims() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if claims.Sub != tc.wantSub {
				t.Errorf("sub = %q, want %q", claims.Sub, tc.wantSub)
			}
			if claims.Email != tc.wantEmail {
				t.Errorf("email = %q, want %q", claims.Email, tc.wantEmail)
			}
		})
	}
}

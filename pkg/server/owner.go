package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/giantswarm/mcp-oauth/handler"
)

// Identity is the caller identity taken from a verified token.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
}

type identityKey struct{}

func contextWithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

func identityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

// TokenVerifier verifies a bearer token and returns the identity it carries.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (Identity, error)
}

// OIDCVerifier verifies JWTs issued by an OIDC provider (for example the Dex
// ID token that muster forwards): signature against the issuer's JWKS,
// issuer, expiry, and an audience in the allowlist.
type OIDCVerifier struct {
	verifier  *oidc.IDTokenVerifier
	audiences []string
}

// NewOIDCVerifier discovers the issuer's keys. audiences must not be empty: a
// token is accepted only when one of its aud values is in the list.
func NewOIDCVerifier(ctx context.Context, issuerURL string, audiences []string) (*OIDCVerifier, error) {
	if len(audiences) == 0 {
		return nil, errors.New("at least one trusted audience is required")
	}
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("discovering OIDC issuer %q: %w", issuerURL, err)
	}
	return &OIDCVerifier{
		// go-oidc checks a single client ID; the allowlist is checked in Verify.
		verifier:  provider.Verifier(&oidc.Config{SkipClientIDCheck: true}),
		audiences: audiences,
	}, nil
}

// Verify implements TokenVerifier.
func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (Identity, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Identity{}, err
	}
	if !slices.ContainsFunc(token.Audience, func(aud string) bool { return slices.Contains(v.audiences, aud) }) {
		return Identity{}, fmt.Errorf("token audience %v is not trusted", token.Audience)
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := token.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("decoding token claims: %w", err)
	}
	return Identity{Subject: token.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified}, nil
}

// VerifyTokenMiddleware rejects a request without a bearer token that the
// verifier accepts, and puts the verified identity on the request context.
func VerifyTokenMiddleware(verifier TokenVerifier, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "Unauthorized: bearer token required", http.StatusUnauthorized)
				return
			}
			id, err := verifier.Verify(r.Context(), token)
			if err != nil {
				logger.Warn("Token verification failed", "error", err)
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(contextWithIdentity(r.Context(), id)))
		})
	}
}

// oauthIdentityMiddleware copies the identity that mcp-oauth's ValidateToken
// verified onto the request context. It must run after ValidateToken.
func oauthIdentityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if info, ok := handler.UserInfoFromContext(r.Context()); ok && info != nil {
			r = r.WithContext(contextWithIdentity(r.Context(), Identity{
				Subject:       info.ID,
				Email:         info.Email,
				EmailVerified: info.EmailVerified,
			}))
		}
		next.ServeHTTP(w, r)
	})
}

// OwnerMiddleware restricts access to the configured owner identity. It reads
// the identity a verifying middleware put on the request context and never
// reads the token itself, so it must run after VerifyTokenMiddleware or after
// mcp-oauth's ValidateToken. A request without a verified identity is refused.
// The owner matches the subject, or the email when the token marks it verified.
//
// When ownerSubject is empty the middleware is a no-op.
func OwnerMiddleware(ownerSubject string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if ownerSubject == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := identityFromContext(r.Context())
			if !ok {
				http.Error(w, "Forbidden: owner verification required but no verified identity", http.StatusForbidden)
				return
			}

			emailMatches := id.EmailVerified && id.Email == ownerSubject
			if id.Subject != ownerSubject && !emailMatches {
				logger.Warn("Owner middleware: access denied", "sub", id.Subject, "email", id.Email)
				http.Error(w, "Forbidden: not the instance owner", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractBearerToken returns the token from an "Authorization: Bearer <token>"
// header, or an empty string if no bearer token is present.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	const prefix = "Bearer "
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}

	return auth[len(prefix):]
}

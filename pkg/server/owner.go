package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// jwtClaims holds the subset of JWT claims used for owner validation.
// Only the sub and email fields are inspected; all other claims are ignored.
type jwtClaims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
}

// OwnerMiddleware returns middleware that restricts access to the configured
// owner identity. The owner is identified by matching the JWT sub or email
// claim against ownerSubject.
//
// When ownerSubject is empty the middleware is a no-op (backward-compatible).
// When a request has no Authorization Bearer token and ownerSubject is set,
// the request is rejected with HTTP 403.
//
// The JWT is decoded but not signature-verified (decode-only). In Kubernetes
// deployments the upstream gateway (muster) or the OAuth layer is responsible
// for token verification; this middleware only performs the authorization check.
func OwnerMiddleware(ownerSubject string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// Fast path: no owner configured, skip validation entirely.
		if ownerSubject == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, "Forbidden: owner verification required but no token provided", http.StatusForbidden)
				return
			}

			claims, err := decodeJWTClaims(token)
			if err != nil {
				logger.Warn("Owner middleware: failed to decode JWT claims", "error", err)
				http.Error(w, "Forbidden: invalid token", http.StatusForbidden)
				return
			}

			if claims.Sub != ownerSubject && claims.Email != ownerSubject {
				logger.Warn("Owner middleware: access denied", "sub", claims.Sub, "email", claims.Email, "owner", ownerSubject)
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

// decodeJWTClaims performs a decode-only (no signature verification) extraction
// of claims from a JWT. The token is split into its three dot-separated parts
// and the payload (second part) is base64url-decoded and unmarshalled.
func decodeJWTClaims(token string) (jwtClaims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return jwtClaims{}, errMalformedJWT
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, err
	}

	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return jwtClaims{}, err
	}

	return claims, nil
}

// errMalformedJWT is returned when a JWT does not have the expected structure.
var errMalformedJWT = errors.New("malformed JWT: expected header.payload.signature")

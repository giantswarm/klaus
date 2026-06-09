package kagentapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
)

type authContextKey struct{}

// AuthInfo carries the caller's identity forwarded from the inbound A2A request.
// In-cluster calls fall back to the pod's service-account token when BearerToken is empty.
type AuthInfo struct {
	BearerToken string
	UserSub     string
}

// WithAuthInfo stores info in ctx.
func WithAuthInfo(ctx context.Context, info AuthInfo) context.Context {
	return context.WithValue(ctx, authContextKey{}, info)
}

// AuthInfoFromContext retrieves AuthInfo from ctx. Returns zero value if absent.
func AuthInfoFromContext(ctx context.Context) AuthInfo {
	v, _ := ctx.Value(authContextKey{}).(AuthInfo)
	return v
}

// ParseJWTSub decodes a JWT without signature verification and returns the
// "sub" claim. Returns "" when the token is not a valid three-part JWT or has no sub.
func ParseJWTSub(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	sub, _ := claims["sub"].(string)
	return sub
}

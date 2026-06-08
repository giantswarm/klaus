package server

import (
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/a2asrv"

	a2apkg "github.com/giantswarm/klaus/pkg/a2a"
	"github.com/giantswarm/klaus/pkg/kagentapi"
)

// registerA2ARoutes mounts the A2A JSONRPC endpoint and agent-card discovery
// URLs on mux. executor may be nil; when nil, this is a no-op. Returns the
// protected JSON-RPC handler so callers can wire it into the root path handler
// (kagent constructs agent URLs as http://{name}.{ns}:8080 with no path).
//
// Routes:
//   - /a2a and /a2a/    — A2A JSON-RPC handler (for clients that include the path)
//   - /.well-known/agent.json       — public agent card (unauthenticated)
//   - /.well-known/agent-card.json  — alias for the above
func registerA2ARoutes(mux *http.ServeMux, executor *a2apkg.Executor, protectedMW func(http.Handler) http.Handler) http.Handler {
	if executor == nil {
		return nil
	}

	card := a2apkg.AgentCard()
	cardHandler := a2asrv.NewStaticAgentCardHandler(card)
	mux.Handle("/.well-known/agent.json", cardHandler)
	mux.Handle("/.well-known/agent-card.json", cardHandler)

	requestHandler := a2asrv.NewHandler(executor)
	jsonRPCHandler := a2asrv.NewJSONRPCHandler(requestHandler)
	// extractCallerAuth sits inside protectedMW so the Authorization header
	// is still present when we read it (the owner middleware does not strip it).
	protected := protectedMW(extractCallerAuth(jsonRPCHandler))
	mux.Handle("/a2a", protected)
	mux.Handle("/a2a/", protected)
	return protected
}

// extractCallerAuth reads the OAuth2-proxy-set Authorization header from
// incoming A2A requests, parses the JWT sub without verification, and stores
// the token + sub in the request context. The kagent controller (trusted-proxy
// mode) requires both when pushing session events; this middleware makes those
// values available to PushEvent via context.
func extractCallerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && token != "" {
			if sub := kagentapi.ParseJWTSub(token); sub != "" {
				ctx := kagentapi.WithAuthInfo(r.Context(), kagentapi.AuthInfo{
					BearerToken: token,
					UserSub:     sub,
				})
				r = r.WithContext(ctx)
			}
		}
		next.ServeHTTP(w, r)
	})
}

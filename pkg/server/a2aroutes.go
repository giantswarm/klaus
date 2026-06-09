package server

import (
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	a2apkg "github.com/giantswarm/klaus/pkg/a2a"
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
	cardHandler := a2asrv.NewAgentCardHandler(a2av0.NewStaticAgentCardProducer(card))
	mux.Handle("/.well-known/agent.json", cardHandler)
	mux.Handle("/.well-known/agent-card.json", cardHandler)

	requestHandler := a2asrv.NewHandler(executor)
	// a2av0.NewJSONRPCHandler accepts the A2A v0.3 JSON-RPC method names
	// ("message/send", "message/stream") that kagent-controller sends, and
	// translates the v0.3 request body shape to the v1.0 types the executor expects.
	jsonRPCHandler := a2av0.NewJSONRPCHandler(requestHandler)
	protected := protectedMW(jsonRPCHandler)
	mux.Handle("/a2a", protected)
	mux.Handle("/a2a/", protected)
	return protected
}

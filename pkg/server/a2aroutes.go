package server

import (
	"net/http"

	"github.com/a2aproject/a2a-go/a2asrv"

	a2apkg "github.com/giantswarm/klaus/pkg/a2a"
)

// registerA2ARoutes mounts the A2A JSONRPC endpoint and agent-card discovery
// URLs on mux. executor may be nil; when nil, this is a no-op.
//
// Routes:
//   - /a2a and /a2a/   — A2A JSON-RPC handler (wrapped by protectedMW)
//   - /.well-known/agent.json       — public agent card (unauthenticated)
//   - /.well-known/agent-card.json  — alias for the above
func registerA2ARoutes(mux *http.ServeMux, executor *a2apkg.Executor, protectedMW func(http.Handler) http.Handler) {
	if executor == nil {
		return
	}

	card := a2apkg.AgentCard()
	cardHandler := a2asrv.NewStaticAgentCardHandler(card)
	mux.Handle("/.well-known/agent.json", cardHandler)
	mux.Handle("/.well-known/agent-card.json", cardHandler)

	requestHandler := a2asrv.NewHandler(executor)
	jsonRPCHandler := a2asrv.NewJSONRPCHandler(requestHandler)
	mux.Handle("/a2a", protectedMW(jsonRPCHandler))
	mux.Handle("/a2a/", protectedMW(jsonRPCHandler))
}

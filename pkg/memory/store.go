// Package memory provides per-user cross-session recall for Klaus.
// On each A2A turn the executor retrieves relevant chunks from previous
// conversations and injects them into the system prompt; after the turn it
// records both sides of the exchange attributed to the caller's identity.
//
// The identity key is the JWT sub (UserSub), never the contextID — so memory
// survives across sessions that mint a fresh contextID on every conversation.
//
// Required env for kagent backend:
//   - MEMORY_ENABLED=true
//   - KAGENT_API_ENDPOINT       — already required for kagent push
//   - KAGENT_AGENT_REF          — already required for kagent push
//   - KLAUS_EMBEDDING_ENDPOINT  — OpenAI-compatible embedding base URL
//   - KLAUS_EMBEDDING_MODEL     — embedding model name
//   - KLAUS_EMBEDDING_API_KEY   — API key (omit for unauthenticated endpoints)
package memory

import "context"

// Chunk is a single memory fragment retrieved for a query.
type Chunk struct {
	Content string
	Score   float32
}

// Store retrieves and records per-user conversation memory across sessions.
// Implementations must be safe for concurrent use.
type Store interface {
	// Retrieve returns the top-K most relevant Chunks for userID.
	Retrieve(ctx context.Context, userID, query string, topK int) ([]Chunk, error)
	// Record persists one turn utterance attributed to userID.
	Record(ctx context.Context, userID, role, content string) error
}

// NoOp is a Store that silently discards all writes and returns empty results.
type NoOp struct{}

var _ Store = NoOp{}

func (NoOp) Retrieve(_ context.Context, _, _ string, _ int) ([]Chunk, error) { return nil, nil }
func (NoOp) Record(_ context.Context, _, _, _ string) error                  { return nil }

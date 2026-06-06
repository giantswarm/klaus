// Package memory defines the interface for retrieving and storing conversation
// memory chunks across sessions. Before each turn, relevant chunks are prepended
// to the user prompt; after each turn, user and assistant text are stored for
// future recall.
//
// Implementations:
//   - NoOp: discard all writes, return empty results (default when env unset)
//   - KagentClient: kagent pgvector API (POST /api/memories/sessions and
//     POST /api/memories/search). Caller generates 768-dim float32 vectors
//     via an OpenAI-compatible embedding endpoint (see pkg/memory/embedding.go).
//
// Required env for kagent backend:
//   - KAGENT_MEMORY_ENDPOINT — kagent controller base URL
//   - KLAUD_EMBEDDING_ENDPOINT — OpenAI-compatible embedding base URL (default: https://api.openai.com/v1)
//   - KLAUD_EMBEDDING_MODEL — embedding model name (e.g. text-embedding-3-small)
//   - KLAUD_EMBEDDING_API_KEY — API key (omit for unauthenticated vLLM endpoints)
package memory

import (
	"context"
)

// Chunk is a single memory fragment retrieved for a query.
type Chunk struct {
	// Content is the recalled text to inject into the system prompt.
	Content string
	// Score is the cosine similarity score (0–1). Higher is more relevant.
	Score float32
}

// Client retrieves and stores conversation memory for a context.
// All implementations must be safe for concurrent use.
type Client interface {
	// Retrieve returns the top-N most relevant memory chunks for the given
	// query within the specified conversation context.
	Retrieve(ctx context.Context, contextID string, query string, topN int) ([]Chunk, error)

	// Store persists a new memory entry for future retrieval.
	Store(ctx context.Context, contextID string, role string, content string) error
}

// NoOp is a Client that discards all writes and returns empty results.
// It is the default when KAGENT_MEMORY_ENDPOINT is unset.
type NoOp struct{}

var _ Client = NoOp{}

func (NoOp) Retrieve(_ context.Context, _ string, _ string, _ int) ([]Chunk, error) {
	return nil, nil
}

func (NoOp) Store(_ context.Context, _ string, _ string, _ string) error {
	return nil
}

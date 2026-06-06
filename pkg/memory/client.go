// Package memory defines the interface for retrieving and storing conversation
// memory chunks. The memory layer injects relevant past context into the system
// prompt before each claude invocation and persists new turns for future recall.
//
// The default implementation is a no-op; the kagent-backed implementation
// (using POST /api/memories/sessions and POST /api/memories/search) is a
// separate follow-up once the embedding model used by kagent is confirmed.
//
// Open item: kagent requires exactly 768-dimensional float32 embeddings.
// The embedding model must match what kagent's ADK uses, otherwise cosine
// search returns garbage. Do not implement the kagent client until the
// embedding model is verified (check kagent source or klausctl embed helper).
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

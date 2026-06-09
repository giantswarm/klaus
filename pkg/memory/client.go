package memory

import "context"

// Chunk is a single memory fragment.
type Chunk struct {
	Content string
	Score   float64
}

// Client retrieves and stores conversation memory. All implementations must
// be safe for concurrent use.
type Client interface {
	Retrieve(ctx context.Context, contextID string, query string, topN int) ([]Chunk, error)
	Store(ctx context.Context, contextID string, role string, content string) error
}

// NoOp is a Client that discards all writes and returns empty results.
type NoOp struct{}

var _ Client = NoOp{}

func (NoOp) Retrieve(_ context.Context, _ string, _ string, _ int) ([]Chunk, error) {
	return nil, nil
}

func (NoOp) Store(_ context.Context, _ string, _ string, _ string) error {
	return nil
}

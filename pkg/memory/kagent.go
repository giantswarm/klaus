package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// KagentStore implements Store against the kagent pgvector memory API.
// All vectors are generated locally by embeddingClient — the kagent controller
// does not embed; it stores and searches raw float32 vectors supplied by the caller.
//
// API:
//
//	POST /api/memories/sessions — store a content+vector pair
//	POST /api/memories/search   — vector similarity search
//
// Both endpoints require X-User-ID for internal-trust auth.
type KagentStore struct {
	endpoint   string
	agentName  string
	embedder   *embeddingClient
	httpClient *http.Client
	log        *slog.Logger
}

var _ Store = (*KagentStore)(nil)

func newKagentStore(endpoint, agentName string, embedder *embeddingClient) *KagentStore {
	return &KagentStore{
		endpoint:   endpoint,
		agentName:  agentName,
		embedder:   embedder,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		log:        slog.Default().With("component", "memory.kagent"),
	}
}

type kagentAddRequest struct {
	AgentName string    `json:"agent_name"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	Vector    []float32 `json:"vector"`
	TTLDays   int       `json:"ttl_days,omitempty"`
}

type kagentSearchRequest struct {
	AgentName string    `json:"agent_name"`
	UserID    string    `json:"user_id"`
	Vector    []float32 `json:"vector"`
	Limit     int       `json:"limit"`
	MinScore  float64   `json:"min_score"`
}

type kagentSearchResult struct {
	ID      string  `json:"id"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// Retrieve searches the kagent memory store for the top-K most relevant chunks
// for the given userID and query.
func (s *KagentStore) Retrieve(ctx context.Context, userID, query string, topK int) ([]Chunk, error) {
	vector, err := s.embedder.embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	body, err := json.Marshal(kagentSearchRequest{
		AgentName: s.agentName,
		UserID:    userID,
		Vector:    vector,
		Limit:     topK,
		MinScore:  0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal search request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/api/memories/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("memory search returned HTTP %d", resp.StatusCode)
	}

	var items []kagentSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	chunks := make([]Chunk, 0, len(items))
	for _, item := range items {
		chunks = append(chunks, Chunk{Content: item.Content, Score: float32(item.Score)})
	}
	return chunks, nil
}

// Record persists a turn utterance to the kagent memory store.
// The role is prepended to content to preserve speaker attribution.
func (s *KagentStore) Record(ctx context.Context, userID, role, content string) error {
	text := role + ": " + content
	vector, err := s.embedder.embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embed content: %w", err)
	}

	body, err := json.Marshal(kagentAddRequest{
		AgentName: s.agentName,
		UserID:    userID,
		Content:   text,
		Vector:    vector,
	})
	if err != nil {
		return fmt.Errorf("marshal store request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/api/memories/sessions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build store request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("store request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("memory store returned HTTP %d", resp.StatusCode)
	}
	return nil
}

package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// KagentClient implements Client against the kagent pgvector memory API.
// All vectors are generated locally by embeddingClient — the kagent controller
// does not embed; it stores and searches raw float32 vectors supplied by the caller.
//
// API (internal, undocumented, may change between kagent minors):
//
//	POST /api/memories/sessions — store a content+vector pair
//	POST /api/memories/search   — vector similarity search
//
// Both endpoints require X-User-ID: <any non-empty value> for internal-trust auth.
type KagentClient struct {
	endpoint   string
	agentName  string
	userID     string
	embedder   *embeddingClient
	httpClient *http.Client
	log        *slog.Logger
}

var _ Client = (*KagentClient)(nil)

func newKagentClientFromEnv(endpoint string) *KagentClient {
	return &KagentClient{
		endpoint:   endpoint,
		agentName:  kagentEnvOrDefault("KAGENT_MEMORY_AGENT_NAME", "klaus"),
		userID:     kagentEnvOrDefault("KAGENT_MEMORY_USER_ID", "default"),
		embedder:   newEmbeddingClientFromEnv(),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		log:        slog.Default().With("component", "memory.kagent"),
	}
}

func kagentEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// effectiveUserID returns the user ID from ctx when set (via WithUserID),
// falling back to the static fallback configured at construction time.
func effectiveUserID(ctx context.Context, fallback string) string {
	if v := UserIDFromContext(ctx); v != "" {
		return v
	}
	return fallback
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

// Retrieve searches the kagent memory store for the top-N most relevant chunks
// for the given query. The contextID is not forwarded to kagent (which uses
// agentName+userID as its own scoping keys). The userID is taken from ctx
// (set by memory.WithUserID) when present, falling back to KAGENT_MEMORY_USER_ID.
func (c *KagentClient) Retrieve(ctx context.Context, _ string, query string, topN int) ([]Chunk, error) {
	userID := effectiveUserID(ctx, c.userID)
	vector, err := c.embedder.embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	body, err := json.Marshal(kagentSearchRequest{
		AgentName: c.agentName,
		UserID:    userID,
		Vector:    vector,
		Limit:     topN,
		MinScore:  0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal search request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/memories/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)

	resp, err := c.httpClient.Do(req)
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

// Store persists content to the kagent memory store asynchronously.
// The role parameter is prepended to content to preserve speaker attribution.
func (c *KagentClient) Store(ctx context.Context, _ string, role string, content string) error {
	userID := effectiveUserID(ctx, c.userID)
	text := role + ": " + content
	vector, err := c.embedder.embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embed content: %w", err)
	}

	body, err := json.Marshal(kagentAddRequest{
		AgentName: c.agentName,
		UserID:    userID,
		Content:   text,
		Vector:    vector,
	})
	if err != nil {
		return fmt.Errorf("marshal store request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/memories/sessions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build store request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("store request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("memory store returned HTTP %d", resp.StatusCode)
	}
	return nil
}

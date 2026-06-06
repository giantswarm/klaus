package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"time"
)

const embeddingDim = 768

// embeddingClient calls an OpenAI-compatible embedding endpoint.
// Compatible with OpenAI, vLLM, Ollama (via its /v1 proxy), and any other
// OpenAI-spec embedding server. Returns exactly 768-dim float32 vectors
// (truncated if the model produces more, with L2 re-normalization).
type embeddingClient struct {
	endpoint   string
	model      string
	apiKey     string
	httpClient *http.Client
}

func newEmbeddingClientFromEnv() *embeddingClient {
	endpoint := os.Getenv("KLAUD_EMBEDDING_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	return &embeddingClient{
		endpoint:   endpoint,
		model:      os.Getenv("KLAUD_EMBEDDING_MODEL"),
		apiKey:     os.Getenv("KLAUD_EMBEDDING_API_KEY"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *embeddingClient) enabled() bool {
	return c.model != ""
}

func (c *embeddingClient) embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]any{
		"model": c.model,
		"input": []string{text},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("embedding API returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response from %s", c.endpoint)
	}

	return truncateAndNormalize(result.Data[0].Embedding, embeddingDim), nil
}

// truncateAndNormalize truncates vec to dim elements and re-applies L2 normalization.
// Truncation without re-normalization produces a vector that is no longer unit-length,
// which degrades cosine similarity scores in the vector store.
func truncateAndNormalize(vec []float32, dim int) []float32 {
	if len(vec) > dim {
		vec = vec[:dim]
	}
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	norm := float32(math.Sqrt(sum))
	if norm == 0 {
		return vec
	}
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = v / norm
	}
	return out
}

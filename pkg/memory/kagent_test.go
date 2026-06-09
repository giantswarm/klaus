package memory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKagentStore_Retrieve(t *testing.T) {
	embSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/embeddings", r.URL.Path)
		vec := make([]float32, embeddingDim)
		vec[0] = 1.0
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vec}},
		})
	}))
	defer embSrv.Close()

	results := []kagentSearchResult{
		{ID: "1", Content: "user: hello from last session", Score: 0.9},
		{ID: "2", Content: "assistant: hi there", Score: 0.8},
	}
	memSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/memories/search", r.URL.Path)
		require.Equal(t, "alice", r.Header.Get("X-User-ID"))

		var req kagentSearchRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "alice", req.UserID)
		require.Equal(t, "myagent", req.AgentName)
		require.Equal(t, 5, req.Limit)
		require.Len(t, req.Vector, embeddingDim)

		_ = json.NewEncoder(w).Encode(results)
	}))
	defer memSrv.Close()

	embedder := &embeddingClient{
		endpoint:   embSrv.URL,
		model:      "test-model",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	store := newKagentStore(memSrv.URL, "myagent", embedder)

	chunks, err := store.Retrieve(t.Context(), "alice", "hello", 5)
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, "user: hello from last session", chunks[0].Content)
	require.InDelta(t, 0.9, chunks[0].Score, 0.01)
}

func TestKagentStore_Record(t *testing.T) {
	embSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vec := make([]float32, embeddingDim)
		vec[0] = 1.0
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vec}},
		})
	}))
	defer embSrv.Close()

	var recorded kagentAddRequest
	memSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/memories/sessions", r.URL.Path)
		require.Equal(t, "bob", r.Header.Get("X-User-ID"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&recorded))
		w.WriteHeader(http.StatusCreated)
	}))
	defer memSrv.Close()

	embedder := &embeddingClient{
		endpoint:   embSrv.URL,
		model:      "test-model",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	store := newKagentStore(memSrv.URL, "myagent", embedder)

	err := store.Record(t.Context(), "bob", "user", "what is the capital of France?")
	require.NoError(t, err)
	require.Equal(t, "bob", recorded.UserID)
	require.Equal(t, "myagent", recorded.AgentName)
	require.Equal(t, "user: what is the capital of France?", recorded.Content)
	require.Len(t, recorded.Vector, embeddingDim)
}

func TestKagentStore_SearchError(t *testing.T) {
	embSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vec := make([]float32, embeddingDim)
		vec[0] = 1.0
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vec}},
		})
	}))
	defer embSrv.Close()

	memSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer memSrv.Close()

	embedder := &embeddingClient{
		endpoint:   embSrv.URL,
		model:      "test-model",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	store := newKagentStore(memSrv.URL, "myagent", embedder)

	_, err := store.Retrieve(t.Context(), "alice", "hello", 5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
}

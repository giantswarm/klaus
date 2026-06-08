package memory_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/klaus/pkg/memory"
)

// TestNew_NoOp verifies that factory.New returns a NoOp when the required env
// vars are absent, without attempting any network calls.
func TestNew_NoOp(t *testing.T) {
	t.Run("no endpoint", func(t *testing.T) {
		t.Setenv("KAGENT_MEMORY_ENDPOINT", "")
		t.Setenv("KLAUS_EMBEDDING_MODEL", "text-embedding-3-small")
		c := memory.New()
		_, ok := c.(memory.NoOp)
		assert.True(t, ok, "expected NoOp when KAGENT_MEMORY_ENDPOINT is unset")
	})

	t.Run("no embedding model", func(t *testing.T) {
		t.Setenv("KAGENT_MEMORY_ENDPOINT", "http://localhost:9999")
		t.Setenv("KLAUS_EMBEDDING_MODEL", "")
		c := memory.New()
		_, ok := c.(memory.NoOp)
		assert.True(t, ok, "expected NoOp when KLAUS_EMBEDDING_MODEL is unset")
	})

	t.Run("both set returns non-NoOp", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		t.Setenv("KAGENT_MEMORY_ENDPOINT", srv.URL)
		t.Setenv("KLAUS_EMBEDDING_MODEL", "text-embedding-3-small")
		c := memory.New()
		_, ok := c.(memory.NoOp)
		assert.False(t, ok, "expected non-NoOp when both vars are set")
	})
}

// TestKagentClient_RetrieveStore exercises the full HTTP path of KagentClient
// using an httptest server for both the embedding endpoint and the kagent API.
func TestKagentClient_RetrieveStore(t *testing.T) {
	// Embedding server: returns a 768-dim unit vector.
	embeddingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		vec := make([]float32, 768)
		vec[0] = 1.0
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": vec},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(embeddingSrv.Close)

	// kagent API server.
	var searchBody, storeBody []byte
	var xUserIDOnSearch, xUserIDOnStore string
	kagentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/memories/search":
			xUserIDOnSearch = r.Header.Get("X-User-ID")
			json.NewDecoder(r.Body).Decode(&searchBody) //nolint:errcheck,gosec
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "a1", "content": "remembered text", "score": 0.85},
			})
		case "/api/memories/sessions":
			xUserIDOnStore = r.Header.Get("X-User-ID")
			json.NewDecoder(r.Body).Decode(&storeBody) //nolint:errcheck,gosec
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(kagentSrv.Close)

	t.Setenv("KAGENT_MEMORY_ENDPOINT", kagentSrv.URL)
	t.Setenv("KAGENT_MEMORY_USER_ID", "test-user")
	t.Setenv("KAGENT_MEMORY_AGENT_NAME", "test-agent")
	t.Setenv("KLAUS_EMBEDDING_ENDPOINT", embeddingSrv.URL+"/v1")
	t.Setenv("KLAUS_EMBEDDING_MODEL", "test-model")
	t.Setenv("KLAUS_EMBEDDING_API_KEY", "")

	client := memory.New()
	_, isNoOp := client.(memory.NoOp)
	require.False(t, isNoOp)

	t.Run("Retrieve returns chunks", func(t *testing.T) {
		chunks, err := client.Retrieve(t.Context(), "ctx-1", "what did we discuss?", 5)
		require.NoError(t, err)
		require.Len(t, chunks, 1)
		assert.Equal(t, "remembered text", chunks[0].Content)
		assert.InDelta(t, 0.85, chunks[0].Score, 1e-5)
		assert.Equal(t, "test-user", xUserIDOnSearch, "X-User-ID must use configured user ID, not hardcoded")
	})

	t.Run("Store sends content to kagent", func(t *testing.T) {
		err := client.Store(t.Context(), "ctx-1", "user", "hello world")
		require.NoError(t, err)
		assert.Equal(t, "test-user", xUserIDOnStore, "X-User-ID must use configured user ID, not hardcoded")
	})
}

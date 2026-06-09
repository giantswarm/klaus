package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/klaus/pkg/a2a"
	"github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/memory"
)

// newTestExecutor returns a minimal *a2a.Executor for route tests.
func newTestExecutor() *a2a.Executor {
	return a2a.New(
		&mockPrompter{status: claude.StatusInfo{Status: claude.ProcessStatusIdle}},
		a2a.ModeChat,
		memory.NoOp{},
	)
}

func TestRegisterA2ARoutes_NilExecutor(t *testing.T) {
	mux := http.NewServeMux()
	registerA2ARoutes(mux, nil, func(h http.Handler) http.Handler { return h })

	for _, path := range []string{"/.well-known/agent.json", "/.well-known/agent-card.json", "/a2a"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		// With nil executor, routes are not registered — mux returns 404.
		assert.Equal(t, http.StatusNotFound, w.Result().StatusCode, "path %s", path)
	}
}

func TestRegisterA2ARoutes_AgentCard(t *testing.T) {
	mux := http.NewServeMux()
	registerA2ARoutes(mux, newTestExecutor(), func(h http.Handler) http.Handler { return h })

	for _, path := range []string{"/.well-known/agent.json", "/.well-known/agent-card.json"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			resp := w.Result()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			// Card must be valid JSON.
			var card map[string]any
			err := json.NewDecoder(w.Body).Decode(&card)
			require.NoError(t, err)
			// a2a-go agent card spec requires "name" and "url" fields.
			assert.NotEmpty(t, card["name"], "agent card missing 'name'")
		})
	}
}

func TestRegisterA2ARoutes_ProtectedMW(t *testing.T) {
	mux := http.NewServeMux()
	blocked := false
	blockingMW := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			blocked = true
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
	registerA2ARoutes(mux, newTestExecutor(), blockingMW)

	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.True(t, blocked, "middleware should have been called for /a2a")
	assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
}

func TestRegisterA2ARoutes_CardIsUnauthenticated(t *testing.T) {
	mux := http.NewServeMux()
	// Middleware that rejects everything.
	rejectMW := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
	registerA2ARoutes(mux, newTestExecutor(), rejectMW)

	// Card routes are NOT wrapped by the middleware.
	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent.json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Result().StatusCode, "agent card should be publicly accessible")
}

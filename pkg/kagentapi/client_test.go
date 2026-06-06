package kagentapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/klaus/pkg/kagentapi"
)

func TestClient_Disabled(t *testing.T) {
	c := kagentapi.New("")
	assert.False(t, c.Enabled())
	// Must not panic or make any requests.
	c.PushEvent(t.Context(), "sess-1", kagentapi.SessionEvent{Role: "user", Content: "hello"})
}

func TestClient_PushEvent(t *testing.T) {
	var received []kagentapi.SessionEvent
	var gotSessionID string
	var gotUserID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Expect POST /api/sessions/{id}/events.
		gotSessionID = r.URL.Path
		gotUserID = r.Header.Get("X-User-ID")

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var event kagentapi.SessionEvent
		require.NoError(t, json.Unmarshal(body, &event))
		received = append(received, event)

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := kagentapi.New(srv.URL)
	require.True(t, c.Enabled())

	c.PushEvent(t.Context(), "sess-abc", kagentapi.SessionEvent{Role: "user", Content: "what is 2+2?"})

	// PushEvent is synchronous (the goroutine in the real path is for
	// the executor, not the client itself).
	require.Len(t, received, 1)
	assert.Equal(t, "user", received[0].Role)
	assert.Equal(t, "what is 2+2?", received[0].Content)
	assert.Equal(t, "/api/sessions/sess-abc/events", gotSessionID)
	assert.Equal(t, "klaus", gotUserID)
}

func TestClient_PushEvent_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := kagentapi.New(srv.URL)
	// Should not panic or return error; failure is logged only.
	c.PushEvent(t.Context(), "sess-xyz", kagentapi.SessionEvent{Role: "assistant", Content: "reply"})
}

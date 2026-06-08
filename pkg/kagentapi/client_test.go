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
	c.PushEvent(t.Context(), "sess-1", kagentapi.NewSessionEvent("id-1", "user", "hello"))
}

func TestClient_PushEvent(t *testing.T) {
	var received []kagentapi.SessionEvent
	var gotSessionID string
	var gotAuthHeader string
	var gotUserID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSessionID = r.URL.Path
		gotAuthHeader = r.Header.Get("Authorization")
		gotUserID = r.Header.Get("X-User-Id")

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

	// Inject auth via context.
	ctx := kagentapi.WithAuthInfo(t.Context(), kagentapi.AuthInfo{
		BearerToken: "tok123",
		UserSub:     "user@example.com",
	})

	event := kagentapi.NewSessionEvent("msg-1", "user", "what is 2+2?")
	c.PushEvent(ctx, "sess-abc", event)

	require.Len(t, received, 1)
	assert.Equal(t, event.ID, received[0].ID)
	assert.Equal(t, event.Data, received[0].Data)
	assert.Equal(t, "/api/sessions/sess-abc/events", gotSessionID)
	assert.Equal(t, "Bearer tok123", gotAuthHeader)
	assert.Equal(t, "user@example.com", gotUserID)
}

func TestClient_PushEvent_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := kagentapi.New(srv.URL)
	// Should not panic or return error; failure is logged only.
	c.PushEvent(t.Context(), "sess-xyz", kagentapi.NewSessionEvent("msg-2", "agent", "reply"))
}

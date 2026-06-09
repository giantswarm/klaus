package kagentapi_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/klaus/pkg/kagentapi"
)

func TestDisabled(t *testing.T) {
	client := kagentapi.New("", "")
	require.False(t, client.Enabled())

	// All operations must be no-ops (would panic/error if they attempted HTTP).
	client.PushEvent(t.Context(), "ctx1", kagentapi.NewSessionEvent("id1", "user", "hello"), kagentapi.AuthInfo{})
	client.StoreTask(t.Context(), "task1", "ctx1", "hello", "world", "completed", nil, kagentapi.AuthInfo{})
}

func TestPushEvent_URL(t *testing.T) {
	contextID := "ctx-abc/def" // contains slash — must be path-escaped
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := kagentapi.New(srv.URL, "")
	client.PushEvent(t.Context(), contextID, kagentapi.NewSessionEvent("id1", "user", "hello"), kagentapi.AuthInfo{})

	want := "/api/sessions/" + url.PathEscape(contextID) + "/events"
	require.Equal(t, want, capturedURL)
}

func TestPushEvent_DoubleEncodedData(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &body))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	event := kagentapi.NewSessionEventWithMetadata("id1", "agent", "hi", map[string]any{
		"adk_usage_metadata": map[string]any{
			"totalTokenCount":      100,
			"promptTokenCount":     80,
			"candidatesTokenCount": 20,
		},
	})
	client := kagentapi.New(srv.URL, "")
	client.PushEvent(t.Context(), "ctx1", event, kagentapi.AuthInfo{})

	// data field must be a JSON string (double-encoded)
	dataStr, ok := body["data"].(string)
	require.True(t, ok, "data must be a JSON string, got %T", body["data"])

	var inner map[string]any
	require.NoError(t, json.Unmarshal([]byte(dataStr), &inner))
	require.Equal(t, "message", inner["kind"])
	require.Equal(t, "agent", inner["role"])

	meta, ok := inner["metadata"].(map[string]any)
	require.True(t, ok)
	usage, ok := meta["adk_usage_metadata"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 100, usage["totalTokenCount"])
	require.EqualValues(t, 80, usage["promptTokenCount"])
	require.EqualValues(t, 20, usage["candidatesTokenCount"])
}

func TestStoreTask_URL(t *testing.T) {
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := kagentapi.New(srv.URL, "")
	client.StoreTask(t.Context(), "task1", "ctx1", "hello", "world", "completed", nil, kagentapi.AuthInfo{})

	require.Equal(t, "/api/tasks", capturedURL)
}

func TestStoreTask_Body(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &body))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	agentMeta := map[string]any{
		"adk_usage_metadata": map[string]any{
			"totalTokenCount":      50,
			"promptTokenCount":     30,
			"candidatesTokenCount": 20,
		},
	}
	client := kagentapi.New(srv.URL, "")
	client.StoreTask(t.Context(), "task42", "ctx99", "user says hi", "agent says bye", "completed", agentMeta, kagentapi.AuthInfo{})

	require.Equal(t, "task", body["kind"])
	require.Equal(t, "task42", body["id"])
	require.Equal(t, "ctx99", body["contextId"])

	history, ok := body["history"].([]any)
	require.True(t, ok)
	require.Len(t, history, 2)

	userMsg, ok := history[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "user", userMsg["role"])

	agentMsg, ok := history[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "agent", agentMsg["role"])

	meta, ok := agentMsg["metadata"].(map[string]any)
	require.True(t, ok)
	_, hasUsage := meta["adk_usage_metadata"]
	require.True(t, hasUsage)

	// user message must not carry metadata
	_, hasUserMeta := userMsg["metadata"]
	require.False(t, hasUserMeta, "user message must not carry metadata")
}

func TestHeaders_Auth(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := kagentapi.New(srv.URL, "my-agent")
	auth := kagentapi.AuthInfo{BearerToken: "tok123", UserSub: "user@example.com"}
	client.PushEvent(t.Context(), "ctx1", kagentapi.NewSessionEvent("id1", "user", "hi"), auth)

	require.Equal(t, "application/json", gotHeaders.Get("Content-Type"))
	require.Equal(t, "Bearer tok123", gotHeaders.Get("Authorization"))
	require.Equal(t, "user@example.com", gotHeaders.Get("X-User-Id"))
	require.Equal(t, "my-agent", gotHeaders.Get("X-Agent-Name"))
}

func TestHeaders_NoUserSub_PlainToken(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Plain opaque token — ParseJWTSub returns ""; no X-User-Id is sent.
	client := kagentapi.New(srv.URL, "")
	client.PushEvent(t.Context(), "ctx1", kagentapi.NewSessionEvent("id1", "user", "hi"), kagentapi.AuthInfo{BearerToken: "tok"})

	require.Empty(t, gotHeaders.Get("X-User-Id"))
	require.Empty(t, gotHeaders.Get("X-Agent-Name"))
	require.Equal(t, "Bearer tok", gotHeaders.Get("Authorization"))
}

func TestHeaders_UserSubFallbackFromJWT(t *testing.T) {
	// Build a minimal unsigned JWT with sub="alice@example.com".
	// kagent requires X-User-Id; the client falls back to ParseJWTSub(token).
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"alice@example.com"}`))
	jwtToken := header + "." + payload + ".sig"

	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := kagentapi.New(srv.URL, "")
	client.PushEvent(t.Context(), "ctx1", kagentapi.NewSessionEvent("id1", "user", "hi"), kagentapi.AuthInfo{BearerToken: jwtToken})

	require.Equal(t, "alice@example.com", gotHeaders.Get("X-User-Id"))
	require.Equal(t, "Bearer "+jwtToken, gotHeaders.Get("Authorization"))
}

func TestPushEvent_ContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	kagentapi.New(srv.URL, "").PushEvent(t.Context(), "ctx", kagentapi.NewSessionEvent("id", "user", "x"), kagentapi.AuthInfo{})
}

func TestNewSessionEvent_NoMetadata(t *testing.T) {
	ev := kagentapi.NewSessionEvent("id1", "user", "hello")
	require.Equal(t, "id1", ev.ID)
	require.True(t, strings.Contains(ev.Data, `"role":"user"`))
	require.True(t, strings.Contains(ev.Data, `"hello"`))
	// No metadata key in data.
	require.False(t, strings.Contains(ev.Data, "metadata"))
}

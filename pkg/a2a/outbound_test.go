package a2a

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry_nil(t *testing.T) {
	r := NewRegistry(RegistryConfig{})
	require.Nil(t, r, "empty config should return nil registry")
}

func TestNewRegistry_targetsOnly(t *testing.T) {
	r := NewRegistry(RegistryConfig{
		Targets: map[string]string{"agent": "http://agent.svc"},
	})
	require.NotNil(t, r)
}

func TestNewRegistry_dynamicWithHosts(t *testing.T) {
	r := NewRegistry(RegistryConfig{
		AllowDynamic: true,
		AllowedHosts: []string{"agent.example.com"},
	})
	require.NotNil(t, r)
}

func TestRegistry_resolveURL_namedTarget(t *testing.T) {
	r := NewRegistry(RegistryConfig{
		Targets: map[string]string{"myagent": "http://myagent.svc"},
	})
	url, err := r.resolveURL("myagent")
	require.NoError(t, err)
	require.Equal(t, "http://myagent.svc", url)
}

func TestRegistry_resolveURL_unknownNameDynamicDisabled(t *testing.T) {
	r := NewRegistry(RegistryConfig{
		Targets: map[string]string{"myagent": "http://myagent.svc"},
	})
	_, err := r.resolveURL("other")
	require.Error(t, err)
	require.Contains(t, err.Error(), "dynamic calls disabled")
}

func TestRegistry_resolveURL_dynamicAllowedHost(t *testing.T) {
	r := NewRegistry(RegistryConfig{
		AllowDynamic: true,
		AllowedHosts: []string{"agent.example.com"},
	})
	url, err := r.resolveURL("http://agent.example.com/path")
	require.NoError(t, err)
	require.Equal(t, "http://agent.example.com/path", url)
}

func TestRegistry_resolveURL_dynamicBlockedHost(t *testing.T) {
	r := NewRegistry(RegistryConfig{
		AllowDynamic: true,
		AllowedHosts: []string{"allowed.example.com"},
	})
	_, err := r.resolveURL("http://blocked.example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in the allowed-hosts list")
}

func TestRegistry_resolveURL_dynamicNoAllowedHosts_failClosed(t *testing.T) {
	// AllowDynamic=true with an empty allowedHosts map must deny every host.
	// Config.Validate() prevents this at start-up; this tests the runtime guard.
	r := &Registry{
		allowDynamic: true,
		allowedHosts: map[string]struct{}{},
		targets:      map[string]string{},
		clients:      make(map[string]*a2aclient.Client),
	}
	_, err := r.resolveURL("http://any.example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in the allowed-hosts list")
}

func TestRegistry_resolveURL_invalidURL(t *testing.T) {
	r := NewRegistry(RegistryConfig{
		AllowDynamic: true,
		AllowedHosts: []string{"good.example.com"},
	})
	_, err := r.resolveURL("not-a-url")
	require.Error(t, err)
}

func TestTextFromResult_message(t *testing.T) {
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "hello"})
	got := textFromResult(msg)
	require.Equal(t, "hello", got)
}

func TestTextFromResult_taskStatusMessage(t *testing.T) {
	statusMsg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "done"})
	task := &a2a.Task{
		Status: a2a.TaskStatus{Message: statusMsg},
	}
	got := textFromResult(task)
	require.Equal(t, "done", got)
}

func TestTextFromResult_taskHistoryFallback(t *testing.T) {
	task := &a2a.Task{
		Status:  a2a.TaskStatus{},
		History: []*a2a.Message{a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "from history"})},
	}
	got := textFromResult(task)
	require.Equal(t, "from history", got)
}

func TestTextFromResult_nil(t *testing.T) {
	got := textFromResult(nil)
	require.Empty(t, got)
}

func TestRegistry_Call_integration(t *testing.T) {
	// Stand up a minimal A2A-compatible test server that serves an agent card
	// and responds to JSON-RPC message/send with a text message.
	var srv *httptest.Server

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-card.json", func(w http.ResponseWriter, _ *http.Request) {
		card := a2a.AgentCard{
			Name:               "test-agent",
			URL:                srv.URL,
			ProtocolVersion:    "0.3",
			PreferredTransport: a2a.TransportProtocolJSONRPC,
			AdditionalInterfaces: []a2a.AgentInterface{
				{URL: srv.URL, Transport: a2a.TransportProtocolJSONRPC},
			},
			Capabilities:       a2a.AgentCapabilities{Streaming: false},
			DefaultInputModes:  []string{"text/plain"},
			DefaultOutputModes: []string{"text/plain"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		reply := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "pong"})
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  reply,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	r := NewRegistry(RegistryConfig{
		Targets: map[string]string{"test-agent": srv.URL},
	})
	require.NotNil(t, r)

	result, err := r.Call(t.Context(), "test-agent", "ping")
	require.NoError(t, err)
	require.Equal(t, "pong", result)
}

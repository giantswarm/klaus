// Package kagentapi provides a best-effort client for the kagent controller's
// internal HTTP API. The API is undocumented and may change between kagent
// minors; all writes are async and failures are logged only, never fatal.
//
// Env: KAGENT_API_ENDPOINT — base URL (e.g. http://kagent-controller.kagent.svc).
// When unset, all operations are no-ops.
//
// Env: KAGENT_AGENT_REF — namespace/name of this agent as registered in kagent
// (e.g. "kagent/klaud-coding"). When set, sent as X-Agent-Name on push requests.
package kagentapi

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

const (
	defaultTimeout    = 5 * time.Second
	envKagentAgentRef = "KAGENT_AGENT_REF"
)

// a2aMessage is the JSON structure stored in Event.Data — a trpc-a2a-go
// protocol.Message. Defined here so pkg/kagentapi has no dependency on the
// trpc SDK.
type a2aMessage struct {
	Kind      string    `json:"kind"`
	MessageID string    `json:"messageId"`
	Role      string    `json:"role"`
	Parts     []a2aPart `json:"parts"`
}

type a2aPart struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// SessionEvent is a single turn event sent to the kagent session history API.
// POST /api/sessions/{id}/events
// Data is a JSON-serialized A2A protocol.Message (see NewSessionEvent).
type SessionEvent struct {
	ID   string `json:"id"`
	Data string `json:"data"`
}

// NewSessionEvent builds a SessionEvent for the given role ("user" or "agent")
// and plain-text content. id should be unique per event (e.g. a UUID).
func NewSessionEvent(id, role, text string) SessionEvent {
	msg := a2aMessage{
		Kind:      "message",
		MessageID: id,
		Role:      role,
		Parts:     []a2aPart{{Kind: "text", Text: text}},
	}
	data, _ := json.Marshal(msg)
	return SessionEvent{ID: id, Data: string(data)}
}

// Client pushes session turn events to the kagent UI. It is safe for
// concurrent use. When Endpoint is empty, all methods are no-ops.
type Client struct {
	endpoint   string
	httpClient *http.Client
	log        *slog.Logger
}

// New returns a Client. endpoint is the kagent controller base URL
// (KAGENT_API_ENDPOINT). When empty, the client is a no-op.
func New(endpoint string) *Client {
	return &Client{
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: defaultTimeout},
		log:        slog.Default().With("component", "kagentapi"),
	}
}

// Enabled reports whether the client has a configured endpoint.
func (c *Client) Enabled() bool {
	return c.endpoint != ""
}

// PushEvent sends a single session event to the kagent controller.
// It is a best-effort operation; any error is logged and discarded.
//
// Auth: the kagent controller runs in trusted-proxy mode and requires a Bearer
// JWT + the user sub via X-User-Id. Auth is read from ctx (set by the
// extractCallerAuth middleware in pkg/server). Without valid auth the push
// returns 401 and is silently discarded.
func (c *Client) PushEvent(ctx context.Context, sessionID string, event SessionEvent) {
	if !c.Enabled() {
		return
	}

	body, err := json.Marshal(event)
	if err != nil {
		c.log.ErrorContext(ctx, "kagentapi: marshal event", "err", err)
		return
	}

	url := fmt.Sprintf("%s/api/sessions/%s/events", c.endpoint, sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		c.log.ErrorContext(ctx, "kagentapi: build request", "err", err, "session_id", sessionID)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Forward the caller's OIDC identity so the kagent controller can locate
	// the session (which is owned by the user, not the agent pod).
	info := AuthInfoFromContext(ctx)
	if info.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+info.BearerToken)
	}
	if info.UserSub != "" {
		req.Header.Set("X-User-Id", info.UserSub)
	}
	if agentRef := os.Getenv(envKagentAgentRef); agentRef != "" {
		req.Header.Set("X-Agent-Name", agentRef)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.WarnContext(ctx, "kagentapi: push event failed", "err", err, "session_id", sessionID)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		c.log.WarnContext(ctx, "kagentapi: push event non-2xx", "status", resp.StatusCode, "session_id", sessionID)
	}
}

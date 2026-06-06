// Package kagentapi provides a best-effort client for the kagent controller's
// internal HTTP API. The API is undocumented and may change between kagent
// minors; all writes are async and failures are logged only, never fatal.
//
// Env: KAGENT_API_ENDPOINT — base URL (e.g. http://kagent-controller.kagent.svc).
// When unset, all operations are no-ops.
package kagentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const defaultTimeout = 5 * time.Second

// SessionEvent is a single turn event sent to the kagent session history API.
// POST /api/sessions/{id}/events
type SessionEvent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	// kagent controller uses internal-trust auth via X-User-ID.
	req.Header.Set("X-User-ID", "klaus")

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

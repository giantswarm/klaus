// Package kagentapi provides a best-effort REST client for pushing turn history
// and token usage to the kagent-controller after each A2A turn.
package kagentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultSATokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	saTokenCacheTTL    = 5 * time.Minute
	httpTimeout        = 10 * time.Second

	kindMessage = "message"
	kindText    = "text"
)

// Client pushes turn history and token usage to the kagent-controller REST API.
// All operations are best-effort: errors are logged and never returned.
// A zero-endpoint Client is valid and disables all operations.
type Client struct {
	endpoint    string
	agentRef    string
	httpClient  *http.Client
	saTokenPath string

	saTokenMu     sync.RWMutex
	saTokenCached string
	saTokenExpiry time.Time
}

// New returns a Client. When endpoint is empty the client is disabled and all
// methods are no-ops.
func New(endpoint, agentRef string) *Client {
	return &Client{
		endpoint:    endpoint,
		agentRef:    agentRef,
		httpClient:  &http.Client{Timeout: httpTimeout},
		saTokenPath: defaultSATokenPath,
	}
}

// Enabled returns true when the client has a non-empty endpoint.
func (c *Client) Enabled() bool {
	return c.endpoint != ""
}

// wire types matching the kagent-controller REST API -------------------------

type a2aMessage struct {
	Kind      string         `json:"kind"`
	MessageID string         `json:"messageId"`
	Role      string         `json:"role"`
	Parts     []a2aPart      `json:"parts"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type a2aPart struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type a2aTask struct {
	Kind      string        `json:"kind"`
	ID        string        `json:"id"`
	ContextID string        `json:"contextId"`
	Status    a2aTaskStatus `json:"status"`
	History   []a2aMessage  `json:"history,omitempty"`
}

type a2aTaskStatus struct {
	State     string `json:"state"`
	Timestamp string `json:"timestamp,omitempty"`
}

// SessionEvent is a single event posted to /api/sessions/{id}/events.
// Data is a double-encoded JSON string containing a serialised a2aMessage.
type SessionEvent struct {
	ID   string `json:"id"`
	Data string `json:"data"`
}

// NewSessionEvent builds a SessionEvent with no metadata.
func NewSessionEvent(id, role, text string) SessionEvent {
	return NewSessionEventWithMetadata(id, role, text, nil)
}

// NewSessionEventWithMetadata builds a SessionEvent with arbitrary metadata on
// the inner a2aMessage. metadata may be nil.
func NewSessionEventWithMetadata(id, role, text string, metadata map[string]any) SessionEvent {
	msg := a2aMessage{
		Kind:      kindMessage,
		MessageID: id,
		Role:      role,
		Parts:     []a2aPart{{Kind: kindText, Text: text}},
		Metadata:  metadata,
	}
	data, _ := json.Marshal(msg)
	return SessionEvent{ID: id, Data: string(data)}
}

// PushEvent posts a session event to /api/sessions/{contextID}/events.
// The call is best-effort; errors are logged and not returned.
func (c *Client) PushEvent(ctx context.Context, contextID string, event SessionEvent, auth AuthInfo) {
	if !c.Enabled() {
		return
	}
	endpoint := fmt.Sprintf("%s/api/sessions/%s/events", c.endpoint, url.PathEscape(contextID))
	c.post(ctx, endpoint, event, auth)
}

// StoreTask upserts a completed task at /api/tasks. The agent message carries
// agentMetadata (usage, etc.) when non-nil.
// The call is best-effort; errors are logged and not returned.
func (c *Client) StoreTask(ctx context.Context, taskID, contextID, userText, agentText, state string, agentMetadata map[string]any, auth AuthInfo) {
	if !c.Enabled() {
		return
	}
	task := a2aTask{
		Kind:      "task",
		ID:        taskID,
		ContextID: contextID,
		Status: a2aTaskStatus{
			State:     state,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		History: []a2aMessage{
			{
				Kind:      kindMessage,
				MessageID: taskID + "-user",
				Role:      "user",
				Parts:     []a2aPart{{Kind: kindText, Text: userText}},
			},
			{
				Kind:      kindMessage,
				MessageID: taskID + "-agent",
				Role:      "agent",
				Parts:     []a2aPart{{Kind: kindText, Text: agentText}},
				Metadata:  agentMetadata,
			},
		},
	}
	endpoint := fmt.Sprintf("%s/api/tasks", c.endpoint)
	c.post(ctx, endpoint, task, auth)
}

// post marshals body as JSON and POSTs it to endpoint. Errors are logged.
func (c *Client) post(ctx context.Context, endpoint string, body any, auth AuthInfo) {
	data, err := json.Marshal(body)
	if err != nil {
		slog.WarnContext(ctx, "kagentapi: marshal failed", "url", endpoint, "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		slog.WarnContext(ctx, "kagentapi: build request failed", "url", endpoint, "error", err)
		return
	}
	c.setHeaders(req, auth)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "kagentapi: request failed", "url", endpoint, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		slog.WarnContext(ctx, "kagentapi: unexpected status", "url", endpoint, "status", resp.StatusCode)
	}
}

// setHeaders attaches auth and content-type headers to req.
func (c *Client) setHeaders(req *http.Request, auth AuthInfo) {
	req.Header.Set("Content-Type", "application/json")
	token := auth.BearerToken
	if token == "" {
		token = c.readServiceAccountToken()
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	userSub := auth.UserSub
	if userSub == "" {
		// kagent /api/sessions/.../events requires X-User-Id.
		// Fall back to the sub of whichever bearer token we are sending.
		userSub = ParseJWTSub(token)
	}
	if userSub != "" {
		req.Header.Set("X-User-Id", userSub)
	}
	if c.agentRef != "" {
		req.Header.Set("X-Agent-Name", c.agentRef)
	}
}

// readServiceAccountToken reads and caches the pod's SA token for up to
// saTokenCacheTTL. Returns "" on error.
func (c *Client) readServiceAccountToken() string {
	c.saTokenMu.RLock()
	if c.saTokenCached != "" && time.Now().Before(c.saTokenExpiry) {
		token := c.saTokenCached
		c.saTokenMu.RUnlock()
		return token
	}
	c.saTokenMu.RUnlock()

	c.saTokenMu.Lock()
	defer c.saTokenMu.Unlock()
	// Re-check under write lock.
	if c.saTokenCached != "" && time.Now().Before(c.saTokenExpiry) {
		return c.saTokenCached
	}
	data, err := os.ReadFile(c.saTokenPath)
	if err != nil {
		return ""
	}
	token := strings.TrimSpace(string(data))
	c.saTokenCached = token
	c.saTokenExpiry = time.Now().Add(saTokenCacheTTL)
	return token
}

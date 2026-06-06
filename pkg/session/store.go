// Package session provides multi-session state persistence for Klaus.
//
// A Klaus pod handles one active claude subprocess (one session) at a time, but
// over its lifetime the same pod may be reused across multiple A2A contexts via
// the warm-pool substrate. The Store tracks the contextID → sessionID binding and
// persists the full conversation history (one Turn per model exchange) so that:
//
//   - Resume across pod restarts is driven by the stored sessionID.
//   - Conversation history is queryable by the MCP messages tool and the kagent UI.
//
// Use NewStore to obtain a backend-selected implementation.
package session

import (
	"context"
	"encoding/json"
	"time"
)

// Turn represents one model exchange within a conversation.
type Turn struct {
	ContextID string
	SessionID string
	// Seq is the 1-based position of this turn within the context's history.
	Seq     int
	Role    string          // "user" | "assistant"
	Content json.RawMessage // arbitrary JSON (OpenAI message shape or raw text)
	TS      time.Time
}

// Store persists contextID → sessionID bindings and per-turn conversation history.
// All methods must be safe for concurrent use.
type Store interface {
	// SessionID returns the claude sessionID bound to contextID, or "" if none.
	SessionID(ctx context.Context, contextID string) (string, error)
	// BindSession records the contextID → sessionID mapping.
	BindSession(ctx context.Context, contextID, sessionID string) error
	// AppendTurn persists a conversation turn.
	AppendTurn(ctx context.Context, t Turn) error
	// History returns all turns for contextID in ascending sequence order.
	History(ctx context.Context, contextID string) ([]Turn, error)
}

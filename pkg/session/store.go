// Package session provides conversation history persistence for Klaus.
//
// The Store persists the full conversation history (one Turn per model exchange)
// so that history is queryable by the MCP messages tool and the kagent UI.
//
// Note: cross-restart resume requires both a persistent Store backend (Postgres
// via KLAUS_PGSQL_DSN) AND that the claude CLI session files (~/.claude/) survive
// the restart (mounted on a PVC). History alone is not sufficient; the CLI cannot
// resume a session whose files no longer exist.
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

// Store persists per-turn conversation history.
// All methods must be safe for concurrent use.
type Store interface {
	// AppendTurn persists a conversation turn.
	AppendTurn(ctx context.Context, t Turn) error
	// History returns all turns for contextID in ascending sequence order.
	History(ctx context.Context, contextID string) ([]Turn, error)
}

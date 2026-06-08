package session_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/klaus/pkg/session"
)

// testStore runs the full Store contract against any implementation.
func testStore(t *testing.T, store session.Store) {
	t.Helper()
	ctx := t.Context()

	const ctxID = "ctx-abc"
	const sessID = "sess-123"

	// No binding yet.
	got, err := store.SessionID(ctx, ctxID)
	require.NoError(t, err)
	require.Empty(t, got)

	// Bind and read back.
	require.NoError(t, store.BindSession(ctx, ctxID, sessID))
	got, err = store.SessionID(ctx, ctxID)
	require.NoError(t, err)
	require.Equal(t, sessID, got)

	// Re-bind (update).
	require.NoError(t, store.BindSession(ctx, ctxID, "sess-456"))
	got, err = store.SessionID(ctx, ctxID)
	require.NoError(t, err)
	require.Equal(t, "sess-456", got)

	// Append turns.
	turn1 := session.Turn{
		ContextID: ctxID,
		SessionID: sessID,
		Seq:       1,
		Role:      "user",
		Content:   json.RawMessage(`{"text":"hello"}`),
	}
	turn2 := session.Turn{
		ContextID: ctxID,
		SessionID: sessID,
		Seq:       2,
		Role:      "assistant",
		Content:   json.RawMessage(`{"text":"world"}`),
	}
	require.NoError(t, store.AppendTurn(ctx, turn1))
	require.NoError(t, store.AppendTurn(ctx, turn2))

	history, err := store.History(ctx, ctxID)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "user", history[0].Role)
	require.Equal(t, "assistant", history[1].Role)
	require.Equal(t, 1, history[0].Seq)
	require.Equal(t, 2, history[1].Seq)

	// Unknown context has empty history.
	history, err = store.History(ctx, "unknown")
	require.NoError(t, err)
	require.Empty(t, history)
}

func TestMemoryStore(t *testing.T) {
	testStore(t, session.NewMemoryStore())
}

func TestLocalStore(t *testing.T) {
	dir := t.TempDir()
	store, err := session.NewLocalStore(dir)
	require.NoError(t, err)
	testStore(t, store)
}

func TestLocalStore_AutoSeq(t *testing.T) {
	dir := t.TempDir()
	store, err := session.NewLocalStore(dir)
	require.NoError(t, err)
	ctx := t.Context()

	// Seq=0 means auto-assign.
	require.NoError(t, store.AppendTurn(ctx, session.Turn{
		ContextID: "ctx1", SessionID: "s1", Role: "user",
		Content: json.RawMessage(`"hi"`),
	}))
	require.NoError(t, store.AppendTurn(ctx, session.Turn{
		ContextID: "ctx1", SessionID: "s1", Role: "assistant",
		Content: json.RawMessage(`"hello"`),
	}))

	history, err := store.History(ctx, "ctx1")
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, 1, history[0].Seq)
	require.Equal(t, 2, history[1].Seq)
}

// TestPostgresStore runs the store contract against a real Postgres instance.
// It is skipped unless KLAUS_PGSQL_DSN is set.
func TestPostgresStore(t *testing.T) {
	dsn := os.Getenv("KLAUS_PGSQL_DSN")
	if dsn == "" {
		t.Skip("KLAUS_PGSQL_DSN not set, skipping Postgres store test")
	}

	store, err := session.NewPostgresStore(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	testStore(t, store)
}

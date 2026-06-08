package session

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver for database/sql
)

//go:embed schema.sql
var schemaDDL string

// PostgresStore is a Postgres-backed Store using the sessions schema.
// Obtain one via NewPostgresStore; the caller must close the DB when done.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore opens a connection pool to the Postgres DSN, applies the
// schema DDL idempotently, and returns a ready Store.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening postgres connection: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applying session schema: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

// Close releases the underlying connection pool.
func (p *PostgresStore) Close() error {
	return p.db.Close()
}

func (p *PostgresStore) AppendTurn(ctx context.Context, t Turn) error {
	if t.TS.IsZero() {
		t.TS = time.Now().UTC()
	}

	content := t.Content
	if len(content) == 0 {
		content = json.RawMessage(`null`)
	}

	if t.Seq != 0 {
		_, err := p.db.ExecContext(ctx, `
			INSERT INTO sessions.turns (context_id, session_id, seq, role, content, ts)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (context_id, seq) DO NOTHING
		`, t.ContextID, t.SessionID, t.Seq, t.Role, []byte(content), t.TS)
		if err != nil {
			return fmt.Errorf("inserting turn: %w", err)
		}
		return nil
	}

	// Auto-assign seq atomically to avoid a TOCTOU race under concurrent appends.
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO sessions.turns (context_id, session_id, seq, role, content, ts)
		SELECT $1, $2, COALESCE(MAX(seq), 0) + 1, $3, $4, $5
		FROM sessions.turns
		WHERE context_id = $1
	`, t.ContextID, t.SessionID, t.Role, []byte(content), t.TS)
	if err != nil {
		return fmt.Errorf("inserting turn: %w", err)
	}
	return nil
}

func (p *PostgresStore) History(ctx context.Context, contextID string) ([]Turn, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT context_id, session_id, seq, role, content, ts
		FROM sessions.turns
		WHERE context_id = $1
		ORDER BY seq ASC
	`, contextID)
	if err != nil {
		return nil, fmt.Errorf("querying turns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var turns []Turn
	for rows.Next() {
		var t Turn
		var content []byte
		if err := rows.Scan(&t.ContextID, &t.SessionID, &t.Seq, &t.Role, &content, &t.TS); err != nil {
			return nil, fmt.Errorf("scanning turn: %w", err)
		}
		t.Content = json.RawMessage(content)
		turns = append(turns, t)
	}
	return turns, rows.Err()
}

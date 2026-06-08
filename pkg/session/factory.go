package session

import (
	"context"
	"os"
	"path/filepath"
)

const (
	// BackendMemory is the in-memory store (tests only).
	BackendMemory = "memory"

	envDSN        = "KLAUS_PGSQL_DSN"
	envSessionDir = "KLAUS_SESSION_DIR"
)

// NewStore constructs a Store from environment variables.
//
//   - KLAUS_PGSQL_DSN: when set, use the Postgres backend. Fails fast if the DB
//     is unreachable.
//   - KLAUS_SESSION_DIR: base directory for the local backend (default
//     ~/.klaus/sessions). Ignored when KLAUS_PGSQL_DSN is set.
//
// When neither variable is set, the local file backend is used and falls back
// to os.TempDir if the directory cannot be created.
func NewStore(ctx context.Context) (Store, error) {
	if dsn := os.Getenv(envDSN); dsn != "" {
		return NewPostgresStore(ctx, dsn)
	}

	dir := os.Getenv(envSessionDir)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		dir = filepath.Join(home, ".klaus", "sessions")
	}
	return NewLocalStore(dir)
}

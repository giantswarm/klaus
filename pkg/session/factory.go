package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// BackendLocal is the default file-backed store under KLAUS_SESSION_DIR.
	BackendLocal = "local"
	// BackendPostgres selects the Postgres store; requires KLAUS_PG_DSN.
	BackendPostgres = "postgres"
	// BackendMemory is the in-memory store (tests only).
	BackendMemory = "memory"

	envBackend    = "KLAUS_RESULT_BACKEND"
	envDSN        = "KLAUS_PG_DSN"
	envSessionDir = "KLAUS_SESSION_DIR"
)

// NewStore constructs a Store from environment variables.
//
//   - Klaus_RESULT_BACKEND (default "local"): "local" | "postgres" | "memory"
//   - KLAUS_PG_DSN: required when backend=postgres
//   - KLAUS_SESSION_DIR: base directory for the local backend (default ~/.klaus/sessions)
//
// The Postgres backend fails fast if no DSN is set or the DB is unreachable.
// The local backend falls back gracefully to os.TempDir if the directory cannot
// be created.
func NewStore(ctx context.Context) (Store, error) {
	backend := os.Getenv(envBackend)
	if backend == "" {
		backend = BackendLocal
	}

	switch backend {
	case BackendPostgres:
		dsn := os.Getenv(envDSN)
		if dsn == "" {
			return nil, fmt.Errorf("KLAUS_RESULT_BACKEND=postgres but KLAUS_PG_DSN is not set")
		}
		return NewPostgresStore(ctx, dsn)

	case BackendMemory:
		return NewMemoryStore(), nil

	default: // local
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
}

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// localSession holds the persisted data for a single contextID in the local store.
type localSession struct {
	Turns []Turn `json:"turns"`
}

// LocalStore is a file-backed Store. Each contextID maps to a single JSON file
// under a configurable directory. It is the default backend when no Postgres DSN
// is provided, mirroring the existing KLAUS_RESULT_DIR pattern.
//
// All file operations are serialized via a single mutex.
type LocalStore struct {
	dir string
	mu  sync.Mutex // guards file-level operations
}

// NewLocalStore returns a LocalStore rooted at dir. dir is created if absent.
func NewLocalStore(dir string) (*LocalStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec
		return nil, fmt.Errorf("creating local session dir %q: %w", dir, err)
	}
	return &LocalStore{dir: dir}, nil
}

func (l *LocalStore) path(contextID string) string {
	// Use a short sanitized name to keep it filesystem-safe.
	safe := sanitize(contextID)
	return filepath.Join(l.dir, safe+".json")
}

func (l *LocalStore) load(contextID string) (*localSession, error) {
	data, err := os.ReadFile(l.path(contextID))
	if os.IsNotExist(err) {
		return &localSession{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading session file: %w", err)
	}
	var s localSession
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing session file: %w", err)
	}
	return &s, nil
}

func (l *LocalStore) save(contextID string, s *localSession) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshalling session: %w", err)
	}
	tmp := l.path(contextID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing session file: %w", err)
	}
	return os.Rename(tmp, l.path(contextID))
}

func (l *LocalStore) AppendTurn(_ context.Context, t Turn) error {
	if t.TS.IsZero() {
		t.TS = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.load(t.ContextID)
	if err != nil {
		return err
	}
	if t.Seq == 0 {
		t.Seq = len(s.Turns) + 1
	}
	s.Turns = append(s.Turns, t)
	return l.save(t.ContextID, s)
}

func (l *LocalStore) History(_ context.Context, contextID string) ([]Turn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.load(contextID)
	if err != nil {
		return nil, err
	}
	out := make([]Turn, len(s.Turns))
	copy(out, s.Turns)
	return out, nil
}

// sanitize replaces characters not safe for filenames with underscores.
func sanitize(s string) string {
	out := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			out[i] = c
		} else {
			out[i] = '_'
		}
	}
	return string(out)
}

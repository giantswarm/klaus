package session

import (
	"context"
	"sync"
)

// MemoryStore is an in-memory Store used in tests. It is not suitable for
// production: all state is lost on process exit.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]string // contextID → sessionID
	turns    map[string][]Turn // contextID → ordered turns
}

// NewMemoryStore returns an empty in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]string),
		turns:    make(map[string][]Turn),
	}
}

func (m *MemoryStore) SessionID(_ context.Context, contextID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[contextID], nil
}

func (m *MemoryStore) BindSession(_ context.Context, contextID, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[contextID] = sessionID
	return nil
}

func (m *MemoryStore) AppendTurn(_ context.Context, t Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns[t.ContextID] = append(m.turns[t.ContextID], t)
	return nil
}

func (m *MemoryStore) History(_ context.Context, contextID string) ([]Turn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	turns := m.turns[contextID]
	if len(turns) == 0 {
		return nil, nil
	}
	out := make([]Turn, len(turns))
	copy(out, turns)
	return out, nil
}

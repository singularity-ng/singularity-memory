package mcp

import (
	"sync"
	"time"
)

// Session holds per-session MCP state.
type Session struct {
	ID        string
	CreatedAt time.Time
	// Initialized tracks whether the session has completed initialize.
	Initialized bool
	// BankID is the default bank for this session (from X-Bank-Id header).
	BankID string
}

// SessionStore is an in-memory store keyed by Mcp-Session-Id.
type SessionStore struct {
	mu sync.Map
}

// NewSessionStore creates a new session store.
func NewSessionStore() *SessionStore {
	return &SessionStore{}
}

// GetOrCreate returns an existing session or creates a new one.
func (s *SessionStore) GetOrCreate(id string) *Session {
	if id == "" {
		return nil
	}
	if val, ok := s.mu.Load(id); ok {
		return val.(*Session)
	}
	sess := &Session{
		ID:        id,
		CreatedAt: time.Now(),
	}
	actual, loaded := s.mu.LoadOrStore(id, sess)
	if loaded {
		return actual.(*Session)
	}
	return sess
}

// Get returns an existing session or nil.
func (s *SessionStore) Get(id string) *Session {
	if val, ok := s.mu.Load(id); ok {
		return val.(*Session)
	}
	return nil
}

// Delete removes a session from the store.
func (s *SessionStore) Delete(id string) {
	s.mu.Delete(id)
}

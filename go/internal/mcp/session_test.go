package mcp

import (
	"testing"
)

func TestSessionStoreGetOrCreate(t *testing.T) {
	s := NewSessionStore()
	sess := s.GetOrCreate("sess-1")
	if sess == nil {
		t.Fatal("expected session, got nil")
	}
	if sess.ID != "sess-1" {
		t.Errorf("id = %q, want %q", sess.ID, "sess-1")
	}
	if sess.CreatedAt.IsZero() {
		t.Error("expected CreatedAt set")
	}

	// Second call returns same session
	sess2 := s.GetOrCreate("sess-1")
	if sess2 != sess {
		t.Error("expected same session pointer")
	}
}

func TestSessionStoreGet(t *testing.T) {
	s := NewSessionStore()
	if s.Get("missing") != nil {
		t.Error("expected nil for missing session")
	}
	s.GetOrCreate("sess-2")
	if s.Get("sess-2") == nil {
		t.Error("expected session for existing id")
	}
}

func TestSessionStoreDelete(t *testing.T) {
	s := NewSessionStore()
	s.GetOrCreate("sess-3")
	s.Delete("sess-3")
	if s.Get("sess-3") != nil {
		t.Error("expected nil after delete")
	}
}

func TestSessionStoreEmptyID(t *testing.T) {
	s := NewSessionStore()
	if s.GetOrCreate("") != nil {
		t.Error("expected nil for empty id")
	}
}

package main

import (
	"container/list"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

type Session struct {
	mu      sync.Mutex
	ID      string
	History []string
}

// SessionStore is an LRU-bounded map of session IDs to Session. When the
// store exceeds maxSessions entries, the least-recently-used session is
// evicted on the next touch. Both bounds are required: maxHist caps the
// per-session history length, maxSessions caps the total session count.
type SessionStore struct {
	mu          sync.Mutex
	sessions    map[string]*Session
	entries     map[string]*list.Element
	order       *list.List
	maxHist     int
	maxSessions int
}

type sessionEntry struct {
	id      string
	session *Session
}

// NewSessionStore returns a store with the given per-session history limit
// and a hard cap on the number of sessions retained at once. maxSessions <=
// 0 falls back to a sensible default (10000).
func NewSessionStore(maxHist, maxSessions int) *SessionStore {
	if maxSessions <= 0 {
		maxSessions = 10000
	}
	return &SessionStore{
		sessions:    make(map[string]*Session),
		entries:     make(map[string]*list.Element),
		order:       list.New(),
		maxHist:     maxHist,
		maxSessions: maxSessions,
	}
}

// GetOrCreate returns the session for id, creating it (and touching it as
// most-recently-used) if it doesn't exist yet.
func (s *SessionStore) GetOrCreate(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.touchLocked(id)
}

// touchLocked must be called with s.mu held.
func (s *SessionStore) touchLocked(id string) *Session {
	if elem, ok := s.entries[id]; ok {
		s.order.MoveToFront(elem)
		return elem.Value.(*sessionEntry).session
	}
	sess := &Session{ID: id, History: make([]string, 0, s.maxHist)}
	elem := s.order.PushFront(&sessionEntry{id: id, session: sess})
	s.entries[id] = elem
	s.sessions[id] = sess
	if s.order.Len() > s.maxSessions {
		oldest := s.order.Back()
		if oldest != nil {
			ent := oldest.Value.(*sessionEntry)
			s.order.Remove(oldest)
			delete(s.entries, ent.id)
			delete(s.sessions, ent.id)
		}
	}
	return sess
}

// AddCall appends call to the session's history, trimming to maxHist and
// marking the session as most-recently-used.
func (s *SessionStore) AddCall(id, call string) {
	s.mu.Lock()
	sess := s.touchLocked(id)
	s.mu.Unlock()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.History = append(sess.History, call)
	if len(sess.History) > s.maxHist {
		sess.History = sess.History[len(sess.History)-s.maxHist:]
	}
}

// Snapshot returns a copy of the session history, or nil if the session is
// unknown. The session is marked as most-recently-used by the lookup.
func (s *SessionStore) Snapshot(id string) []string {
	s.mu.Lock()
	elem, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	s.order.MoveToFront(elem)
	sess := elem.Value.(*sessionEntry).session
	s.mu.Unlock()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	out := make([]string, len(sess.History))
	copy(out, sess.History)
	return out
}

// Count returns the number of sessions currently retained.
func (s *SessionStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func NewSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}

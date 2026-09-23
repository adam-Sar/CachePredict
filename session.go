package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

type Session struct {
	mu      sync.Mutex
	ID      string
	History []string
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	maxHist  int
}

func NewSessionStore(maxHist int) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		maxHist:  maxHist,
	}
}

func (s *SessionStore) GetOrCreate(id string) *Session {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if ok {
		return sess
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess
	}
	sess = &Session{
		ID:      id,
		History: make([]string, 0, s.maxHist),
	}
	s.sessions[id] = sess
	return sess
}

func (s *SessionStore) AddCall(id, call string) {
	sess := s.GetOrCreate(id)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.History = append(sess.History, call)
	if len(sess.History) > s.maxHist {
		sess.History = sess.History[len(sess.History)-s.maxHist:]
	}
}

func (s *SessionStore) Snapshot(id string) []string {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	out := make([]string, len(sess.History))
	copy(out, sess.History)
	return out
}

func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

func NewSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}
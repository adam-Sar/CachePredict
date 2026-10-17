package precog

import (
	"net/http"
	"sync"
	"time"
)

// ResponseEntry represents the HTTP response stored in the cache.
type ResponseEntry struct {
	Body        []byte
	Header      http.Header
	ContentType string
	StatusCode  int
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Store defines the cache storage interface for Precog.
type Store interface {
	Get(key string) (*ResponseEntry, bool)
	Set(key string, entry *ResponseEntry, ttl time.Duration) bool
	Del(key string)
	Close() error
}


// MemoryStore is a zero-dependency in-memory store suitable for testing.
type MemoryStore struct {
	mu    sync.RWMutex
	items map[string]memItem
}

type memItem struct {
	entry     *ResponseEntry
	expiresAt time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: make(map[string]memItem)}
}

func (s *MemoryStore) Get(key string) (*ResponseEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[key]
	if !ok || (!item.expiresAt.IsZero() && time.Now().After(item.expiresAt)) {
		return nil, false
	}
	return item.entry, true
}

func (s *MemoryStore) Set(key string, entry *ResponseEntry, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	s.items[key] = memItem{entry: entry, expiresAt: exp}
	return true
}

func (s *MemoryStore) Del(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]memItem)
	return nil
}

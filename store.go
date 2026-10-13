package precog

import (
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
)

// ResponseEntry represents the HTTP response stored in the cache.
type ResponseEntry struct {
	Body        []byte
	ContentType string
	StatusCode  int
	CreatedAt   time.Time
}

// Store defines the cache storage interface for Precog.
type Store interface {
	Get(key string) (*ResponseEntry, bool)
	Set(key string, entry *ResponseEntry, ttl time.Duration) bool
	Del(key string)
	Close() error
}

// RistrettoStore is the high-performance in-memory cache implementation using TinyLFU.
type RistrettoStore struct {
	cache *ristretto.Cache
}

// NewRistrettoStore creates a new TinyLFU cache with sane defaults.
func NewRistrettoStore(maxCost int64) (*RistrettoStore, error) {
	if maxCost <= 0 {
		maxCost = 100 * 1024 * 1024 // 100 MB default capacity
	}
	c, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,     // 1M counters for tracking access frequency
		MaxCost:     maxCost, // Max total bytes stored
		BufferItems: 64,      // 64 keys per Get buffer
	})
	if err != nil {
		return nil, err
	}
	return &RistrettoStore{cache: c}, nil
}

func (s *RistrettoStore) Get(key string) (*ResponseEntry, bool) {
	val, found := s.cache.Get(key)
	if !found || val == nil {
		return nil, false
	}
	entry, ok := val.(*ResponseEntry)
	return entry, ok
}

func (s *RistrettoStore) Set(key string, entry *ResponseEntry, ttl time.Duration) bool {
	cost := int64(len(entry.Body)) + 64
	return s.cache.SetWithTTL(key, entry, cost, ttl)
}

func (s *RistrettoStore) Del(key string) {
	s.cache.Del(key)
}

func (s *RistrettoStore) Close() error {
	s.cache.Close()
	return nil
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

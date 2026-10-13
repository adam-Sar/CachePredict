//go:build !js || !wasm

package precog

import (
	"time"

	"github.com/dgraph-io/ristretto"
)

// RistrettoStore is the high-performance in-memory cache implementation using TinyLFU.
// Note: ShardedStore is recommended for zero-GC overhead and synchronous write consistency.
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

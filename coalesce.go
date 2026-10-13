package precog

import (
	"sync"

	"github.com/cespare/xxhash/v2"
)

const numCoalesceShards = 64

type inflightCall struct {
	wg    sync.WaitGroup
	entry *ResponseEntry
	err   error
}

type coalesceShard struct {
	mu    sync.Mutex
	calls map[string]*inflightCall
}

// InflightGroup deduplicates concurrent requests for the same cache key.
// If a background prefetcher or another request is currently fetching data from
// the database, subsequent requests automatically wait and share the single result
// directly from memory, stopping any duplicate queries before they reach the database.
type InflightGroup struct {
	shards [numCoalesceShards]coalesceShard
}

// NewInflightGroup creates a sharded in-flight request coalescer.
func NewInflightGroup() *InflightGroup {
	g := &InflightGroup{}
	for i := 0; i < numCoalesceShards; i++ {
		g.shards[i].calls = make(map[string]*inflightCall)
	}
	return g
}

func (g *InflightGroup) shardFor(key string) *coalesceShard {
	h := xxhash.Sum64String(key)
	return &g.shards[h%numCoalesceShards]
}

// Do executes fn once for a given key. If another call is already in-flight for
// the same key, it blocks until the active call finishes and returns the cached result,
// preventing any duplicate database calls.
func (g *InflightGroup) Do(key string, fn func() (*ResponseEntry, error)) (*ResponseEntry, error) {
	sh := g.shardFor(key)

	sh.mu.Lock()
	if c, exists := sh.calls[key]; exists {
		// Call is ALREADY in-flight!
		sh.mu.Unlock()
		c.wg.Wait() // Wait for active DB query to complete
		return c.entry, c.err
	}

	// First call for this key: register in-flight marker
	c := &inflightCall{}
	c.wg.Add(1)
	sh.calls[key] = c
	sh.mu.Unlock()

	// Execute actual query/handler exactly once
	c.entry, c.err = fn()

	// Notify all waiting callers and unregister
	sh.mu.Lock()
	delete(sh.calls, key)
	sh.mu.Unlock()

	c.wg.Done()
	return c.entry, c.err
}

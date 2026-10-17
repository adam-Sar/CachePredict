package precog

import (
	"fmt"
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

// InflightGroup coalesces concurrent requests for identical cache keys.
// Subsequent requests wait on the active in-flight execution and share the resulting entry,
// preventing duplicate upstream database or handler invocations.
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

// Do executes fn once for a given key. Concurrent callers with identical keys
// block until the active execution completes and share the returned entry.
func (g *InflightGroup) Do(key string, fn func() (*ResponseEntry, error)) (*ResponseEntry, error) {
	sh := g.shardFor(key)

	sh.mu.Lock()
	if c, exists := sh.calls[key]; exists {
		sh.mu.Unlock()
		c.wg.Wait()
		return c.entry, c.err
	}

	// First call for this key: register in-flight marker
	c := &inflightCall{}
	c.wg.Add(1)
	sh.calls[key] = c
	sh.mu.Unlock()

	var panicked bool
	var panicVal any

	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				panicVal = r
			}
		}()
		c.entry, c.err = fn()
	}()

	sh.mu.Lock()
	delete(sh.calls, key)
	sh.mu.Unlock()

	if panicked {
		c.err = fmt.Errorf("precog: coalesced handler panicked: %v", panicVal)
	}

	c.wg.Done()

	if panicked {
		panic(panicVal)
	}

	return c.entry, c.err
}

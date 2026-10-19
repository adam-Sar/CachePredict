package precog

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/cespare/xxhash/v2"
)

const numCoalesceShards = 64

// ErrNotShareable is returned to waiting followers when the leader's response
// cannot be safely shared (e.g. Set-Cookie, private Cache-Control, or non-200 code).
var ErrNotShareable = errors.New("precog: response is private or unshareable")

// ErrStreamingResponse is returned to waiting followers when the leader's response
// is detected as a live stream (e.g. SSE) and followers must execute independently.
var ErrStreamingResponse = errors.New("precog: response is streaming and cannot be coalesced")

type inflightCall struct {
	done  chan struct{}
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
	return g.DoContext(context.Background(), key, fn)
}

// DoContext executes fn once for a given key, respecting context cancellation for waiting callers.
// If ctx is cancelled before completion, waiting callers return ctx.Err() immediately.
func (g *InflightGroup) DoContext(ctx context.Context, key string, fn func() (*ResponseEntry, error)) (*ResponseEntry, error) {
	sh := g.shardFor(key)

	sh.mu.Lock()
	if c, exists := sh.calls[key]; exists {
		sh.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.done:
			return c.entry, c.err
		}
	}

	// First call for this key: register in-flight marker
	c := &inflightCall{
		done: make(chan struct{}),
	}
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

	close(c.done)

	if panicked {
		panic(panicVal)
	}

	return c.entry, c.err
}

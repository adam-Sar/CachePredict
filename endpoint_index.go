package precog

import (
	"sync"
	"sync/atomic"
)

// MaxIndexedRoutes is the maximum number of distinct API endpoint patterns supported.
const MaxIndexedRoutes = 4096

// EndpointRegistry dynamically and thread-safely maps route patterns
// (e.g. "GET /items/:id", "POST /cart") to compact uint16 integer IDs.
// This enables nanosecond-speed L1 CPU cache array lookups instead of slow string map hashing.
type EndpointRegistry struct {
	mu       sync.Mutex
	patterns atomic.Value // holds map[string]uint16 for lock-free reads
	reverse  atomic.Value // holds []string for O(1) reverse lookup by uint16
	count    uint32
}

// NewEndpointRegistry creates a thread-safe endpoint registry.
func NewEndpointRegistry() *EndpointRegistry {
	r := &EndpointRegistry{}
	r.patterns.Store(make(map[string]uint16))
	r.reverse.Store(make([]string, 0, 64))
	return r
}

// GetOrRegister returns the uint16 ID for a route pattern, registering it if new.
// Fast path is completely lock-free using atomic.Value.
func (r *EndpointRegistry) GetOrRegister(pattern string) uint16 {
	// 1. Lock-free fast path for known routes
	m := r.patterns.Load().(map[string]uint16)
	if id, exists := m[pattern]; exists {
		return id
	}

	// 2. Slow path: register new route pattern under lock
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring lock
	m = r.patterns.Load().(map[string]uint16)
	if id, exists := m[pattern]; exists {
		return id
	}

	currentCount := uint16(r.count)
	if currentCount >= MaxIndexedRoutes-1 {
		return currentCount // Capped at max
	}

	newID := currentCount
	r.count++

	// Copy-On-Write (COW) update to ensure concurrent lock-free readers see immutable state
	newMap := make(map[string]uint16, len(m)+1)
	for k, v := range m {
		newMap[k] = v
	}
	newMap[pattern] = newID
	r.patterns.Store(newMap)

	rev := r.reverse.Load().([]string)
	newRev := make([]string, len(rev)+1)
	copy(newRev, rev)
	newRev[newID] = pattern
	r.reverse.Store(newRev)

	return newID
}

// Lookup returns the ID for a pattern if registered, or false. (Zero allocations, lock-free)
func (r *EndpointRegistry) Lookup(pattern string) (uint16, bool) {
	m := r.patterns.Load().(map[string]uint16)
	id, ok := m[pattern]
	return id, ok
}

// Pattern returns the route pattern for a given uint16 ID in O(1) time.
func (r *EndpointRegistry) Pattern(id uint16) string {
	rev := r.reverse.Load().([]string)
	if int(id) < len(rev) {
		return rev[id]
	}
	return ""
}

// Count returns the number of registered unique route patterns.
func (r *EndpointRegistry) Count() int {
	return int(atomic.LoadUint32(&r.count))
}

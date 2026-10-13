package precog

import (
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
)

const (
	// L1Slots defines the number of direct-mapped atomic cache slots.
	L1Slots = 256
)

type l1Slot struct {
	hash     atomic.Uint64
	expireAt atomic.Int64
	entry    atomic.Pointer[ResponseEntry]
}

// TieredStore implements a two-tier caching hierarchy combining a direct-mapped
// atomic L1 cache with a sharded L2 store.
type TieredStore struct {
	l1 [L1Slots]l1Slot
	l2 Store
}

// NewTieredStore creates a two-tier caching engine with the specified L2 backend.
// If l2 is nil, a default ShardedStore is used.
func NewTieredStore(l2 Store) *TieredStore {
	if l2 == nil {
		l2 = NewShardedStore(nil)
	}
	return &TieredStore{
		l2: l2,
	}
}

// Get retrieves an item by key, checking L1 first before falling back to L2.
func (t *TieredStore) Get(key string) (*ResponseEntry, bool) {
	h := xxhash.Sum64String(key)
	slotIdx := h & (L1Slots - 1)

	// Probe L1 atomic slot
	if t.l1[slotIdx].hash.Load() == h {
		exp := t.l1[slotIdx].expireAt.Load()
		if exp == 0 || time.Now().UnixNano() <= exp {
			entry := t.l1[slotIdx].entry.Load()
			if entry != nil {
				return entry, true
			}
		}
	}

	// Fall back to L2 store
	entry, found := t.l2.Get(key)
	if !found || entry == nil {
		return nil, false
	}

	// Promote entry to L1
	t.promoteL1(slotIdx, h, entry, 0)
	return entry, true
}

// Set stores an item into L2 and populates L1 for instant subsequent hits.
func (t *TieredStore) Set(key string, entry *ResponseEntry, ttl time.Duration) bool {
	if entry == nil {
		return false
	}

	// Write to L2
	ok := t.l2.Set(key, entry, ttl)
	if !ok {
		return false
	}

	// Promote to L1
	h := xxhash.Sum64String(key)
	slotIdx := h & (L1Slots - 1)
	var exp int64
	if ttl > 0 {
		exp = time.Now().Add(ttl).UnixNano()
	}
	t.promoteL1(slotIdx, h, entry, exp)
	return true
}

func (t *TieredStore) promoteL1(slotIdx uint64, h uint64, entry *ResponseEntry, exp int64) {
	t.l1[slotIdx].entry.Store(entry)
	t.l1[slotIdx].expireAt.Store(exp)
	t.l1[slotIdx].hash.Store(h)
}

// Del invalidates the key in both L1 and L2.
func (t *TieredStore) Del(key string) {
	h := xxhash.Sum64String(key)
	slotIdx := h & (L1Slots - 1)

	if t.l1[slotIdx].hash.Load() == h {
		t.l1[slotIdx].hash.Store(0)
		t.l1[slotIdx].entry.Store(nil)
	}

	t.l2.Del(key)
}

// L2 returns the underlying L2 store.
func (t *TieredStore) L2() Store {
	return t.l2
}

// Close closes the underlying L2 store and clears L1.
func (t *TieredStore) Close() error {
	for i := 0; i < L1Slots; i++ {
		t.l1[i].hash.Store(0)
		t.l1[i].entry.Store(nil)
	}
	return t.l2.Close()
}

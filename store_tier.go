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

type l1Entry struct {
	hash     uint64
	expireAt int64
	entry    *ResponseEntry
}

type l1Slot struct {
	data atomic.Pointer[l1Entry]
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

	// Probe L1 atomic slot (lock-free immutable snapshot)
	if item := t.l1[slotIdx].data.Load(); item != nil && item.hash == h {
		if item.expireAt == 0 || time.Now().UnixNano() <= item.expireAt {
			if item.entry != nil {
				return item.entry, true
			}
		}
	}

	// Fall back to L2 store
	entry, found := t.l2.Get(key)
	if !found || entry == nil {
		return nil, false
	}

	var exp int64
	if !entry.ExpiresAt.IsZero() {
		exp = entry.ExpiresAt.UnixNano()
		if time.Now().UnixNano() > exp {
			return nil, false
		}
	}

	// Promote entry to L1 with remaining expiration
	t.promoteL1(slotIdx, h, entry, exp)
	return entry, true
}

// Set stores an item into L2 and populates L1 for instant subsequent hits.
func (t *TieredStore) Set(key string, entry *ResponseEntry, ttl time.Duration) bool {
	if entry == nil {
		return false
	}

	var exp int64
	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl)
		exp = entry.ExpiresAt.UnixNano()
	}

	// Write to L2
	ok := t.l2.Set(key, entry, ttl)
	if !ok {
		return false
	}

	// Promote to L1
	h := xxhash.Sum64String(key)
	slotIdx := h & (L1Slots - 1)
	t.promoteL1(slotIdx, h, entry, exp)
	return true
}

func (t *TieredStore) promoteL1(slotIdx uint64, h uint64, entry *ResponseEntry, exp int64) {
	t.l1[slotIdx].data.Store(&l1Entry{
		hash:     h,
		expireAt: exp,
		entry:    entry,
	})
}

// Del invalidates the key in both L1 and L2.
func (t *TieredStore) Del(key string) {
	h := xxhash.Sum64String(key)
	slotIdx := h & (L1Slots - 1)

	if item := t.l1[slotIdx].data.Load(); item != nil && item.hash == h {
		t.l1[slotIdx].data.CompareAndSwap(item, nil)
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
		t.l1[i].data.Store(nil)
	}
	return t.l2.Close()
}

package precog_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/adam-Sar/precog"
)

func TestEndpointRegistry(t *testing.T) {
	reg := precog.NewEndpointRegistry()

	id0 := reg.GetOrRegister("GET /api/users/:id")
	if id0 != 0 {
		t.Fatalf("expected first ID to be 0, got %d", id0)
	}

	id1 := reg.GetOrRegister("POST /api/orders")
	if id1 != 1 {
		t.Fatalf("expected second ID to be 1, got %d", id1)
	}

	// Repeated lookup should return identical ID
	if reg.GetOrRegister("GET /api/users/:id") != 0 {
		t.Fatalf("expected duplicate lookup to return 0")
	}

	// Reverse lookup
	if reg.Pattern(0) != "GET /api/users/:id" {
		t.Fatalf("expected pattern 0 to be 'GET /api/users/:id', got '%s'", reg.Pattern(0))
	}
	if reg.Pattern(1) != "POST /api/orders" {
		t.Fatalf("expected pattern 1 to be 'POST /api/orders', got '%s'", reg.Pattern(1))
	}

	if reg.Count() != 2 {
		t.Fatalf("expected count 2, got %d", reg.Count())
	}
}

func TestEndpointRegistryConcurrency(t *testing.T) {
	reg := precog.NewEndpointRegistry()
	var wg sync.WaitGroup
	numWorkers := 32

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				pattern := "GET /api/res/:id"
				id := reg.GetOrRegister(pattern)
				if id != 0 {
					t.Errorf("worker %d got wrong ID %d for same pattern", workerID, id)
				}
				lookupID, ok := reg.Lookup(pattern)
				if !ok || lookupID != 0 {
					t.Errorf("worker %d lookup failed", workerID)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestFastHash64(t *testing.T) {
	h1 := precog.FastHash64("GET", "/items", "b=2&a=1", "")
	h2 := precog.FastHash64("GET", "/items", "a=1&b=2", "")
	if h1 != h2 {
		t.Fatalf("expected sorted query hashes to match: %d vs %d", h1, h2)
	}

	k1 := precog.CanonicalCacheKey("GET", "/items", "b=2&a=1", "")
	k2 := precog.CanonicalCacheKey("GET", "/items", "a=1&b=2", "")
	if k1 != k2 {
		t.Fatalf("expected canonical cache keys to match: %s vs %s", k1, k2)
	}
	if len(k1) != 16 {
		t.Fatalf("expected 16-hex-char key, got len=%d (%s)", len(k1), k1)
	}
}

func TestEndpointRegistryCapacityReturnsSentinel(t *testing.T) {
	reg := precog.NewEndpointRegistry()

	// Fill the registry to capacity
	for i := 0; i < precog.MaxIndexedRoutes; i++ {
		pattern := "GET /route/" + fmt.Sprintf("%d", i)
		if id := reg.GetOrRegister(pattern); id == precog.InvalidRouteID {
			t.Fatalf("unexpected sentinel before capacity at %d", i)
		}
	}

	if reg.Count() != precog.MaxIndexedRoutes {
		t.Fatalf("expected count %d, got %d", precog.MaxIndexedRoutes, reg.Count())
	}

	// Existing pattern must still resolve correctly at capacity
	if reg.GetOrRegister("GET /route/0") != 0 {
		t.Fatalf("existing pattern lookup broke at capacity")
	}

	// NEW pattern at capacity must return the sentinel, never an aliased existing ID
	if id := reg.GetOrRegister("GET /overflow"); id != precog.InvalidRouteID {
		t.Fatalf("expected InvalidRouteID sentinel at capacity, got %d", id)
	}
}

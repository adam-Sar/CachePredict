package precog_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/adam-Sar/precog"
)

func TestTieredStorePromotionAndHit(t *testing.T) {
	sharded := precog.NewShardedStore(nil)
	tiered := precog.NewTieredStore(sharded)
	defer tiered.Close()

	key := "hot_resource_key"
	entry := &precog.ResponseEntry{
		StatusCode:  http.StatusOK,
		ContentType: "application/json",
		Body:        []byte(`{"tier":"l1_l2"}`),
	}

	// 1. Set writes to L2 and seeds L1
	tiered.Set(key, entry, 5*time.Second)

	// 2. Immediate read hits L1
	got, found := tiered.Get(key)
	if !found {
		t.Fatalf("expected key to be found")
	}
	if string(got.Body) != `{"tier":"l1_l2"}` {
		t.Errorf("body mismatch: %s", string(got.Body))
	}

	// 3. Delete should clear both L1 and L2
	tiered.Del(key)
	_, foundAfterDel := tiered.Get(key)
	if foundAfterDel {
		t.Errorf("expected key to be deleted from both tiers")
	}
}

func TestTieredStoreHighConcurrency(t *testing.T) {
	tiered := precog.NewTieredStore(precog.NewShardedStore(nil))
	defer tiered.Close()

	var wg sync.WaitGroup
	workers := 32
	ops := 300

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				k := fmt.Sprintf("k_%d", j%10) // Small key space to test hot L1 collisions and hits
				entry := &precog.ResponseEntry{
					StatusCode:  200,
					ContentType: "text/plain",
					Body:        []byte("hot_content"),
				}
				tiered.Set(k, entry, 2*time.Second)
				val, ok := tiered.Get(k)
				if !ok || val.StatusCode != 200 {
					t.Errorf("worker %d failed to read %s", workerID, k)
				}
			}
		}(i)
	}
	wg.Wait()
}

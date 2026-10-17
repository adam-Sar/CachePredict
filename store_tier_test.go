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

func TestTieredStoreL1Expiration(t *testing.T) {
	tiered := precog.NewTieredStore(precog.NewShardedStore(nil))
	defer tiered.Close()

	key := "short_lived_key"
	entry := &precog.ResponseEntry{
		StatusCode:  200,
		ContentType: "application/json",
		Body:        []byte(`{"expiring":true}`),
	}

	// 50ms TTL
	tiered.Set(key, entry, 50*time.Millisecond)

	// Immediate hit (seeds L1)
	val, ok := tiered.Get(key)
	if !ok || val == nil {
		t.Fatalf("expected immediate hit")
	}

	// Sleep past expiration
	time.Sleep(80 * time.Millisecond)

	// L1 must respect expiration and not serve stale data
	_, okAfter := tiered.Get(key)
	if okAfter {
		t.Fatalf("expected key to be expired from L1 and L2")
	}
}

func TestTieredStoreNoTornReads(t *testing.T) {
	tiered := precog.NewTieredStore(precog.NewShardedStore(nil))
	defer tiered.Close()

	var wg sync.WaitGroup
	workers := 16
	ops := 200

	// Multiple distinct keys that will frequently map into L1
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(wID int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				k := fmt.Sprintf("key_w%d_j%d", wID%4, j%8)
				expectedBody := fmt.Sprintf("data_for_%s", k)

				tiered.Set(k, &precog.ResponseEntry{
					StatusCode:  200,
					ContentType: "text/plain",
					Body:        []byte(expectedBody),
				}, time.Second)

				got, found := tiered.Get(k)
				if found {
					// CRITICAL: Ensure we never read an entry belonging to a different key!
					if string(got.Body) != expectedBody {
						t.Errorf("TORN READ DETECTED: expected %s, got %s", expectedBody, string(got.Body))
					}
				}
			}
		}(i)
	}

	wg.Wait()
}

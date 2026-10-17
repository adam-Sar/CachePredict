package precog_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/adam-Sar/precog"
)

func TestShardedStoreBasic(t *testing.T) {
	store := precog.NewShardedStore(&precog.ShardedConfig{
		NumShards:    16,
		MaxCostBytes: 10 * 1024 * 1024,
	})
	defer store.Close()

	key := "test_key_1"
	entry := &precog.ResponseEntry{
		StatusCode:  http.StatusOK,
		ContentType: "application/json",
		Body:        []byte(`{"greeting":"hello precog"}`),
	}

	ok := store.Set(key, entry, 5*time.Second)
	if !ok {
		t.Fatalf("expected Set to succeed")
	}

	got, found := store.Get(key)
	if !found {
		t.Fatalf("expected key to be found")
	}

	if got.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", got.StatusCode)
	}
	if got.ContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", got.ContentType)
	}
	if string(got.Body) != `{"greeting":"hello precog"}` {
		t.Errorf("body mismatch: %s", string(got.Body))
	}

	// Test Deletion
	store.Del(key)
	_, foundAfterDel := store.Get(key)
	if foundAfterDel {
		t.Errorf("expected key to be deleted")
	}
}

func TestShardedStoreTTL(t *testing.T) {
	store := precog.NewShardedStore(&precog.ShardedConfig{
		NumShards: 8,
	})
	defer store.Close()

	key := "expire_key"
	entry := &precog.ResponseEntry{
		StatusCode:  http.StatusOK,
		ContentType: "text/plain",
		Body:        []byte("expiring soon"),
	}

	// 50ms TTL
	store.Set(key, entry, 50*time.Millisecond)

	_, found := store.Get(key)
	if !found {
		t.Fatalf("expected key to be found immediately")
	}

	time.Sleep(70 * time.Millisecond)

	_, foundAfterExpire := store.Get(key)
	if foundAfterExpire {
		t.Errorf("expected key to have expired")
	}
}

func TestShardedStoreHighConcurrency(t *testing.T) {
	store := precog.NewShardedStore(&precog.ShardedConfig{
		NumShards:    64,
		MaxCostBytes: 32 * 1024 * 1024,
	})
	defer store.Close()

	var wg sync.WaitGroup
	workers := 64
	opsPerWorker := 200

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				k := fmt.Sprintf("worker_%d_key_%d", workerID, j)
				entry := &precog.ResponseEntry{
					StatusCode:  200,
					ContentType: "application/json",
					Body:        []byte(fmt.Sprintf(`{"worker":%d,"idx":%d}`, workerID, j)),
				}
				store.Set(k, entry, 10*time.Second)

				val, found := store.Get(k)
				if !found || val.StatusCode != 200 {
					t.Errorf("concurrency error for key %s", k)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestResponseEntryWriteResponse(t *testing.T) {
	entry := &precog.ResponseEntry{
		StatusCode:  200,
		ContentType: "application/json",
		Body:        []byte(`{"status":"precog_dma"}`),
	}

	rec := httptest.NewRecorder()
	n, err := entry.WriteResponse(rec)
	if err != nil {
		t.Fatalf("WriteResponse failed: %v", err)
	}
	if n <= 0 {
		t.Errorf("expected positive bytes written, got %d", n)
	}
	if rec.Code != 200 {
		t.Errorf("expected code 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT")
	}
	if rec.Body.String() != `{"status":"precog_dma"}` {
		t.Errorf("body mismatch: %s", rec.Body.String())
	}
}

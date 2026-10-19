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

func TestShardedStorePreservesHeaders(t *testing.T) {
	store := precog.NewShardedStore(&precog.ShardedConfig{
		NumShards: 8,
	})
	defer store.Close()

	key := "headers_key"
	hdr := make(http.Header)
	hdr.Set("ETag", `"etag-123456789"`)
	hdr.Set("X-Custom-Trace", "trace-xyz-abc")
	hdr.Set("Access-Control-Allow-Origin", "*")
	hdr.Add("Vary", "Accept-Encoding")
	hdr.Add("Vary", "Origin")

	entry := &precog.ResponseEntry{
		StatusCode:  200,
		ContentType: "application/json",
		Header:      hdr,
		Body:        []byte(`{"data":"headers_preserved"}`),
	}

	ok := store.Set(key, entry, 5*time.Second)
	if !ok {
		t.Fatalf("failed to set entry")
	}

	got, found := store.Get(key)
	if !found {
		t.Fatalf("entry not found")
	}

	if got.Header == nil {
		t.Fatalf("expected Header map to be preserved, got nil")
	}
	if got.Header.Get("ETag") != `"etag-123456789"` {
		t.Errorf("expected ETag preserved, got %s", got.Header.Get("ETag"))
	}
	if got.Header.Get("X-Custom-Trace") != "trace-xyz-abc" {
		t.Errorf("expected X-Custom-Trace preserved, got %s", got.Header.Get("X-Custom-Trace"))
	}
	if got.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected CORS preserved, got %s", got.Header.Get("Access-Control-Allow-Origin"))
	}
	varyVals := got.Header.Values("Vary")
	if len(varyVals) != 2 || varyVals[0] != "Accept-Encoding" || varyVals[1] != "Origin" {
		t.Errorf("expected multiple Vary values preserved, got %v", varyVals)
	}

	// Verify WriteResponse replays all preserved headers
	rec := httptest.NewRecorder()
	_, err := got.WriteResponse(rec)
	if err != nil {
		t.Fatalf("WriteResponse failed: %v", err)
	}
	if rec.Header().Get("ETag") != `"etag-123456789"` {
		t.Errorf("WriteResponse missing ETag, got %s", rec.Header().Get("ETag"))
	}
	if rec.Header().Get("X-Custom-Trace") != "trace-xyz-abc" {
		t.Errorf("WriteResponse missing X-Custom-Trace, got %s", rec.Header().Get("X-Custom-Trace"))
	}
}

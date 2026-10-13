package precog_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adam-Sar/precog"
)

func TestInflightCoalescer(t *testing.T) {
	g := precog.NewInflightGroup()
	var dbQueries int32

	var wg sync.WaitGroup
	numConcurrent := 20
	responses := make([]*precog.ResponseEntry, numConcurrent)

	// 20 concurrent requests arrive at the exact same millisecond
	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			entry, err := g.Do("product_42_key", func() (*precog.ResponseEntry, error) {
				// Simulating slow 90km database roundtrip
				atomic.AddInt32(&dbQueries, 1)
				time.Sleep(50 * time.Millisecond)

				return &precog.ResponseEntry{
					StatusCode:  200,
					ContentType: "application/json",
					Body:        []byte(`{"id":42,"name":"Sneakers"}`),
				}, nil
			})

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			responses[idx] = entry
		}(i)
	}

	wg.Wait()

	// CRITICAL PROOF: The database was queried EXACTLY ONCE across all 20 concurrent requests!
	if atomic.LoadInt32(&dbQueries) != 1 {
		t.Fatalf("expected EXACTLY 1 database query, got %d queries!", atomic.LoadInt32(&dbQueries))
	}

	// Verify all 20 callers received the valid response
	for i, resp := range responses {
		if resp == nil || string(resp.Body) != `{"id":42,"name":"Sneakers"}` {
			t.Errorf("caller %d got invalid response", i)
		}
	}
}

func TestMiddlewareRequestCoalescingUnderLoad(t *testing.T) {
	var dbCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/api/slow-resource", func(w http.ResponseWriter, r *http.Request) {
		// Simulate 90km database roundtrip
		atomic.AddInt32(&dbCalls, 1)
		time.Sleep(40 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"coalesced"}`))
	})

	engine, err := precog.New(precog.Config{
		TTL: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to init precog: %v", err)
	}
	defer engine.Close()

	server := httptest.NewServer(engine.Handler(mux))
	defer server.Close()

	client := server.Client()

	var wg sync.WaitGroup
	concurrentRequests := 15

	// Fire 15 requests simultaneously at the un-cached endpoint
	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(reqNum int) {
			defer wg.Done()
			resp, err := client.Get(server.URL + "/api/slow-resource")
			if err != nil {
				t.Errorf("request %d failed: %v", reqNum, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
		}(i)
	}

	wg.Wait()

	// PROOF: Out of 15 simultaneous requests hitting a cold route,
	// only 1 hit the database! The remaining 14 were coalesced in-flight.
	calls := atomic.LoadInt32(&dbCalls)
	if calls != 1 {
		t.Fatalf("expected EXACTLY 1 DB call for coalesced in-flight requests, got %d", calls)
	}
	fmt.Printf("[Coalescing Test Passed] 15 simultaneous requests triggered only %d DB call!\n", calls)
}

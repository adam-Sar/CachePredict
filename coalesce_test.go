package precog_test

import (
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
				// Simulate slow database query latency
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

	// Verify the upstream handler was invoked exactly once
	if atomic.LoadInt32(&dbQueries) != 1 {
		t.Fatalf("expected 1 database query, got %d", atomic.LoadInt32(&dbQueries))
	}

	// Verify all callers received the valid response
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

	calls := atomic.LoadInt32(&dbCalls)
	if calls != 1 {
		t.Fatalf("expected 1 DB call for coalesced in-flight requests, got %d", calls)
	}
}

func TestInflightCoalescerPanicSafety(t *testing.T) {
	g := precog.NewInflightGroup()
	key := "panic_resource"

	// First call panics
	assertPanics := func() {
		defer func() {
			_ = recover()
		}()
		_, _ = g.Do(key, func() (*precog.ResponseEntry, error) {
			panic("simulated upstream failure")
		})
	}
	assertPanics()

	// Subsequent call for the same key must NOT deadlock and must execute normally
	done := make(chan bool, 1)
	go func() {
		entry, err := g.Do(key, func() (*precog.ResponseEntry, error) {
			return &precog.ResponseEntry{
				StatusCode:  200,
				ContentType: "application/json",
				Body:        []byte(`{"recovered":true}`),
			}, nil
		})
		if err != nil || entry == nil || string(entry.Body) != `{"recovered":true}` {
			t.Errorf("expected clean recovery on subsequent call, got err=%v entry=%v", err, entry)
		}
		done <- true
	}()

	select {
	case <-done:
		// Succeeded without deadlock!
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("deadlock detected: subsequent call hung after upstream panic")
	}
}

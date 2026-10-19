package precog_test

import (
	"context"
	"io"
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

func TestMiddlewareCoalescingContextIsolation(t *testing.T) {
	var dbCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/api/isolated-resource", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&dbCalls, 1)
		// Simulate database query latency
		time.Sleep(60 * time.Millisecond)

		// Context should NOT be canceled even if caller 1 disconnected
		if err := r.Context().Err(); err != nil {
			http.Error(w, "context unexpectedly canceled: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"isolated":true}`))
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
	wg.Add(2)

	// Caller 1: Starts first, but client cancels context after 10ms
	go func() {
		defer wg.Done()
		ctx1, cancel1 := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel1()

		req1, _ := http.NewRequestWithContext(ctx1, "GET", server.URL+"/api/isolated-resource", nil)
		resp1, err := client.Do(req1)
		if err == nil {
			resp1.Body.Close()
		}
	}()

	// Small pause so Caller 1 registers in-flight first
	time.Sleep(5 * time.Millisecond)

	// Caller 2: Waiting follower with healthy context
	var resp2Status int
	var resp2Body string
	go func() {
		defer wg.Done()
		req2, _ := http.NewRequest("GET", server.URL+"/api/isolated-resource", nil)
		resp2, err := client.Do(req2)
		if err != nil {
			t.Errorf("caller 2 failed: %v", err)
			return
		}
		defer resp2.Body.Close()
		b, _ := io.ReadAll(resp2.Body)
		resp2Status = resp2.StatusCode
		resp2Body = string(b)
	}()

	wg.Wait()

	if resp2Status != http.StatusOK {
		t.Fatalf("caller 2 expected 200 OK, got %d (body=%s)", resp2Status, resp2Body)
	}
	if resp2Body != `{"isolated":true}` {
		t.Fatalf("caller 2 body mismatch: %s", resp2Body)
	}
	if atomic.LoadInt32(&dbCalls) != 1 {
		t.Fatalf("expected 1 DB call, got %d", atomic.LoadInt32(&dbCalls))
	}
}

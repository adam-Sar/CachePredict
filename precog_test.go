package precog_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adam-Sar/precog"
)

func TestPrecogBasicCaching(t *testing.T) {
	var handlerCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&handlerCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

	// 1st request -> Cache MISS
	resp1, err := client.Get(server.URL + "/api/data")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if resp1.Header.Get("X-Cache") != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %s", resp1.Header.Get("X-Cache"))
	}
	resp1.Body.Close()

	if atomic.LoadInt32(&handlerCalls) != 1 {
		t.Errorf("expected 1 handler call, got %d", atomic.LoadInt32(&handlerCalls))
	}

	// Second request should result in a cache hit
	resp2, err := client.Get(server.URL + "/api/data")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %s", resp2.Header.Get("X-Cache"))
	}
	resp2.Body.Close()

	if atomic.LoadInt32(&handlerCalls) != 1 {
		t.Errorf("expected handler call count to remain 1 on cache HIT, got %d", atomic.LoadInt32(&handlerCalls))
	}
}

func TestPrecogSpeculativePrewarming(t *testing.T) {
	var productCalls int32
	var cartCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/api/products/44", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&productCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "Skirt", "id": "44"})
	})
	mux.HandleFunc("/api/cart", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&cartCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]string{"cart": "active"})
	})

	predictor := precog.NewAdaptiveMarkovPredictor()
	// Pre-train the transition: 100% of users viewing products go to /api/cart
	predictor.SeedTransition("GET /api/products/:id", "GET /api/cart", 10)

	engine, err := precog.New(precog.Config{
		Predictor:           predictor,
		ConfidenceThreshold: 0.50, // 50% threshold
		TTL:                 5 * time.Second,
		Workers:             2,
	})
	if err != nil {
		t.Fatalf("failed to init precog: %v", err)
	}
	defer engine.Close()

	server := httptest.NewServer(engine.Handler(mux))
	defer server.Close()

	client := server.Client()

	// User requests /api/products/44
	resp1, err := client.Get(server.URL + "/api/products/44")
	if err != nil {
		t.Fatalf("products request failed: %v", err)
	}
	resp1.Body.Close()

	// Allow background prefetcher time to warm /api/cart in cache
	time.Sleep(150 * time.Millisecond)

	// Subsequent request to /api/cart should be served from cache
	req, _ := http.NewRequest("GET", server.URL+"/api/cart", nil)
	// Pass the same session cookie received from first call
	for _, c := range resp1.Cookies() {
		req.AddCookie(c)
	}

	resp2, err := client.Do(req)
	if err != nil {
		t.Fatalf("cart request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Errorf("expected speculatively pre-warmed /api/cart to be a HIT, got %s", resp2.Header.Get("X-Cache"))
	}

	stats := engine.Stats()
	if stats.CacheHits < 1 {
		t.Errorf("expected at least 1 cache hit, got %d", stats.CacheHits)
	}
}

func TestPrecogOnlineLearningFromLiveTraffic(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/step1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"step": "1"})
	})
	mux.HandleFunc("/step2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"step": "2"})
	})

	// Clean engine with zero pre-seeded knowledge
	engine, err := precog.New(precog.Config{
		ConfidenceThreshold: 0.50,
		TTL:                 5 * time.Second,
		Workers:             2,
	})
	if err != nil {
		t.Fatalf("failed to init precog: %v", err)
	}
	defer engine.Close()

	server := httptest.NewServer(engine.Handler(mux))
	defer server.Close()

	client := server.Client()

	// Phase 1: User A generates transition traffic
	// User A visits /step1
	userAResp1, err := client.Get(server.URL + "/step1")
	if err != nil {
		t.Fatalf("User A step 1 failed: %v", err)
	}
	userACookies := userAResp1.Cookies()
	userAResp1.Body.Close()

	// User A visits /step2 using session cookie
	reqA2, _ := http.NewRequest("GET", server.URL+"/step2", nil)
	for _, c := range userACookies {
		reqA2.AddCookie(c)
	}
	userAResp2, err := client.Do(reqA2)
	if err != nil {
		t.Fatalf("User A step 2 failed: %v", err)
	}
	userAResp2.Body.Close()

	// Phase 2: User B visits /step1 without prior history
	userBResp1, err := client.Get(server.URL + "/step1")
	if err != nil {
		t.Fatalf("User B step 1 failed: %v", err)
	}
	userBCookies := userBResp1.Cookies()
	userBResp1.Body.Close()

	// Wait for background prefetcher to speculatively execute /step2
	time.Sleep(150 * time.Millisecond)

	// User B visits /step2; request should be served from cache
	reqB2, _ := http.NewRequest("GET", server.URL+"/step2", nil)
	for _, c := range userBCookies {
		reqB2.AddCookie(c)
	}
	userBResp2, err := client.Do(reqB2)
	if err != nil {
		t.Fatalf("User B step 2 failed: %v", err)
	}
	defer userBResp2.Body.Close()

	if userBResp2.Header.Get("X-Cache") != "HIT" {
		t.Errorf("expected User B's /step2 to be an online pre-warmed HIT, got: %s", userBResp2.Header.Get("X-Cache"))
	}
}

func TestPrefetcherSafeShutdownUnderLoad(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/item", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"item":1}`))
	})

	store := precog.NewMemoryStore()
	prefetcher := precog.NewPrefetcher(mux, store, nil, time.Minute, 0.5, 4)

	var wg sync.WaitGroup
	workers := 10

	// Multiple goroutines continuously submit prefetch jobs
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", "/api/item", nil)
			for j := 0; j < 50; j++ {
				prefetcher.Submit("sess_test", []precog.Prediction{
					{Call: "GET /api/item", Probability: 0.9},
				}, req)
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	// Concurrently close the prefetcher
	time.Sleep(10 * time.Millisecond)
	prefetcher.Close()

	// Calling Close() again must be safe and idempotent
	prefetcher.Close()

	wg.Wait()
}

func TestResponseHeadersPreserved(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/headers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"33a64df551425fcc55e4d42a148795d9f25f89d4"`)
		w.Header().Set("X-Custom-Trace", "trace-xyz-123")
		_, _ = w.Write([]byte(`{"data":"headers"}`))
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

	// 1st request (MISS)
	resp1, err := client.Get(server.URL + "/api/headers")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if resp1.Header.Get("ETag") != `"33a64df551425fcc55e4d42a148795d9f25f89d4"` {
		t.Errorf("missing ETag on MISS: %s", resp1.Header.Get("ETag"))
	}
	if resp1.Header.Get("X-Custom-Trace") != "trace-xyz-123" {
		t.Errorf("missing X-Custom-Trace on MISS: %s", resp1.Header.Get("X-Custom-Trace"))
	}
	resp1.Body.Close()

	// 2nd request (HIT) - headers MUST be preserved from cache
	resp2, err := client.Get(server.URL + "/api/headers")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache: HIT, got %s", resp2.Header.Get("X-Cache"))
	}
	if resp2.Header.Get("ETag") != `"33a64df551425fcc55e4d42a148795d9f25f89d4"` {
		t.Errorf("missing or incorrect ETag on HIT: %s", resp2.Header.Get("ETag"))
	}
	if resp2.Header.Get("X-Custom-Trace") != "trace-xyz-123" {
		t.Errorf("missing or incorrect X-Custom-Trace on HIT: %s", resp2.Header.Get("X-Custom-Trace"))
	}
}

func TestAuthenticatedRequestNotSharedInCache(t *testing.T) {
	var handlerCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&handlerCalls, 1)
		auth := r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if auth == "Bearer user_a_token" {
			_, _ = w.Write([]byte(`{"user":"UserA","secret":"secretA"}`))
		} else {
			_, _ = w.Write([]byte(`{"user":"Anonymous"}`))
		}
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

	// User A requests /api/profile with Authorization header
	reqA, _ := http.NewRequest("GET", server.URL+"/api/profile", nil)
	reqA.Header.Set("Authorization", "Bearer user_a_token")
	respA, err := client.Do(reqA)
	if err != nil {
		t.Fatalf("User A request failed: %v", err)
	}
	respA.Body.Close()

	// User B visits /api/profile with NO Authorization header
	reqB, _ := http.NewRequest("GET", server.URL+"/api/profile", nil)
	respB, err := client.Do(reqB)
	if err != nil {
		t.Fatalf("User B request failed: %v", err)
	}
	defer respB.Body.Close()

	var data map[string]string
	_ = json.NewDecoder(respB.Body).Decode(&data)

	// User B MUST NOT see User A's private data!
	if data["user"] == "UserA" || data["secret"] == "secretA" {
		t.Fatalf("CRITICAL SECURITY LEAK: User B received User A's cached private data: %v", data)
	}
}


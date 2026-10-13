package precog_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	// 2nd request -> Cache HIT (served from RAM, handler never called again)
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

	// Allow speculative background worker 150ms to warm /api/cart in RAM
	time.Sleep(150 * time.Millisecond)

	// User now clicks /api/cart: Should be an instant CACHE HIT!
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

	// --- PHASE 1: User A trains the model naturally through live traffic ---
	// User A visits /step1
	userAResp1, err := client.Get(server.URL + "/step1")
	if err != nil {
		t.Fatalf("User A step 1 failed: %v", err)
	}
	userACookies := userAResp1.Cookies()
	userAResp1.Body.Close()

	// User A visits /step2 using their session cookie
	reqA2, _ := http.NewRequest("GET", server.URL+"/step2", nil)
	for _, c := range userACookies {
		reqA2.AddCookie(c)
	}
	userAResp2, err := client.Do(reqA2)
	if err != nil {
		t.Fatalf("User A step 2 failed: %v", err)
	}
	userAResp2.Body.Close()

	// --- PHASE 2: New User B arrives (zero history) ---
	userBResp1, err := client.Get(server.URL + "/step1")
	if err != nil {
		t.Fatalf("User B step 1 failed: %v", err)
	}
	userBCookies := userBResp1.Cookies()
	userBResp1.Body.Close()

	// Wait 150ms for background prefetcher to speculatively execute /step2 for User B
	time.Sleep(150 * time.Millisecond)

	// User B visits /step2: Should be an instant CACHE HIT!
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


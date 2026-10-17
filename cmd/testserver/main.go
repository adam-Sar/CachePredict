// Command testserver is a self-contained integration target for verifying
// Precog behavior over real HTTP: cache HIT/MISS, speculative pre-warming,
// request coalescing, POST pass-through, and Set-Cookie exclusion.
//
// Run it, then execute scripts/smoke.ps1 (or hit it manually with curl).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/adam-Sar/precog"
)

// Handler call counters — exposed via /stats so the smoke script can verify
// that the upstream handler runs exactly when expected (never on cache hits,
// once per coalesced burst).
var (
	callsProduct  atomic.Int64
	callsCart     atomic.Int64
	callsCheckout atomic.Int64
	callsPrivate  atomic.Int64
	callsSlow     atomic.Int64
)

func main() {
	port := flag.String("port", "8099", "listen port")
	flag.Parse()

	// Seeded transitions so speculative pre-warming works immediately,
	// without needing traffic to train the model first.
	predictor := precog.NewAdaptiveMarkovPredictor()
	predictor.SeedTransition("GET /api/products/:id", "GET /api/cart", 10)
	predictor.SeedTransition("GET /api/cart", "GET /api/checkout", 6)

	engine, err := precog.New(precog.Config{
		Predictor:           predictor,
		ConfidenceThreshold: 0.5,
		TTL:                 30 * time.Second,
		Workers:             4,
		SkipRoutes:          []string{"/stats"},
	})
	if err != nil {
		log.Fatalf("failed to init precog: %v", err)
	}
	defer engine.Close()

	mux := http.NewServeMux()

	// 120ms simulated DB latency: cache hits are visibly instant.
	mux.HandleFunc("/api/products/42", func(w http.ResponseWriter, r *http.Request) {
		n := callsProduct.Add(1)
		log.Printf("HANDLER /api/products/42 call #%d", n)
		time.Sleep(120 * time.Millisecond)
		writeJSON(w, `{"id":42,"name":"Skirt","price":49.99}`)
	})

	mux.HandleFunc("/api/cart", func(w http.ResponseWriter, r *http.Request) {
		n := callsCart.Add(1)
		log.Printf("HANDLER /api/cart call #%d", n)
		time.Sleep(120 * time.Millisecond)
		writeJSON(w, `{"cart":"active","items":0}`)
	})

	// POST: never cached, must pass straight through every time.
	// Non-POST requests (e.g. speculative GETs from the prefetcher) are rejected.
	mux.HandleFunc("/api/checkout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		n := callsCheckout.Add(1)
		log.Printf("HANDLER POST /api/checkout call #%d", n)
		body, _ := io.ReadAll(r.Body)
		time.Sleep(120 * time.Millisecond)
		writeJSON(w, fmt.Sprintf(`{"status":"checked_out","bytes":%d,"call":%d}`, len(body), n))
	})

	// Sets a private cookie on every call: must NEVER be shared-cached.
	mux.HandleFunc("/api/private", func(w http.ResponseWriter, r *http.Request) {
		n := callsPrivate.Add(1)
		log.Printf("HANDLER /api/private call #%d", n)
		http.SetCookie(w, &http.Cookie{Name: "session", Value: fmt.Sprintf("token-%d", n)})
		writeJSON(w, fmt.Sprintf(`{"token":"token-%d"}`, n))
	})

	// 300ms latency: long window to observe in-flight coalescing.
	mux.HandleFunc("/api/slow", func(w http.ResponseWriter, r *http.Request) {
		n := callsSlow.Add(1)
		log.Printf("HANDLER /api/slow call #%d", n)
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, `{"slow":true}`)
	})

	// Excluded from caching via SkipRoutes; reports live counters.
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, mustJSON(map[string]any{
			"engine": engine.Stats(),
			"handler_calls": map[string]int64{
				"/api/products/42": callsProduct.Load(),
				"/api/cart":        callsCart.Load(),
				"/api/checkout":    callsCheckout.Load(),
				"/api/private":     callsPrivate.Load(),
				"/api/slow":        callsSlow.Load(),
			},
		}))
	})

	addr := ":" + *port
	log.Printf("precog test server listening on %s (routes: /api/products/42, /api/cart, /api/checkout, /api/private, /api/slow, /stats)", addr)
	log.Fatal(http.ListenAndServe(addr, engine.Handler(mux)))
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

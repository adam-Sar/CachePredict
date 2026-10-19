# Precog

[![Go Reference](https://pkg.go.dev/badge/github.com/adam-Sar/precog.svg)](https://pkg.go.dev/github.com/adam-Sar/precog)
[![Go Report Card](https://goreportcard.com/badge/github.com/adam-Sar/precog)](https://goreportcard.com/report/github.com/adam-Sar/precog)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Precog** is a predictive caching middleware for Go. It caches API responses in a low-overhead in-memory store, coalesces concurrent duplicate requests, learns navigation patterns with a Markov predictor, and speculatively pre-warms the responses users are likely to need next — on the server and in the browser.

---

## What it does

- **Caches safe responses** (`GET`/`HEAD`) in a two-tier store: a lock-free direct-mapped L1 (atomic pointer slots, ~5 ns hits, zero allocations) backed by a pointer-free sharded byte-arena L2 (256 shards, no GC scanning of the index).
- **Coalesces concurrent requests** for the same resource, so 50 simultaneous cache misses trigger exactly one handler execution (one database call).
- **Predicts the next request** per session using a dense lock-free transition matrix and **pre-warms** predicted endpoints in background workers.
- **Accelerates the browser**: serves an embedded client SDK (`precog.js`) that runs the same predictor as WebAssembly, pre-fetches into the browser's native `CacheStorage`, and injects W3C Speculation Rules.

## Installation

```bash
go get github.com/adam-Sar/precog
```

## Quick Start

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/adam-Sar/precog"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items": ["item1", "item2"]}`))
	})

	engine, err := precog.New(precog.Config{
		TTL:                 5 * time.Minute,
		ConfidenceThreshold: 0.60,
	})
	if err != nil {
		log.Fatalf("failed to init precog: %v", err)
	}
	defer engine.Close()

	log.Println("Server running on :8080 with precog")
	http.ListenAndServe(":8080", engine.Handler(mux))
}
```

Wrap your router with `engine.Handler(mux)` — it works with `net/http`, Chi, Echo, Gin, or anything that speaks `http.Handler`.

Responses carry an `X-Cache: HIT|MISS` header so you can observe what's happening.

## Caching policy

Precog only caches when it is safe to do so:

- Only `GET`/`HEAD` requests are cached or coalesced; everything else passes straight through.
- Only `200 OK` responses are stored.
- Responses with `Set-Cookie` are never shared-cached (RFC 9111 §3.5).
- `Cache-Control: no-store` and `private` are respected. Responses to authenticated requests (`Authorization`, API tokens, or session cookies) are only cached when the handler explicitly declares `Cache-Control: public`.
- Original response headers (ETag, Content-Type, custom headers) are preserved on hits.
- Predicted endpoints are advertised to browsers via `Link: <url>; rel=prefetch` headers.

## Sessions

Precog tracks navigation per session to drive predictions. A session is resolved in this order:

1. `SessionExtractor` from `Config`, if you provide one (e.g. read a JWT claim or custom header).
2. The cookie named by `SessionCookieName` (default `precog_sid`, set automatically).
3. The `X-Session-ID` request header.

## Configuration

```go
engine, err := precog.New(precog.Config{
	// Minimum Markov confidence (0.0-1.0) to trigger background prefetch. Default: 0.65
	ConfidenceThreshold: 0.65,

	// How long cached responses stay valid. Default: 60s
	TTL: 5 * time.Minute,

	// Custom store backend. Default: two-tier L1 + sharded L2.
	Store: precog.NewTieredStore(precog.NewShardedStore(&precog.ShardedConfig{
		NumShards:    256,
		MaxCostBytes: 256 * 1024 * 1024,
	})),

	// Custom predictor. Default: DenseMatrixPredictor (max 1024 routes).
	Predictor: precog.NewDenseMatrixPredictor(&precog.DensePredictorConfig{
		MaxRoutes: 1024,
	}),

	// Cookie name for session tracking. Default: "precog_sid"
	SessionCookieName: "precog_sid",

	// Set the Secure flag on the session cookie (use behind HTTPS).
	CookieSecure: true,

	// Override session resolution (e.g. from a JWT or custom header).
	SessionExtractor: func(r *http.Request) string { return r.Header.Get("X-User") },

	// Optional custom authentication detector. By default, requests with Authorization,
	// Proxy-Authorization, API token headers, or cookies other than SessionCookieName
	// are treated as authenticated and will not receive/populate shared cache without
	// an explicit Cache-Control: public header.
	IsAuthenticated: func(r *http.Request) bool {
		return r.Header.Get("Authorization") != "" || r.Header.Get("X-User-Token") != ""
	},

	// Whitelist specific cookie names that represent authenticated sessions.
	// If empty, any cookie other than SessionCookieName is considered an auth credential.
	AuthCookieNames: []string{"session_id", "jwt"},

	// Path prefixes never cached or prefetched.
	SkipRoutes: []string{"/healthz", "/metrics", "/admin"},

	// Concurrent speculative prefetch workers. Default: 4
	Workers: 8,
})
```

## Browser SDK

Precog serves its client SDK at `/precog/precog.js`. Include it in your HTML:

```html
<script src="/precog/precog.js" defer></script>
```

The SDK automatically:

1. Loads the WebAssembly predictor from `/precog/precog.wasm` (the Go runtime glue at `/precog/wasm_exec.js` is served too; if the module isn't deployed, a lightweight JS tracker is used instead).
2. Intercepts `fetch()` calls and link hovers to learn navigation patterns.
3. Pre-fetches predicted URLs into the browser's `CacheStorage` for zero-latency repeat requests.
4. Injects W3C Speculation Rules for Chromium-native prefetching.

A minimal JS fallback predictor is exposed as `window.Precog.record/predict/clearCache`.

## Compiling the WebAssembly module

```bash
# Windows
.\build_wasm.bat

# Linux / macOS
./build_wasm.sh
```

Place the resulting `precog.wasm` in your server's working directory; Precog serves it at `/precog/precog.wasm` with the correct `application/wasm` content type.

## Custom stores and predictors

Implement the `Store` interface to plug in your own backend (Redis, disk, etc.):

```go
type Store interface {
	Get(key string) (*ResponseEntry, bool)
	Set(key string, entry *ResponseEntry, ttl time.Duration) bool
	Del(key string)
	Close() error
}
```

Built-in options: `NewTieredStore` (default), `NewShardedStore`, `NewMemoryStore`, `NewRistrettoStore` (TinyLFU eviction).

Implement the `Predictor` interface for custom prediction logic. `NewAdaptiveMarkovPredictor` is a map-based alternative to the default dense matrix, and supports `SeedTransition(from, to, weight)` to bootstrap known flows:

```go
p := precog.NewAdaptiveMarkovPredictor()
p.SeedTransition("GET /api/products/:id", "GET /api/cart", 10)
```

## Telemetry

```go
stats := engine.Stats()
fmt.Printf("Hit rate: %.2f%%\n", stats.HitRate*100)
fmt.Printf("Prefetches executed: %d\n", stats.PrefetchesExecuted)
fmt.Printf("Cache hits: %d / %d requests\n", stats.CacheHits, stats.TotalRequests)
```

## Testing your integration

The repo ships a self-contained instrumented server (`cmd/testserver`) with fake DB latency and handler-call counters, plus an end-to-end smoke suite:

```bash
go run ./cmd/testserver                # routes on :8099
powershell -ExecutionPolicy Bypass -File scripts\smoke.ps1
```

The smoke suite asserts caching, pre-warming, coalescing, POST pass-through and Set-Cookie isolation over real HTTP — a useful template for verifying your own deployment.

## Benchmarks

Measured on an Intel Core i7-1165G7 @ 2.80GHz (`go test -bench . -benchmem`):

| Benchmark Component | Speed / Op | Allocations | Memory / Op |
| :--- | :--- | :--- | :--- |
| **TieredStore L1 Atomic Hit** | **4.67 ns/op** | **0 allocs/op** | **0 B/op** |
| **EndpointRegistry Route Lookup** | **8.16 ns/op** | **0 allocs/op** | **0 B/op** |
| **FastHash64 (Bare Path)** | **32.5 ns/op** | **0 allocs/op** | **0 B/op** |
| **FastHash64 (Multi-Param Query)** | **221.2 ns/op** | **1 allocs/op** | **64 B/op** |
| **ShardedStore Parallel Read (256 Shards)**| **107.4 ns/op** | **5 allocs/op** | **199 B/op** |
| **Dense Matrix Predictor Query** | **386.1 ns/op** | **6 allocs/op** | **280 B/op** |
| **Dense Matrix Lock-Free Record** | **528.5 ns/op** | **13 allocs/op** | **295 B/op** |

## License

MIT License. See [LICENSE](LICENSE) for details.

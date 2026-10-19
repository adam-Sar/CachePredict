# Precog

[![Go Reference](https://pkg.go.dev/badge/github.com/adam-Sar/precog.svg)](https://pkg.go.dev/github.com/adam-Sar/precog)
[![Go Report Card](https://goreportcard.com/badge/github.com/adam-Sar/precog)](https://goreportcard.com/report/github.com/adam-Sar/precog)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Precog** is a predictive caching middleware for Go. It caches API responses in a low-overhead in-memory store, coalesces concurrent duplicate requests, learns navigation patterns with a Markov predictor, and speculatively pre-warms the responses users are likely to need next, both on the server and in the browser.

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

Wrap your router with `engine.Handler(mux)`. It works with `net/http`, Chi, Echo, Gin, or anything that speaks `http.Handler`.

Responses carry an `X-Cache: HIT|MISS` header so you can observe what's happening.

## Caching policy & Security boundaries

Precog only caches and coalesces when it is provably safe to do so:

- **Method Safety**: Only `GET`/`HEAD` requests are cached or coalesced; all other methods pass straight through.
- **Status & Headers**: Only `200 OK` responses are stored. Responses with `Set-Cookie` are **never** shared-cached (RFC 9111 §3.5) and are never shared across coalesced callers.
- **Authentication Isolation**: Requests carrying credentials (`Authorization`, API tokens, or session cookies) bypass the shared cache and in-flight coalescing by default. They can only be shared if declared in `PublicRoutes` or if the upstream handler explicitly declares `Cache-Control: public`.
- **Cross-User Coalescing Defense**: Private or unshareable responses are never delivered to waiting followers; waiters automatically fall back to independent upstream execution.
- **Client Directives**: Client `Cache-Control: no-cache, no-store, max-age=0` and `Pragma: no-cache` are fully respected.
- **Virtual Host Isolation**: Cache keys incorporate the request `Host` to prevent cross-tenant cache pollution in multi-tenant environments.
- **Vary Header Support**: RFC 9111 §4.1 compliant secondary keying. Responses with `Vary: *` are never cached. Responses with `Vary: Accept-Encoding` or other headers are stored under variant keys and verified on subsequent lookups.
- **Streaming & SSE**: Server-Sent Events (`text/event-stream`), WebSockets, chunked transfers, and `http.Flusher`/`http.Hijacker` interfaces bypass caching and stream without buffering.
- **Oversized Response Protection**: Responses exceeding `MaxResponseSize` (default: 2MB) stream through directly to protect server memory.

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

	// Path prefixes guaranteed to be public catalog/reference data.
	// Eligible for shared caching and coalescing even if authentication headers exist.
	PublicRoutes: []string{"/api/catalog", "/api/products"},

	// Maximum response body size eligible for in-memory caching.
	// Responses larger than this bypass caching and stream directly. Default: 2MB
	MaxResponseSize: 2 * 1024 * 1024,

	// Isolated coalescing for authenticated requests (scoped strictly per-user).
	// Default: false (authenticated requests bypass coalescing for strict security).
	CoalesceAuthenticated: false,

	// Cookie name for session tracking. Default: "precog_sid"
	SessionCookieName: "precog_sid",

	// Set the Secure flag on the session cookie (use behind HTTPS).
	CookieSecure: true,

	// Override session resolution (e.g. from a JWT or custom header).
	SessionExtractor: func(r *http.Request) string { return r.Header.Get("X-User") },

	// Optional custom authentication detector. By default, requests with Authorization,
	// Proxy-Authorization, API token headers, or cookies other than SessionCookieName
	// are treated as authenticated.
	IsAuthenticated: func(r *http.Request) bool {
		return r.Header.Get("Authorization") != "" || r.Header.Get("X-User-Token") != ""
	},

	// Whitelist specific cookie names that represent authenticated sessions.
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

1. Loads the WebAssembly predictor from `/precog/precog.wasm` (or runs the lightweight JS tracker fallback).
2. Intercepts `fetch()` calls and link hovers to learn navigation patterns.
3. Pre-fetches predicted URLs into the browser's `CacheStorage` for zero-latency repeat requests.
4. Strictly inspects response `Cache-Control` (`private` and `no-store` responses and `Set-Cookie` are **never** stored in browser `CacheStorage`).
5. Injects W3C Speculation Rules for Chromium-native prefetching.
6. Honors `data-precog-skip` and `rel="nofollow"` attributes on links to prevent speculative prefetching.
7. Exposes `window.Precog.reset()` to invalidate all browser cache entries upon user logout.

## Benchmarks

Measured on an Intel Core i7-1165G7 @ 2.80GHz (`go test -bench . -benchmem`):

### Core Caching & Modeling Primitives
| Benchmark Component | Speed / Op | Allocations | Memory / Op |
| :--- | :--- | :--- | :--- |
| **TieredStore L1 Atomic Hit** | **4.50 ns/op** | **0 allocs/op** | **0 B/op** |
| **EndpointRegistry Route Lookup** | **7.41 ns/op** | **0 allocs/op** | **0 B/op** |
| **FastHash64 (Bare Path)** | **33.37 ns/op** | **0 allocs/op** | **0 B/op** |
| **FastHash64 (Multi-Param Query)** | **188.2 ns/op** | **1 allocs/op** | **64 B/op** |
| **ShardedStore Parallel Read (256 Shards)**| **97.55 ns/op** | **5 allocs/op** | **199 B/op** |
| **Dense Matrix Predictor Query** | **508.7 ns/op** | **6 allocs/op** | **280 B/op** |
| **Dense Matrix Lock-Free Record** | **864.0 ns/op** | **13 allocs/op** | **295 B/op** |

### Complete End-to-End HTTP Middleware Stack
| End-to-End HTTP Benchmark | Speed / Op | Throughput | Allocations / Op |
| :--- | :--- | :--- | :--- |
| **HTTP Middleware Parallel Cache Hits** | **2.08 µs/op** | **~480,000 req/sec** | **30 allocs/op (1640 B)** |
| **HTTP Middleware Single Cache Hit** | **5.00 µs/op** | **~200,000 req/sec** | **30 allocs/op (1661 B)** |
| **HTTP In-Flight Request Coalescing** | **4.81 µs/op** | **~207,000 req/sec** | **39 allocs/op (6788 B)** |

## License

MIT License. See [LICENSE](LICENSE) for details.

# Precog

[![Go Reference](https://pkg.go.dev/badge/github.com/adam-Sar/precog.svg)](https://pkg.go.dev/github.com/adam-Sar/precog)
[![Go Report Card](https://goreportcard.com/badge/github.com/adam-Sar/precog)](https://goreportcard.com/report/github.com/adam-Sar/precog)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Precog** is a universal, ultra-fast speculative API pre-warming and caching engine for Go. It predicts subsequent user actions in real-time, pre-cooks wire-ready HTTP responses, and delivers zero-latency ($0\text{ ms}$) client-side navigations using WebAssembly, Browser `CacheStorage`, and W3C Speculation Rules.

---

## Highlights & Performance Architecture

- ⚡ **Lock-Free Route Indexing (`EndpointRegistry`)**: Maps arbitrary dynamic URL routes (`GET /api/v1/items/:id`) to `uint16` IDs via lock-free atomic Copy-On-Write ($< 8.5\text{ ns}$ lookups).
- 🔒 **Zero-GC 256-Shard Store (`BigCache` Architecture)**: Uses a pointer-free `map[uint64]uint32` index and contiguous byte ring buffers. Because the index contains zero pointers, the Go Garbage Collector completely skips it during STW and mark phases, preventing memory ballooning.
- 🚀 **Two-Tier Caching (Atomic L1 + Sharded L2)**: Direct-mapped lock-free atomic L1 cache delivering **~7.0 ns** hits with **0 locks and 0 allocations**, backed by a 256-striped global L2 store.
- 🌐 **WebAssembly Browser Predictor**: Runs the predictor directly in the user's browser via WebAssembly (`precog.wasm`), pre-fetching into native Browser `CacheStorage` (`window.caches`) for instant $0\text{ ms}$ navigations.
- 🎯 **W3C Speculation Rules API**: Injects browser-native `<script type="speculationrules">` and HTTP `Link: <url>; rel=prefetch` headers for speculative Chromium pre-rendering.
- 🧬 **Lock-Free Dense Contiguous Transition Matrix**: Replaces pointer-heavy Markov graphs with a flat 1D contiguous `uint32` array updated via atomic CPU instructions (`atomic.AddUint32`) with zero locks and zero allocations.
- 🔑 **Fast xxHash64 Keying**: Sub-microsecond non-cryptographic 64-bit hashing replacing slow SHA-256.
- 🧩 **100% Universal**: Drop-in middleware compatible with standard `net/http`, Echo, Chi, Gin, or any Go web framework.

---

## Architectural Flow

```
+-----------------------------------------------------------------------------------+
|                                 USER BROWSER                                      |
|                                                                                   |
|   User Navigation / Hover / Click                                                 |
|          |                                                                        |
|          v                                                                        |
|   precog.js Interceptor  <====>  precog.wasm (Dense Predictor)                    |
|          |                                                                        |
|          +---> Browser CacheStorage (caches.open) ---> 0ms Instant Client Hit     |
|          +---> W3C Speculation Rules (<script type="speculationrules">)           |
+-----------------------------------------------------------------------------------+
                                   |
                      HTTP Request | (Network / Background Prefetch)
                                   v
+-----------------------------------------------------------------------------------+
|                             GO BACKEND SERVER                                     |
|                                                                                   |
|  precog.Middleware()                                                              |
|    |                                                                              |
|    |---> 1. Fast xxHash64 Keying                                                  |
|    |                                                                              |
|    |---> 2. Tier 1 (L1): Lock-Free Direct-Mapped Atomic Cache  [ ~7.0 ns Hit ]    |
|    |                                                                              |
|    |---> 3. Tier 2 (L2): Zero-GC 256-Shard Byte Arena Store    [ ~200 ns Hit ]    |
|    |                                                                              |
|    |---> 4. On Hit: Cached Response Replay                                        |
|    |                                                                              |
|    +---> 5. On Miss: Execute Route Handler                                        |
|              |                                                                    |
|              +---> Lock-Free Dense Transition Matrix (atomic.AddUint32)           |
|              +---> Speculative Background Pre-warming                             |
+-----------------------------------------------------------------------------------+
```

---

## Benchmarks

Micro-benchmarks run on Intel Core i7-1165G7 @ 2.80GHz:

| Benchmark Component | Speed / Op | Allocations | Memory / Op |
| :--- | :--- | :--- | :--- |
| **TieredStore L1 Atomic Hit** | **7.08 ns/op** | **0 allocs/op** | **0 B/op** |
| **EndpointRegistry Route Lookup** | **8.50 ns/op** | **0 allocs/op** | **0 B/op** |
| **FastHash64 (Bare Path)** | **49.28 ns/op** | **1 allocs/op** | **48 B/op** |
| **ShardedStore Parallel Read (256 Shards)**| **209.0 ns/op** | **5 allocs/op** | **295 B/op** |
| **Dense Matrix Predictor Query** | **397.5 ns/op** | **5 allocs/op** | **248 B/op** |
| **Dense Matrix Lock-Free Record** | **650.8 ns/op** | **13 allocs/op** | **294 B/op** |

---

## Installation

```bash
go get github.com/adam-Sar/precog
```

---

## Quick Start

### 1. Backend Integration (`net/http`)

Wrap your HTTP handler or router with `engine.Handler`:

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
		w.Write([]byte(`{"items": ["item1", "item2"]}`))
	})

	mux.HandleFunc("/api/checkout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "ready"}`))
	})

	// Initialize Precog with default Zero-GC TieredStore and Dense Predictor
	engine, err := precog.New(precog.Config{
		TTL:                 5 * time.Minute,
		ConfidenceThreshold: 0.60,
	})
	if err != nil {
		log.Fatalf("failed to init precog: %v", err)
	}
	defer engine.Close()

	log.Println("Server running on :8080 with Precog predictive acceleration")
	http.ListenAndServe(":8080", engine.Handler(mux))
}
```

### 2. Client-Side 0ms Browser Acceleration

Precog automatically serves its embedded browser SDK at `/precog/precog.js`. Simply include the script in your HTML template:

```html
<!DOCTYPE html>
<html>
<head>
    <title>My Fast App</title>
    <!-- Automatically served by Precog backend -->
    <script src="/precog/precog.js" defer></script>
</head>
<body>
    <nav>
        <a href="/api/items">Items</a>
        <a href="/api/checkout">Checkout</a>
    </nav>
</body>
</html>
```

`precog.js` will automatically:
1. Intercept link hovers and navigation.
2. Predict upcoming API requests.
3. Speculatively pre-warm them into the browser's native `CacheStorage`.
4. Inject W3C Speculation Rules into the document head.
5. Serve subsequent `fetch()` requests directly from browser cache with **0ms latency**.

---

## Configuration Options

```go
engine, err := precog.New(precog.Config{
    // Minimum Markov confidence required to trigger background prefetch (0.0 - 1.0)
    ConfidenceThreshold: 0.65,

    // Time-to-live for cached response entries
    TTL: 5 * time.Minute,

    // Custom backend store (defaults to Two-Tier L1 atomic + L2 ShardedStore)
    Store: precog.NewTieredStore(precog.NewShardedStore(&precog.ShardedConfig{
        NumShards:    256,
        MaxCostBytes: 256 * 1024 * 1024, // 256 MB
    })),

    // Custom predictor (defaults to lock-free DenseMatrixPredictor)
    Predictor: precog.NewDenseMatrixPredictor(&precog.DensePredictorConfig{
        MaxRoutes: 1024,
    }),

    // Cookie name for tracking session navigation flows
    SessionCookieName: "precog_sid",

    // Concurrency workers for server-side speculative hydrations
    Workers: 8,

    // Routes to exclude from predictive caching
    SkipRoutes: []string{"/healthz", "/metrics", "/auth/login"},
})
```

---

## Compiling WebAssembly

The client WebAssembly binary (`precog.wasm`) can be compiled using the included build scripts:

```bash
# Windows
.\build_wasm.bat

# Linux / macOS
./build_wasm.sh
```

When placed in the root directory, Precog serves `precog.wasm` at `/precog/precog.wasm` with `application/wasm` headers.

---

## Telemetry & Metrics

Inspect real-time predictive cache metrics:

```go
stats := engine.Stats()
fmt.Printf("Hit Rate: %.2f%%\n", stats.HitRate * 100)
fmt.Printf("Prefetches Triggered: %d\n", stats.PrefetchesExecuted)
fmt.Printf("Hits from Cache: %d\n", stats.CacheHits)
```

---

## License

MIT License. See [LICENSE](LICENSE) for details.
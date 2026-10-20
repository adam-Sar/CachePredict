# Changelog

All notable changes to Precog will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v1.1.0] - 2026-10-20

### Security
- **Cross-User Request Coalescing Isolation**:
  - Authenticated requests carrying credentials (`Authorization`, proxy headers, API tokens, or session cookies) are completely isolated from shared in-flight coalescing by default.
  - Added double-layer response defense: unshareable responses (`Set-Cookie`, `Cache-Control: private`, `no-store`, non-200 codes) are strictly rejected from follower delivery; waiting followers automatically fall back to independent upstream execution.
  - Added optional `CoalesceAuthenticated` config to allow isolated coalescing strictly scoped per-user.
- **Browser CacheStorage Hardening**:
  - Strictly enforce RFC 9111 cacheability checks before placing entries in browser `CacheStorage` (`precog.js`). Responses with `Cache-Control: private`, `no-store`, or `Set-Cookie` are rejected from client-side persistent storage.
  - Added `window.Precog.reset()` to invalidate client-side `CacheStorage` and session navigation history upon user logout.
  - Added support for `data-precog-skip` and `rel="nofollow"` on HTML anchor tags to prevent accidental speculative prefetching of sensitive endpoints.

### HTTP Conformance
- **Host & Multi-Tenant Isolation**:
  - Cache keys now incorporate the request `Host` via `CanonicalCacheKeyWithHost`, preventing cross-tenant cache pollution in multi-tenant environments.
- **RFC 9111 Vary Header Compliance**:
  - Added secondary cache keying based on response `Vary` headers (e.g. `Vary: Accept-Encoding, Accept`).
  - Stored responses record client request header values and verify matching headers on subsequent lookups via `MatchesVary(r)`.
  - Responses declaring `Vary: *` are strictly excluded from shared caching.
- **Request Cache Directives**:
  - Middleware now respects client `Cache-Control: no-cache, no-store, max-age=0` and `Pragma: no-cache` by bypassing cache lookups.
- **Protocol & Streaming Compatibility**:
  - Replaced blind `httptest.NewRecorder` buffering with `capturingResponseWriter`, supporting `http.Flusher`, `http.Hijacker`, and Go 1.20+ `http.ResponseController.Unwrap()`.
  - Server-Sent Events (`text/event-stream`), WebSockets, and chunked streaming responses bypass caching and stream immediately to clients without buffering.
  - Added `MaxResponseSize` (default: 2MB) to stream oversized responses directly and protect server memory.
  - Bypassed caching for HTTP Range requests (`Range` header).

### Concurrency & Reliability
- **In-flight Waiter Context Cancellation**:
  - Replaced `sync.WaitGroup` in `InflightGroup` with channel-based completion (`c.done`), allowing waiting callers with a cancelled `context.Context` to unblock immediately without waiting for a slow leader.
- **Predictor Counter Overflow Protection**:
  - Added row counter saturation guards and automatic rescaling (`rescaleRow`) in `DenseMatrixPredictor` when transition totals approach uint32 limits, preventing arithmetic overflow while preserving Markov probabilities.
- **Synthetic Prefetch Request Fidelity**:
  - Prefetch background jobs now capture and preserve original request `Host` and `Scheme` on synthetic loopback requests.

### CI/CD
- Added GitHub Actions workflow (`.github/workflows/ci.yml`) testing Linux, macOS, and Windows across Go 1.21, 1.22, and 1.23 with race detection and WebAssembly verification.

---

## [v1.0.0] - 2026-10-19

### Initial Release
- Two-tier cache hierarchy (`TieredStore`): atomic pointer direct-mapped L1 cache backed by 256-shard byte-arena L2 (`ShardedStore`).
- Dense contiguous Markov transition matrix predictor (`DenseMatrixPredictor`).
- Sharded in-flight request coalescer (`InflightGroup`).
- WebAssembly browser client SDK (`precog.js`, `precog.wasm`) and W3C Speculation Rules injection.
- Telemetry and statistics reporting.

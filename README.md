# CachePredict

A Go HTTP API in front of a Supabase product catalogue with a twist: every request goes through middleware that predicts the next call via an LSTM and pre-warms an in-memory cache (Ristretto) so the next request hits RAM instead of Postgres.

The predictor is an ONNX model (`cache_predict_model.onnx`) that tokenises each request as `METHOD /path[?query][ body=…]` and emits a probability distribution over a 142-token vocabulary.

---

## Request Flow

```
                ┌─────────────────────────────────────┐
 client ──►     │  echo middleware (PrefetchMiddleware)│
                │  1. read session_id cookie           │
                │  2. append call to session history   │
                │  3. compute cache key                │
                │  4. HIT  → writeCached() → return    │
                │  5. MISS → handler → cache.SetWithTTL │
                │  6. spawn goroutine: prefetch()      │
                └────────────┬────────────────────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
        handlers.go     predictor.go    session.go
        (real work)     (LSTM inference) (history)
                             │
                             ▼
                       predictor_onnx.go
                       (ONNX Runtime)
```

---

## Quick Start

```bash
# .env is required (see Configuration below)
go build -o app.exe .
./app.exe            # listens on :1234
```

Two routes are wired through the prefetch registry; the rest are honest HTTP handlers with no prediction. Test sequence once it's up:

```bash
BASE=http://localhost:1234
rm -f /tmp/cj.txt

curl -s -c /tmp/cj.txt -b /tmp/cj.txt -D - "$BASE/products"            -o /dev/null | grep -i x-cache   # MISS
sleep 0.3
curl -s -c /tmp/cj.txt -b /tmp/cj.txt -D - "$BASE/products?id=88"     -o /dev/null | grep -i x-cache   # HIT (top prediction)
curl -s -c /tmp/cj.txt -b /tmp/cj.txt -D - "$BASE/products?id=105"    -o /dev/null | grep -i x-cache   # HIT
curl -s -c /tmp/cj.txt -b /tmp/cj.txt -D - "$BASE/products?id=33"     -o /dev/null | grep -i x-cache   # HIT
```

The first call seeds the session; the predictor fires in a goroutine, warms the top-3 predicted `GET /products?id=N` keys, and the next three calls all return `X-Cache: HIT` within `CACHE_TTL` (default 60s).

---

## File Map

| File | Role |
|---|---|
| `main.go` | Entry point — wires config, store, predictor, cache, sessions, registry, middleware, handlers |
| `config.go` | Loads `.env` into a typed `Config` struct |
| `cache.go` | `CacheKey()` — SHA-256 of canonical request signature |
| `middleware.go` | `PrefetchMiddleware`, `prefetch()`, recorder, session cookie, `PrefetchRegistry` |
| `handlers.go` | `GET /products`, `GET/POST /products/filter`, `GET /healthz` |
| `predictor.go` | Public predictor wrapper (RLock/Close), `Tokenizer`, `Encode` |
| `predictor_onnx.go` | ONNX Runtime backend (build tag `cgo && windows`) |
| `predictor_stub.go` | Deterministic non-CGO fallback (build tag `!cgo`) |
| `session.go` | LRU-bounded `SessionStore` keyed by `session_id` cookie |
| `store.go` | Supabase (PostgREST) HTTP client for products |

---

## How They Communicate

| File | Imports / Calls | Purpose |
|---|---|---|
| `main.go` | every other file | Bootstrap and DI |
| `middleware.go` | `cache.CacheKey`, `predictor.Predictor.Predict`, `session.SessionStore`, `store.Store` (via registry) | Hot path on every request |
| `predictor.go` | `predictor_onnx.go` or `predictor_stub.go` via build tag | Backend selection |
| `handlers.go` | `store.Store` | Reads/writes product data |
| `store.go` | net/http only | Talks to Supabase REST API |
| `cache.go` | stdlib only | Pure function, no dependencies |
| `session.go` | stdlib `container/list` | Self-contained LRU |

Dependency direction: `main` is the only file that imports everything; everything else is a leaf except `middleware`, which is the only consumer of both `predictor` and `session` and is itself only consumed by `main`.

---

## File-by-File

### `main.go`

Entry point. On startup:

1. Loads `.env` (`godotenv.Load()` — non-fatal if missing).
2. `LoadConfig()` — fatal on missing required vars.
3. Builds the four singletons: `Store`, `Predictor`, `ristretto.Cache`, `SessionStore`.
4. Builds the `PrefetchRegistry` and registers two handlers:
   - `GET /products` — branches on `?id=N` to return a single product or the full list.
   - `GET /products/filter` — returns `{"filters":{}}` placeholder.
5. Mounts `PrefetchMiddleware` on `echo.New()`.
6. Registers four routes: `GET /products`, `GET/POST /products/filter`, `GET /healthz`.
7. Starts the server via `signal.NotifyContext` for graceful shutdown.

> Only 2 of the 4 routes are in the prefetch registry. The model may predict `/cart`, `/checkout`, etc. (in the vocab) — those predictions are logged as `SKIP no-registered-handler` and silently dropped.

### `config.go`

| Field | Env var | Default |
|---|---|---|
| `SupabaseURL` | `SUPABASE_URL` | **required** |
| `SupabaseKey` | `SUPABASE_KEY` | **required** |
| `ImageBaseURL` | `IMAGE_BASE_URL` | **required** |
| `ONNXModelPath` | `ONNX_MODEL_PATH` | `cache_predict_model.onnx` |
| `TokenizerPath` | `TOKENIZER_PATH` | `tokenizer.json` |
| `CacheTTL` | `CACHE_TTL` (Go duration or seconds) | `60s` |
| `CacheMaxEntries` | `CACHE_MAX` | `500` |
| `Port` | `PORT` | `:1234` |
| `SessionMaxHist` | `SESSION_MAX_HIST` | `8` |
| `SessionMaxCount` | `SESSION_MAX_COUNT` | `10000` |
| `CookieSecure` | `COOKIE_SECURE` | `false` |
| `ShutdownTimeout` | `SHUTDOWN_TIMEOUT` | `10s` |

`parseCacheTTL` accepts either Go durations (`60s`, `5m`) or bare integers (`60` = 60s). `parseInt`/`parseBool` fall back silently.

### `cache.go`

One exported function: `CacheKey(method, path, rawQuery, body)`.

| Step | What it does |
|---|---|
| 1 | `TrimPrefix("?")` then `url.ParseQuery` then **sort query keys** so `?a=1&b=2` and `?b=2&a=1` collide |
| 2 | Build canonical string: `METHOD path[?query][ body=…]` |
| 3 | `sha256.Sum256` + `hex.EncodeToString` → 64-char key |
| 4 | Skip the body when method is GET/HEAD/DELETE/OPTIONS — those methods have no defined body, and clients like Postman sometimes erroneously attach one, which would silently desync prefetched keys |
| 5 | Return `""` on parse error so callers can bypass caching |

### `middleware.go`

The hot path. Six things happen on every request:

| # | What | Where |
|---|---|---|
| 1 | Read or create `session_id` cookie | `sessionIDFromRequest` / `writeSessionCookie` |
| 2 | Append `METHOD /path` to that session's history | `sessions.AddCall` |
| 3 | Read body once (skipped for bodyless methods), restore it for the handler | inline |
| 4 | Compute `CacheKey` and `cache.Get(key)` | inline |
| 5 | On hit: `writeCached()` replays status + content-type + body, sets `X-Cache: HIT`, returns | `writeCached` |
| 6 | On miss: wrap response in a `recorder`, run handler, `cache.SetWithTTL`, set `X-Cache: MISS`, then `go prefetch(...)` | inline |

#### `recorder`

`http.ResponseWriter` wrapper that tees `Write` calls into a `bytes.Buffer` and captures `WriteHeader` so the handler's status and body can both be replayed later.

#### `cachedResponse`

What is actually stored: `status int`, `contentType string`, `body []byte`. Status is required so a cached 404 stays a 404.

#### `PrefetchRegistry`

`map[string]PrefetchFunc` keyed by `"METHOD /path"`. `Lookup(call)` parses `"METHOD /path?query"` and returns `(fn, query)`. The query is folded into the cache key, so different predicted inputs cache separately.

#### `prefetch()` (goroutine)

Runs after every miss:

1. `sessions.Snapshot(sid)` → history slice.
2. `predictor.Predict(history, 3)` → top-3 predictions with softmax probs.
3. For each prediction:
   - skip empty / `"END"`,
   - skip if no registered handler,
   - run the handler to produce bytes,
   - skip on error or empty body,
   - `cache.SetWithTTL(method, path, query, body="")` with the predicted query.
4. Empty body is critical: the predictor never sees bodies, so the prefetched key always has `body=""`.

### `handlers.go`

| Function | Route | Body / Query | Response |
|---|---|---|---|
| `ListProductsHandler` | `GET /products` | `?id=N` optional | single product or list |
| `FilterProductsHandler` | `POST /products/filter` | `{name_substr, min_price?, max_price?, limit?}` | filtered list |
| `FilterProductsPageHandler` | `GET /products/filter` | — | `{"filters":{}}` placeholder |
| `HealthHandler` | `GET /healthz` | — | `{"status":"ok"}` |

`HealthHandler` and `GET /products/filter` are intentionally outside the prefetch registry — `/healthz` should never be cached and the GET-filter is a metadata stub.

### `predictor.go`

Public surface: `Prediction{Call, Prob}`, `Tokenizer`, `Predictor`, `NewPredictor`, `Predict`, `Close`.

`Predictor` is concurrency-safe: `Predict` takes an `RLock` so many inferences run in parallel; `Close` takes the write lock exclusively.

`Tokenizer.Encode(history)`:

1. If history is longer than `MaxLen`, keep the last `MaxLen`.
2. Right-align the IDs into a `MaxLen`-long slice (oldest at the end).
3. Unknown strings → 0 (PAD).

`NewPredictor(onnxPath, tokenizerPath)` calls `newBackend` which is selected at compile time (see Build Variants below).

### `predictor_onnx.go` *(build tag: `cgo && windows`)*

The real ONNX backend using `github.com/yalue/onnxruntime_go`:

1. `init()` — on Windows, sets the DLL search order so a newer `onnxruntime.dll` (API ≥ 29) wins over the older System32 copy. Resolves the DLL via `ONNXRUNTIME_DLL` env var or `./onnxruntime.dll`.
2. `newBackend()` — initializes the ORT environment once (`sync.Once`), creates a `DynamicAdvancedSession` with input `"input_layer"` and output `"output_0"`.
3. `Predict(history, topK)`:
   - Tokenise → flat `[]float32`.
   - Build input tensor `[1, MaxLen]`, output tensor `[1, VocabSize]`.
   - Run session, sort indices by probability descending, return top-K.

### `predictor_stub.go` *(build tag: `!cgo`)*

Deterministic fallback so the rest of the pipeline compiles and runs without CGO or ONNX. **Not real inference** — picks the last history call as the head prediction and fills remaining slots from the vocab in sorted order. Useful for tests and dev machines without the ONNX runtime.

### `session.go`

| Type | What |
|---|---|
| `Session` | One caller's history (slice of strings) |
| `SessionStore` | LRU map of `session_id → Session` |

Both bounds matter: `maxHist` caps per-session history (the LSTM only sees `MaxLen` anyway), `maxSessions` caps total memory.

| Method | Behaviour |
|---|---|
| `GetOrCreate(id)` | Touch (move-to-front) + return |
| `AddCall(id, call)` | Append + trim to `maxHist` |
| `Snapshot(id)` | Copy of history (or `nil` if unknown) — touches LRU as a side effect |
| `Count()` | Diagnostics |

`NewSessionID()` is 16 random bytes hex-encoded (32 chars).

### `store.go`

Thin Supabase/PostgREST client.

| Function | Supabase endpoint |
|---|---|
| `ListProducts(ctx)` | `GET /products?select=*&order=id` |
| `GetProduct(ctx, id)` | `GET /products?id=eq.<id>&select=*` |
| `FilterProducts(ctx, opts)` | `GET /products?<buildFilterQuery(opts)>` |

`buildFilterQuery` translates `FilterOptions` into PostgREST operators — `ilike.*X*` for substring (with `%`, `_`, `&` URL-escaped), `gte.`/`lte.` for price range, `limit=` for truncation.

`WithImageURL(base)` joins the CloudFront base to each product's `image_path`, prepending `https://` if the base has no scheme.

---

## Two Key Lifecycle Loops

### Request lifecycle

```
incoming request
    │
    ▼
read session_id cookie (or create one, set it)
    │
    ▼
append "METHOD /path" to session history
    │
    ▼
compute cache key (method + path + sorted query + body*)
    │            (*body only for non-bodyless methods)
    ▼
cache.Get(key)?
    ├── yes → writeCached(): replay status/content-type/body, set X-Cache: HIT
    └── no  → run handler through recorder, cache.SetWithTTL, set X-Cache: MISS
                                              │
                                              ▼
                                  go prefetch(...)  // background
```

### Prefetch loop (background goroutine)

```
sessions.Snapshot(sid)  ──►  history []string
                              │
                              ▼
                  predictor.Predict(history, 3)
                              │
                              ▼
                  []Prediction{Call, Prob}
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
   skip empty/END     skip if no         run handler →
                       registry entry    compute key,
                                          cache.SetWithTTL
```

---

## Configuration Reference (`.env`)

```env
SUPABASE_URL=https://wodxueleyrgukeewrxve.supabase.co
SUPABASE_KEY=...
IMAGE_BASE_URL=d3qtqd30ynvals.cloudfront.net
ONNX_MODEL_PATH=cache_predict_model.onnx
TOKENIZER_PATH=tokenizer.json
CACHE_TTL=60
CACHE_MAX=500
PORT=:1234
```

For a live demo: bump `CACHE_TTL=600` so prefetched entries survive between calls.

---

## Build Variants

| Build tag | Backend used | When |
|---|---|---|
| `cgo && windows` | `predictor_onnx.go` (real ONNX) | default production build |
| `!cgo` | `predictor_stub.go` (deterministic stub) | tests, dev boxes without C toolchain / onnxruntime |

`go build` on Windows with the right DLL present picks the real backend. Cross-compiling to Linux without CGO picks the stub — the API still works, predictions are nonsense.

---

## Observability

Every request emits structured-ish log lines. Useful prefixes:

| Prefix | What it tells you |
|---|---|
| `[mw] sid=… METHOD /path key=…` | request received and cache key |
| `[mw] sid=… HIT key=…` | served from cache |
| `[mw] sid=… MISS→cached … bytes=N` | handler ran, body stored |
| `[mw] sid=… MISS no-header …` | handler did not write a header — nothing cached (rare) |
| `[pre] sid=… history=[…] predictions=N` | predictor run summary |
| `[pre] sid=… [i] p=0.xxxx call="…"` | each candidate prediction |
| `[pre] sid=… [i] SKIP no-registered-handler …` | model wants an endpoint you have no `PrefetchFunc` for |
| `[pre] sid=… [i] STORED … bytes=N ttl=…` | successful prefetch — the next matching request should `HIT` |

The `sid=…` is the first 8 hex chars of the `session_id` cookie; same sid across requests means same caller and same history.

---

## Known Limitations

- The registry only covers `GET /products` and `GET /products/filter`. The model can predict `/cart`, `/checkout`, `/search`, `/reviews`, `/wishlist` from the vocab but those endpoints aren't wired, so predictions get dropped (`SKIP no-registered-handler`).
- `POST /products/filter` is also unregistered — the predictor doesn't see request bodies, so it can't warm a specific filter combination in advance.
- Probabilities on the first call are low (~0.07–0.08) because the model has no signal yet; it spreads mass across plausible next IDs. Confidence rises after 2+ history entries.
- `/healthz` is intentionally outside the registry — health checks should always hit the live handler.
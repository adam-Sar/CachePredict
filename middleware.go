package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/labstack/echo/v5"
)

// recorder tees response writes into a buffer for capture while forwarding
// headers and WriteHeader to the underlying writer.
type recorder struct {
	http.ResponseWriter
	buf *bytes.Buffer
}

func (r *recorder) Write(b []byte) (int, error) {
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}

// PrefetchFunc produces the bytes that should be cached for an endpoint.
// Implementations must return the exact bytes the matching handler would
// emit so the cache stays consistent across prefetch and real requests.
type PrefetchFunc func(ctx context.Context) ([]byte, error)

// PrefetchRegistry maps "METHOD /path" to a PrefetchFunc. Predicted calls
// include their query string, but dispatch is method+path only; the query
// becomes part of the cache key so different predicted inputs still cache
// separately.
type PrefetchRegistry struct {
	funcs map[string]PrefetchFunc
}

func NewPrefetchRegistry() *PrefetchRegistry {
	return &PrefetchRegistry{funcs: make(map[string]PrefetchFunc)}
}

func (r *PrefetchRegistry) Register(method, path string, fn PrefetchFunc) {
	r.funcs[strings.ToUpper(method)+" "+path] = fn
}

// Lookup resolves a predicted call string ("METHOD /path?query") to its
// registered PrefetchFunc and the query string that should be folded into
// the cache key.
func (r *PrefetchRegistry) Lookup(call string) (PrefetchFunc, string) {
	method, path, query, ok := splitCall(call)
	if !ok {
		return nil, ""
	}
	fn, ok := r.funcs[method+" "+path]
	if !ok {
		return nil, ""
	}
	return fn, query
}

// splitCall parses "METHOD /path?query" into its parts. The method is
// upper-cased so callers can register and look up without worrying about
// case. ok is false when the call is malformed (missing method or path).
func splitCall(call string) (method, path, query string, ok bool) {
	parts := strings.SplitN(call, " ", 2)
	if len(parts) != 2 {
		return "", "", "", false
	}
	method = strings.ToUpper(parts[0])
	rest := parts[1]
	if idx := strings.Index(rest, "?"); idx >= 0 {
		path = rest[:idx]
		query = rest[idx+1:]
	} else {
		path = rest
	}
	if path == "" {
		return "", "", "", false
	}
	return method, path, query, true
}

// PrefetchMiddleware returns an echo middleware that:
//   - assigns a session id (via cookie) and logs the call to session history;
//   - serves cached responses for matching keys (skipping predictor on hit);
//   - on miss, runs the handler with a recording writer and stores the bytes;
//   - asynchronously warms the cache with the predictor's top-K predictions.
//
// Cache values are []byte response bodies; cost is len(body); TTL comes from
// the ttl argument via cache.SetWithTTL.
//
// Inputs:
//
// p:        LSTM predictor; pass nil to disable async prefetch.
// cache:    ristretto cache shared with the rest of the app.
// registry: endpoint → byte-producing function used by prefetch.
// sessions: per-user history used by the predictor.
// ttl:      how long a cached response stays valid.
func PrefetchMiddleware(
	p *Predictor,
	cache *ristretto.Cache,
	registry *PrefetchRegistry,
	sessions *SessionStore,
	ttl time.Duration,
) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// Session: create if absent, log this call.
			sid := sessionIDFromRequest(c)
			if sid == "" {
				sid = NewSessionID()
				writeSessionCookie(c, sid)
			}
			sessions.AddCall(sid, c.Request().Method+" "+c.Request().URL.Path)

			// Read body once (for cache key) and restore it for the handler.
			var body string
			if c.Request().Body != nil {
				b, _ := io.ReadAll(c.Request().Body)
				body = string(b)
				c.Request().Body = io.NopCloser(bytes.NewReader(b))
			}

			key := CacheKey(c.Request().Method, c.Request().URL.Path, c.Request().URL.RawQuery, body)
			if key == "" {
				return next(c) // unparseable query — skip caching, still serve
			}

			if v, found := cache.Get(key); found {
				if b, ok := v.([]byte); ok {
					return writeCached(c, b)
				}
			}

			rec := &recorder{ResponseWriter: c.Response(), buf: &bytes.Buffer{}}
			c.SetResponse(rec)
			if err := next(c); err != nil {
				return err
			}
			if rec.buf.Len() > 0 {
				cache.SetWithTTL(key, rec.buf.Bytes(), int64(rec.buf.Len()), ttl)
			}

			if p != nil && registry != nil {
				go prefetch(p, cache, registry, sessions, sid, ttl)
			}
			return nil
		}
	}
}

// writeCached writes pre-cached bytes back as a 200 application/json response.
// Inputs: c — echo context; b — cached body.
func writeCached(c *echo.Context, b []byte) error {
	c.Response().Header().Set("Content-Type", "application/json")
	c.Response().WriteHeader(http.StatusOK)
	_, err := c.Response().Write(b)
	return err
}

// prefetch runs the predictor for the session and warms the cache for each
// predicted call that has a registered PrefetchFunc. Unregistered or
// unparseable predictions are silently skipped.
func prefetch(p *Predictor, cache *ristretto.Cache, registry *PrefetchRegistry, sessions *SessionStore, sid string, ttl time.Duration) {
	history := sessions.Snapshot(sid)
	preds, err := p.Predict(history, 3)
	if err != nil {
		return
	}
	for _, pred := range preds {
		if pred.Call == "" || pred.Call == "END" {
			continue
		}
		fn, query := registry.Lookup(pred.Call)
		if fn == nil {
			continue
		}
		body, err := fn(context.Background())
		if err != nil || len(body) == 0 {
			continue
		}
		method, path, _, _ := splitCall(pred.Call)
		key := CacheKey(method, path, query, "")
		if key == "" {
			continue
		}
		cache.SetWithTTL(key, body, int64(len(body)), ttl)
	}
}

// sessionIDFromRequest returns the "session_id" cookie value, or "" if missing.
// Inputs: c — echo context. Output: cookie value or "".
func sessionIDFromRequest(c *echo.Context) string {
	cookie, err := c.Cookie("session_id")
	if err != nil {
		return ""
	}
	return cookie.Value
}

// writeSessionCookie sets the "session_id" cookie on the response.
// Inputs: c — echo context; id — session id to write.
func writeSessionCookie(c *echo.Context, id string) {
	c.SetCookie(&http.Cookie{
		Name:     "session_id",
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400,
	})
}

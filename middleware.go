package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/labstack/echo/v5"
)

// recorder tees response writes into a buffer for capture while forwarding
// headers and WriteHeader to the underlying writer. It captures the status
// code and content type so cached entries can be replayed faithfully.
type recorder struct {
	http.ResponseWriter
	buf         *bytes.Buffer
	status      int
	wroteHeader bool
}

func (r *recorder) WriteHeader(s int) {
	if !r.wroteHeader {
		r.status = s
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(s)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		// net/http auto-writes 200 before the first Write; mirror that.
		r.status = http.StatusOK
		r.wroteHeader = true
	}
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}

// cachedResponse is what the ristretto cache stores. Status and content type
// must be replayed alongside the body so e.g. a 404 stays a 404 after the
// underlying handler is bypassed.
type cachedResponse struct {
	status      int
	contentType string
	body        []byte
}

// PrefetchFunc produces the bytes that should be cached for an endpoint.
// Implementations must return the exact bytes the matching handler would
// emit so the cache stays consistent across prefetch and real requests.
// The query argument is the parsed query string from the predicted call
// (without the leading "?"), or "" when the call had no query.
type PrefetchFunc func(ctx context.Context, query string) ([]byte, error)

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
//   - assigns a session id (via cookie) and appends the call to session history;
//   - serves cached responses for matching keys (skipping the predictor on hit);
//   - on miss, runs the handler with a recording writer and stores the bytes;
//   - asynchronously warms the cache with the predictor's top-K predictions.
//
// Pass a nil predictor to disable async prefetch. cookieSecure controls whether
// the session cookie is marked Secure (set true behind HTTPS).
func PrefetchMiddleware(
	p *Predictor,
	cache *ristretto.Cache,
	registry *PrefetchRegistry,
	sessions *SessionStore,
	ttl time.Duration,
	cookieSecure bool,
) echo.MiddlewareFunc {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c *echo.Context) error {
				if c.Request().URL.Path == "/api/activity" {
					return next(c) // bypass caching, logging, prefetch
				}
				// Per-session history: create + cookie on first hit, then append.
			sid := sessionIDFromRequest(c)
			if sid == "" {
				sid = NewSessionID()
				writeSessionCookie(c, sid, cookieSecure)
			}
			sessions.AddCall(sid, c.Request().Method+" "+c.Request().URL.Path)

			// Read body once (for cache key) and restore it for the handler.
			// GET/HEAD/DELETE/OPTIONS carry no defined body, so skip reading
			// to keep cache keys stable across clients that erroneously
			// attach one (Postman, some proxies).
			var body string
			if c.Request().Body != nil && !isBodylessMethod(c.Request().Method) {
				b, _ := io.ReadAll(c.Request().Body)
				body = string(b)
				c.Request().Body = io.NopCloser(bytes.NewReader(b))
			}

			key := CacheKey(c.Request().Method, c.Request().URL.Path, c.Request().URL.RawQuery, body)
			if key == "" {
				return next(c) // unparseable query — skip caching, still serve
			}
			log.Printf("[mw] sid=%s %s %s key=%s", sid[:8], c.Request().Method, c.Request().URL.RequestURI(), key[:12])
			label := humanLabel(c.Request().Method, c.Request().URL.Path, c.Request().URL.RawQuery, body)
			publishEvent(Event{
				Type:    "request",
				SID:     sid,
				Method:  c.Request().Method,
				Path:    c.Request().URL.RequestURI(),
				Query:   c.Request().URL.RawQuery,
				Key:     key,
				Label:   label,
			})

			if v, found := cache.Get(key); found {
				if resp, ok := v.(*cachedResponse); ok {
					log.Printf("[mw] sid=%s HIT key=%s", sid[:8], key[:12])
					publishEvent(Event{
						Type:        "request",
						SID:         sid,
						Method:      c.Request().Method,
						Path:        c.Request().URL.RequestURI(),
						Query:       c.Request().URL.RawQuery,
						Key:         key,
						CacheStatus: "HIT",
						Label:       label,
					})
					return writeCached(c, resp)
				}
			}

			rec := &recorder{ResponseWriter: c.Response(), buf: &bytes.Buffer{}}
			rec.Header().Set("X-Cache", "MISS")
			c.SetResponse(rec)
			if err := next(c); err != nil {
				return err
			}
			if rec.wroteHeader {
				contentType := rec.Header().Get("Content-Type")
				if contentType == "" {
					contentType = "application/json"
				}
				status := rec.status
				if status == 0 {
					status = http.StatusOK
				}
				body := rec.buf.Bytes()
				rememberNameFromBody(body)
				resp := &cachedResponse{
					status:      status,
					contentType: contentType,
					body:        body,
				}
				cache.SetWithTTL(key, resp, int64(len(resp.body)), ttl)
				log.Printf("[mw] sid=%s MISS→cached key=%s status=%d bytes=%d ttl=%s", sid[:8], key[:12], status, len(resp.body), ttl)
				resource := extractResourceFromQuery(c.Request().URL.RawQuery)
				publishEvent(Event{
					Type:        "request",
					SID:         sid,
					Method:      c.Request().Method,
					Path:        c.Request().URL.RequestURI(),
					Query:       c.Request().URL.RawQuery,
					Key:         key,
					CacheStatus: "MISS",
					Bytes:       len(resp.body),
					TTLMs:       ttl.Milliseconds(),
					Label:       label,
					Resource:    resource,
				})
			} else {
				log.Printf("[mw] sid=%s MISS no-header key=%s", sid[:8], key[:12])
				publishEvent(Event{
					Type:        "request",
					SID:         sid,
					Method:      c.Request().Method,
					Path:        c.Request().URL.RequestURI(),
					Query:       c.Request().URL.RawQuery,
					Key:         key,
					CacheStatus: "MISS",
					Label:       label,
				})
			}

			if p != nil && registry != nil {
				go prefetch(p, cache, registry, sessions, sid, ttl)
			}
			return nil
		}
	}
}

// writeCached replays a cached response (status, content type, body) on the
// echo context. Used after a cache hit to bypass the handler.
func writeCached(c *echo.Context, resp *cachedResponse) error {
	c.Response().Header().Set("Content-Type", resp.contentType)
	c.Response().Header().Set("X-Cache", "HIT")
	c.Response().WriteHeader(resp.status)
	_, err := c.Response().Write(resp.body)
	return err
}

// prefetch runs the predictor on the session history and warms the cache for
// each prediction that has a registered PrefetchFunc. Anything unregistered or
// unparseable is silently dropped. Runs as a goroutine; errors only log.
func prefetch(p *Predictor, cache *ristretto.Cache, registry *PrefetchRegistry, sessions *SessionStore, sid string, ttl time.Duration) {
	history := sessions.Snapshot(sid)
	preds, err := p.Predict(history, 3)
	if err != nil {
		log.Printf("[pre] sid=%s predictor err=%v history=%v", sid[:8], err, history)
		return
	}
	log.Printf("[pre] sid=%s history=%v predictions=%d", sid[:8], history, len(preds))

	out := make([]EventPrediction, 0, len(preds))
	for i, pred := range preds {
		log.Printf("[pre] sid=%s  [%d] p=%.4f call=%q", sid[:8], i, pred.Prob, pred.Call)
		method, path, query, _ := splitCall(pred.Call)
		ep := EventPrediction{
			Call:  pred.Call,
			Prob:  pred.Prob,
			Label: humanLabel(method, path, query, ""),
		}
		if pred.Call == "" || pred.Call == "END" {
			ep.Reason = "end"
			out = append(out, ep)
			continue
		}
		fn, query := registry.Lookup(pred.Call)
		if fn == nil {
			log.Printf("[pre] sid=%s  [%d] SKIP no-registered-handler for %q", sid[:8], i, pred.Call)
			ep.Reason = "no-handler"
			out = append(out, ep)
			continue
		}
		body, err := fn(context.Background(), query)
		if err != nil || len(body) == 0 {
			log.Printf("[pre] sid=%s  [%d] SKIP fn-err=%v bytes=%d for %q", sid[:8], i, err, len(body), pred.Call)
			ep.Reason = "fn-error"
			out = append(out, ep)
			continue
		}
		rememberNameFromBody(body)
		key := CacheKey(method, path, query, "")
		if key == "" {
			log.Printf("[pre] sid=%s  [%d] SKIP empty-key for %q", sid[:8], i, pred.Call)
			ep.Reason = "empty-key"
			out = append(out, ep)
			continue
		}
		resp := &cachedResponse{
			status:      http.StatusOK,
			contentType: "application/json",
			body:        body,
		}
		cache.SetWithTTL(key, resp, int64(len(body)), ttl)
		log.Printf("[pre] sid=%s  [%d] STORED key=%s call=%q bytes=%d ttl=%s", sid[:8], i, key[:12], pred.Call, len(body), ttl)
		ep.Stored = true
		ep.Bytes = len(body)
		ep.Resource = extractResourceFromQuery(query)
		if ep.Resource == "" && path == "/products" && query == "" {
			ep.Resource = ""
		}
		out = append(out, ep)
	}
	publishEvent(Event{
		Type:        "prefetch",
		SID:         sid,
		History:     history,
		Predictions: out,
	})
}

// sessionIDFromRequest returns the "session_id" cookie value, or "" if absent.
func sessionIDFromRequest(c *echo.Context) string {
	cookie, err := c.Cookie("session_id")
	if err != nil {
		return ""
	}
	return cookie.Value
}

// writeSessionCookie sets the "session_id" cookie. When secure is true the
// cookie is only sent over HTTPS.
func writeSessionCookie(c *echo.Context, id string, secure bool) {
	c.SetCookie(&http.Cookie{
		Name:     "session_id",
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
}

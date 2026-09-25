package main

import (
	"bytes"
	"io"
	"net/http"
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
// sessions: per-user history used by the predictor.
// ttl:      how long a cached response stays valid.
func PrefetchMiddleware(
	p *Predictor,
	cache *ristretto.Cache,
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

			if p != nil {
				go prefetch(p, cache, sessions, sid, ttl)
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

// prefetch predicts the next likely calls for the given session. Cache warming
// is a no-op until a handler registry is added to route pred.Call strings to
// the matching core handler.
func prefetch(p *Predictor, cache *ristretto.Cache, sessions *SessionStore, sid string, ttl time.Duration) {
	history := sessions.Snapshot(sid)
	preds, err := p.Predict(history, 3)
	if err != nil {
		return
	}
	for _, pred := range preds {
		if pred.Call == "" || pred.Call == "END" {
			continue
		}
		_ = cache
		_ = ttl
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

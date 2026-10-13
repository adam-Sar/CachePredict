package precog

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// responseRecorder captures the status code, headers, and body of the downstream handler.
type responseRecorder struct {
	http.ResponseWriter
	buf         *bytes.Buffer
	status      int
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.status = http.StatusOK
		r.wroteHeader = true
	}
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}

// Middleware returns a standard net/http middleware handler.
func (e *Engine) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Automatically serve Precog browser SDK assets (/precog/precog.js & /precog/precog.wasm)
		if ServeClientAssets(w, r) {
			return
		}

		// Ignore prefetch loopback requests to prevent recursion
		if r.Header.Get("X-Precog-Speculative") != "" {
			next.ServeHTTP(w, r)
			return
		}

		// Check if route is explicitly skipped
		for _, skip := range e.config.SkipRoutes {
			if strings.HasPrefix(r.URL.Path, skip) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Extract or generate session ID
		sid := e.extractSession(r)
		if sid == "" {
			sid = generateSessionID()
			e.writeSessionCookie(w, sid)
		}

		method := strings.ToUpper(r.Method)
		isSafe := method == "GET" || method == "HEAD"

		// Read body once for non-GET requests (if needed for cache key)
		var bodyStr string
		if !isSafe && r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			bodyStr = string(b)
			r.Body = io.NopCloser(bytes.NewReader(b))
		}

		cacheKey := CanonicalCacheKey(method, r.URL.Path, r.URL.RawQuery, bodyStr)

		// 1. Check Cache for Safe Requests
		if isSafe && cacheKey != "" {
			if entry, found := e.store.Get(cacheKey); found {
				e.telemetry.RecordHit()

				// Record navigation in predictor so next predictions continue
				e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
				e.triggerPrefetch(sid, r, w)

				// Serve with pre-cooked zero-copy streaming
				w.Header().Set("X-Cache", "HIT")
				w.Header().Set("X-Precog", "ram-cache")
				_, _ = entry.WriteResponse(w)
				return
			}
		}

		// 2. Cache Miss: Coalesce in-flight requests to stop duplicate DB calls
		e.telemetry.RecordMiss()

		if isSafe && cacheKey != "" && e.inflight != nil {
			entry, err := e.inflight.Do(cacheKey, func() (*ResponseEntry, error) {
				// Double-check cache in case prefetcher committed while we waited
				if cached, found := e.store.Get(cacheKey); found {
					return cached, nil
				}

				rec := &responseRecorder{
					ResponseWriter: httptest.NewRecorder(),
					buf:            &bytes.Buffer{},
					status:         http.StatusOK,
				}
				next.ServeHTTP(rec, r)

				if rec.status == http.StatusOK {
					respBody := rec.buf.Bytes()
					contentType := rec.Header().Get("Content-Type")
					ent := &ResponseEntry{
						Body:        respBody,
						ContentType: contentType,
						StatusCode:  rec.status,
						CreatedAt:   time.Now(),
						WireBytes:   PreCookWireResponse(rec.status, contentType, respBody),
					}
					e.store.Set(cacheKey, ent, e.config.TTL)
					return ent, nil
				}
				return nil, nil
			})

			if err == nil && entry != nil {
				if entry.ContentType != "" {
					w.Header().Set("Content-Type", entry.ContentType)
				}
				w.Header().Set("X-Cache", "MISS")
				w.WriteHeader(entry.StatusCode)
				_, _ = w.Write(entry.Body)

				e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
				e.triggerPrefetch(sid, r, w)
				return
			}
		}

		// Fallback for non-GET or un-coalesced requests
		w.Header().Set("X-Cache", "MISS")
		next.ServeHTTP(w, r)
		e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
		if isSafe {
			e.triggerPrefetch(sid, r, w)
		}
	})
}

func (e *Engine) triggerPrefetch(sid string, r *http.Request, w http.ResponseWriter) {
	preds := e.predictor.Predict(sid, 3)
	if len(preds) == 0 {
		return
	}

	// Attach W3C Speculation Rules prefetch link headers to response
	if w != nil {
		if linkHeader := BuildSpeculationRulesHeader(preds); linkHeader != "" {
			w.Header().Add("Link", linkHeader)
		}
	}

	// Trigger server-side background speculative fetch
	if e.prefetcher != nil {
		e.prefetcher.Submit(sid, preds, r)
	}
}

func (e *Engine) extractSession(r *http.Request) string {
	if e.config.SessionExtractor != nil {
		return e.config.SessionExtractor(r)
	}
	// Default: Look in Cookie then X-Session-ID header
	if cookie, err := r.Cookie(e.config.SessionCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return r.Header.Get("X-Session-ID")
}

func (e *Engine) writeSessionCookie(w http.ResponseWriter, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     e.config.SessionCookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		Secure:   e.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
}

func generateSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

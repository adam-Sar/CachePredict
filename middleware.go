package precog

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
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
				e.triggerPrefetch(sid, r)

				// Serve from cache
				w.Header().Set("X-Cache", "HIT")
				w.Header().Set("X-Precog", "ram-cache")
				if entry.ContentType != "" {
					w.Header().Set("Content-Type", entry.ContentType)
				}
				w.WriteHeader(entry.StatusCode)
				_, _ = w.Write(entry.Body)
				return
			}
		}

		// 2. Cache Miss: Execute handler and record response
		e.telemetry.RecordMiss()
		rec := &responseRecorder{
			ResponseWriter: w,
			buf:            &bytes.Buffer{},
			status:         http.StatusOK,
		}
		w.Header().Set("X-Cache", "MISS")

		next.ServeHTTP(rec, r)

		// Store successful GET/HEAD responses in cache
		if isSafe && rec.status == http.StatusOK && cacheKey != "" {
			respBody := rec.buf.Bytes()
			entry := &ResponseEntry{
				Body:        respBody,
				ContentType: rec.Header().Get("Content-Type"),
				StatusCode:  rec.status,
				CreatedAt:   time.Now(),
			}
			e.store.Set(cacheKey, entry, e.config.TTL)
		}

		// Update predictor and trigger background prefetch
		e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
		if isSafe {
			e.triggerPrefetch(sid, r)
		}
	})
}

func (e *Engine) triggerPrefetch(sid string, r *http.Request) {
	if e.prefetcher == nil {
		return
	}
	preds := e.predictor.Predict(sid, 3)
	if len(preds) > 0 {
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

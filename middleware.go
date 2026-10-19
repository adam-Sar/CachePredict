package precog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// Middleware returns a standard net/http middleware handler.
func (e *Engine) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Automatically serve Precog browser SDK assets (/precog/precog.js & /precog/precog.wasm)
		if ServeClientAssets(w, r) {
			return
		}

		// Mark if this is a speculative loopback/prefetch request
		isSpeculative := r.Header.Get("X-Precog-Speculative") != ""

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
		isAuth := e.IsRequestAuthenticated(r)

		// Cache keys only apply to safe methods. Non-safe methods are neither
		// cached nor coalesced, so their bodies are never read or hashed.
		var cacheKey string
		if isSafe {
			cacheKey = CanonicalCacheKey(method, r.URL.Path, r.URL.RawQuery, "")
		}

		// 1. Check Cache for Safe Requests (skip shared cache lookup for unconfirmed auth requests)
		if isSafe && cacheKey != "" {
			if entry, found := e.store.Get(cacheKey); found {
				isPublic := strings.Contains(entry.Header.Get("Cache-Control"), "public")
				if !isAuth || isPublic {
					e.telemetry.RecordHit()

					if !isSpeculative {
						// Record navigation in predictor so next predictions continue
						e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
						e.triggerPrefetch(sid, r, w)
					}

					// Serve with headers preserved
					_, _ = entry.WriteResponse(w)
					return
				}
			}
		}

		// 2. Cache Miss: Coalesce in-flight requests to stop duplicate DB calls
		e.telemetry.RecordMiss()

		if isSafe && cacheKey != "" && e.inflight != nil {
			entry, err := e.inflight.Do(cacheKey, func() (*ResponseEntry, error) {
				// Double-check cache in case prefetcher committed while we waited
				if cached, found := e.store.Get(cacheKey); found {
					isPublic := strings.Contains(cached.Header.Get("Cache-Control"), "public")
					if !isAuth || isPublic {
						return cached, nil
					}
				}

				rec := httptest.NewRecorder()
				detachedReq := r.WithContext(context.WithoutCancel(r.Context()))
				next.ServeHTTP(rec, detachedReq)

				respBody := rec.Body.Bytes()
				contentType := rec.Header().Get("Content-Type")
				cc := rec.Header().Get("Cache-Control")

				hdr := make(http.Header)
				for k, vv := range rec.Header() {
					hdr[k] = append([]string(nil), vv...)
				}

				ent := &ResponseEntry{
					Body:        respBody,
					Header:      hdr,
					ContentType: contentType,
					StatusCode:  rec.Code,
					CreatedAt:   time.Now(),
					ExpiresAt:   time.Now().Add(e.config.TTL),
				}

				// Only cache safe responses according to RFC 9111.
				// Responses with Set-Cookie must never be shared-cached (RFC 9111 §3.5).
				canCache := rec.Code == http.StatusOK &&
					len(rec.Header().Values("Set-Cookie")) == 0 &&
					!strings.Contains(cc, "no-store") &&
					!strings.Contains(cc, "private") &&
					(!isAuth || strings.Contains(cc, "public"))

				if canCache {
					e.store.Set(cacheKey, ent, e.config.TTL)
				}
				return ent, nil
			})

		if err == nil && entry != nil {
			// Record navigation and attach prefetch hints BEFORE the response
			// is committed; headers added after WriteHeader are dropped by net/http.
			if !isSpeculative {
				e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
				e.triggerPrefetch(sid, r, w)
			}

			for k, vv := range entry.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			if entry.ContentType != "" && w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", entry.ContentType)
			}
			w.Header().Set("X-Cache", "MISS")
			w.WriteHeader(entry.StatusCode)
			_, _ = w.Write(entry.Body)
			return
		}
		}

		// Fallback for non-GET or un-coalesced requests
		w.Header().Set("X-Cache", "MISS")
		next.ServeHTTP(w, r)
		if !isSpeculative {
			e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
			if isSafe {
				e.triggerPrefetch(sid, r, w)
			}
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

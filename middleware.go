package precog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
)

// Middleware returns a standard net/http middleware handler with production-grade
// predictive caching, security isolation, and RFC 9111 HTTP semantics compliance.
func (e *Engine) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Automatically serve Precog browser SDK assets (/precog/precog.js & /precog/precog.wasm)
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

		// Non-safe methods (POST, PUT, DELETE, PATCH, etc.) pass straight through
		if !isSafe {
			w.Header().Set("X-Cache", "MISS")
			next.ServeHTTP(w, r)
			if !isSpeculative {
				e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
			}
			return
		}

		// 2. Protocol & Streaming Bypass Checks:
		// WebSockets, Server-Sent Events (SSE), and Range requests bypass caching and coalescing
		isUpgrade := r.Header.Get("Upgrade") != ""
		isSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream")
		isRange := r.Header.Get("Range") != ""

		if isUpgrade || isSSE || isRange {
			w.Header().Set("X-Cache", "MISS")
			next.ServeHTTP(w, r)
			if !isSpeculative {
				e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
			}
			return
		}

		// 3. Client Request Directives (RFC 9111 §5.2.1)
		reqCC := r.Header.Get("Cache-Control")
		isClientNoCache := strings.Contains(reqCC, "no-cache") ||
			strings.Contains(reqCC, "max-age=0") ||
			r.Header.Get("Pragma") == "no-cache"
		isClientNoStore := strings.Contains(reqCC, "no-store")

		// 4. Authentication and Route Classification
		isExplicitPublic := e.isRoutePublic(r.URL.Path)
		isAuth := e.IsRequestAuthenticated(r)

		// 5. Host-aware Cache Key
		host := r.Host
		cacheKey := CanonicalCacheKeyWithHost(method, host, r.URL.Path, r.URL.RawQuery)

		// 6. Cache Lookup (skip on client no-cache or client no-store)
		if !isClientNoCache {
			if entry, found := e.store.Get(cacheKey); found && entry != nil {
				varyHeader := entry.Header.Get("Vary")
				lookupEntry := entry
				if varyHeader != "" && varyHeader != "*" {
					vKey := varyKey(cacheKey, varyHeader, r)
					if vKey != cacheKey {
						if ve, ok := e.store.Get(vKey); ok && ve != nil {
							lookupEntry = ve
						}
					}
				}

				if lookupEntry.MatchesVary(r) {
					isPublicResp := strings.Contains(lookupEntry.Header.Get("Cache-Control"), "public")
					if !isAuth || isExplicitPublic || isPublicResp {
						e.telemetry.RecordHit()

						if !isSpeculative {
							e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
							e.triggerPrefetch(sid, r, w)
						}

						_, _ = lookupEntry.WriteResponse(w)
						return
					}
				}
			}
		}

		// 7. Cache Miss: Record telemetry
		e.telemetry.RecordMiss()

		// 8. Coalescing Security Boundary
		// Only safe, unauthenticated (or explicitly public) requests are eligible for shared coalescing.
		// Authenticated requests NEVER coalesce across different users/sessions by default.
		canCoalesce := !isClientNoCache && !isClientNoStore && (isExplicitPublic || (!isAuth && len(r.Cookies()) == 0))
		var coalesceKey string

		if canCoalesce {
			coalesceKey = cacheKey
		} else if isAuth && e.config.CoalesceAuthenticated {
			// Per-user isolated coalescing only if explicitly enabled
			coalesceKey = cacheKey + ":user:" + sid
		}

		// 9. Coalesced Path
		if coalesceKey != "" && e.inflight != nil {
			var isLeader bool
			var leaderEntry *ResponseEntry
			var leaderRecorder *capturingResponseWriter

			entry, err := e.inflight.DoContext(r.Context(), coalesceKey, func() (*ResponseEntry, error) {
				isLeader = true

				// Double-check cache in case a prefetcher committed while we waited
				if cached, found := e.store.Get(cacheKey); found && cached != nil {
					varyHeader := cached.Header.Get("Vary")
					lookupCached := cached
					if varyHeader != "" && varyHeader != "*" {
						vKey := varyKey(cacheKey, varyHeader, r)
						if vKey != cacheKey {
							if ve, ok := e.store.Get(vKey); ok && ve != nil {
								lookupCached = ve
							}
						}
					}
					if lookupCached.MatchesVary(r) {
						isPublicResp := strings.Contains(lookupCached.Header.Get("Cache-Control"), "public")
						if !isAuth || isExplicitPublic || isPublicResp {
							return lookupCached, nil
						}
					}
				}

				rec := newCapturingResponseWriter(w, e.config.MaxResponseSize)
				leaderRecorder = rec

				detachedReq := r.WithContext(context.WithoutCancel(r.Context()))
				next.ServeHTTP(rec, detachedReq)

				// If response streamed or overflowed, the leader already flushed to its client
				if rec.streaming || rec.overflow {
					return nil, ErrStreamingResponse
				}

				respBody := rec.BodyBytes()
				contentType := rec.Header().Get("Content-Type")
				cc := rec.Header().Get("Cache-Control")
				varyHeader := rec.Header().Get("Vary")

				hdr := make(http.Header)
				for k, vv := range rec.Header() {
					hdr[k] = append([]string(nil), vv...)
				}

				varyMap := extractVaryMap(r, varyHeader)

				ent := &ResponseEntry{
					Body:        respBody,
					Header:      hdr,
					ContentType: contentType,
					StatusCode:  rec.StatusCode(),
					CreatedAt:   time.Now(),
					ExpiresAt:   time.Now().Add(e.config.TTL),
					Vary:        varyMap,
				}
				leaderEntry = ent

				// Evaluate RFC 9111 cacheability
				canCache := rec.StatusCode() == http.StatusOK &&
					len(rec.Header().Values("Set-Cookie")) == 0 &&
					!strings.Contains(cc, "no-store") &&
					!strings.Contains(cc, "private") &&
					varyHeader != "*" &&
					(!isAuth || isExplicitPublic || strings.Contains(cc, "public"))

				if canCache && !isClientNoStore {
					e.store.Set(cacheKey, ent, e.config.TTL)
					if varyHeader != "" && varyHeader != "*" {
						vKey := varyKey(cacheKey, varyHeader, r)
						e.store.Set(vKey, ent, e.config.TTL)
					}
					return ent, nil // Safe to share with waiting followers
				}

				// Response is private, set-cookie, or uncacheable: DO NOT share with followers!
				return nil, ErrNotShareable
			})

			// If this goroutine was the leader and it already streamed to w, it is finished
			if isLeader && leaderRecorder != nil && (leaderRecorder.streaming || leaderRecorder.overflow) {
				if !isSpeculative {
					e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
				}
				return
			}

			// Case A: Leader or Follower received a shared, cacheable entry
			if err == nil && entry != nil {
				if !isSpeculative {
					e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
					e.triggerPrefetch(sid, r, w)
				}

				for k, vv := range entry.Header {
					if strings.HasPrefix(strings.ToLower(k), "x-precog-vary") {
						continue
					}
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

			// Case B: Leader produced a private / unshareable response
			// Leader serves its own response to its own client
			if isLeader && leaderEntry != nil && leaderRecorder != nil && !leaderRecorder.headerWritten {
				if !isSpeculative {
					e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
					if isSafe {
						e.triggerPrefetch(sid, r, w)
					}
				}
				for k, vv := range leaderEntry.Header {
					if strings.HasPrefix(strings.ToLower(k), "x-precog-vary") {
						continue
					}
					for _, v := range vv {
						w.Header().Add(k, v)
					}
				}
				w.Header().Set("X-Cache", "MISS")
				w.WriteHeader(leaderEntry.StatusCode)
				_, _ = w.Write(leaderEntry.Body)
				return
			}

			// Case C: Waiting followers who were unblocked with ErrNotShareable or ErrStreamingResponse:
			// Fall through to execute upstream fresh for their own connection.
		}

		// 10. Direct Passthrough / Uncoalesced Execution
		rec := newCapturingResponseWriter(w, e.config.MaxResponseSize)
		next.ServeHTTP(rec, r)

		// If streaming or already committed, finish
		if rec.streaming || rec.overflow {
			if !isSpeculative {
				e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
			}
			return
		}

		respBody := rec.BodyBytes()
		contentType := rec.Header().Get("Content-Type")
		cc := rec.Header().Get("Cache-Control")
		varyHeader := rec.Header().Get("Vary")

		hdr := make(http.Header)
		for k, vv := range rec.Header() {
			hdr[k] = append([]string(nil), vv...)
		}

		canCache := rec.StatusCode() == http.StatusOK &&
			len(rec.Header().Values("Set-Cookie")) == 0 &&
			!strings.Contains(cc, "no-store") &&
			!strings.Contains(cc, "private") &&
			varyHeader != "*" &&
			(!isAuth || isExplicitPublic || strings.Contains(cc, "public"))

		if canCache && !isClientNoStore {
			varyMap := extractVaryMap(r, varyHeader)
			ent := &ResponseEntry{
				Body:        respBody,
				Header:      hdr,
				ContentType: contentType,
				StatusCode:  rec.StatusCode(),
				CreatedAt:   time.Now(),
				ExpiresAt:   time.Now().Add(e.config.TTL),
				Vary:        varyMap,
			}
			e.store.Set(cacheKey, ent, e.config.TTL)
			if varyHeader != "" && varyHeader != "*" {
				vKey := varyKey(cacheKey, varyHeader, r)
				e.store.Set(vKey, ent, e.config.TTL)
			}
		}

		if !isSpeculative {
			e.predictor.Record(sid, method, r.URL.Path, r.URL.RawQuery)
			if isSafe {
				e.triggerPrefetch(sid, r, w)
			}
		}

		for k, vv := range hdr {
			if strings.HasPrefix(strings.ToLower(k), "x-precog-vary") {
				continue
			}
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		if contentType != "" && w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("X-Cache", "MISS")
		w.WriteHeader(rec.StatusCode())
		_, _ = w.Write(respBody)
	})
}

func varySignature(varyHeader string, r *http.Request) string {
	parts := strings.Split(varyHeader, ",")
	var b strings.Builder
	for i, p := range parts {
		h := strings.TrimSpace(p)
		if h != "" {
			if i > 0 {
				b.WriteString("&")
			}
			b.WriteString(strings.ToLower(h))
			b.WriteString("=")
			b.WriteString(r.Header.Get(h))
		}
	}
	return b.String()
}

func varyKey(primaryKey string, varyHeader string, r *http.Request) string {
	if varyHeader == "" || varyHeader == "*" {
		return primaryKey
	}
	sig := varySignature(varyHeader, r)
	h := xxhash.Sum64String(sig)
	return fmt.Sprintf("%s_v%x", primaryKey, h)
}

func extractVaryMap(r *http.Request, varyHeader string) map[string]string {
	if varyHeader == "" || varyHeader == "*" {
		return nil
	}
	parts := strings.Split(varyHeader, ",")
	m := make(map[string]string, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name != "" {
			m[name] = r.Header.Get(name)
		}
	}
	return m
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

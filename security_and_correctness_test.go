package precog_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adam-Sar/precog"
)

// TestCrossUserCoalescingSecurity guarantees that two different authenticated users
// concurrently requesting the exact same path NEVER receive each other's private data.
func TestCrossUserCoalescingSecurity(t *testing.T) {
	engine, err := precog.New(precog.Config{
		TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		// Simulate database lookup latency so concurrent requests overlap
		time.Sleep(30 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		// Explicitly private per-user data
		w.Header().Set("Cache-Control", "private, no-cache")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"user_token":"%s"}`, token)))
	})

	wrapped := engine.Handler(handler)

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		userID := fmt.Sprintf("user_%d", i)
		go func(id string) {
			defer wg.Done()

			req := httptest.NewRequest("GET", "/api/profile", nil)
			req.Header.Set("Authorization", "Bearer "+id)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			body := rec.Body.String()
			expectedToken := "Bearer " + id
			if !strings.Contains(body, expectedToken) {
				t.Errorf("SECURITY LEAK: user %s received another user's response: %s", id, body)
			}
		}(userID)
	}

	wg.Wait()
}

// TestSetCookieNeverSharedAcrossWaiters ensures that even if unauthenticated requests
// are coalesced, an endpoint issuing a Set-Cookie header NEVER leaks that cookie to other waiters.
func TestSetCookieNeverSharedAcrossWaiters(t *testing.T) {
	engine, err := precog.New(precog.Config{
		TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	var callCount atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		time.Sleep(40 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{
			Name:  "session_token",
			Value: fmt.Sprintf("token_%d", n),
		})
		_, _ = w.Write([]byte(fmt.Sprintf(`{"login_call":%d}`, n)))
	})

	wrapped := engine.Handler(handler)

	const concurrency = 5
	var wg sync.WaitGroup
	wg.Add(concurrency)

	receivedCookies := make([]string, concurrency)

	for i := 0; i < concurrency; i++ {
		idx := i
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/auth/token", nil)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)
			cookie := rec.Header().Get("Set-Cookie")
			receivedCookies[idx] = cookie
		}()
	}

	wg.Wait()

	// Every waiter that received a cookie must have received a UNIQUE token (no replayed Set-Cookie)
	cookieCounts := make(map[string]int)
	for _, c := range receivedCookies {
		if c != "" {
			cookieCounts[c]++
			if cookieCounts[c] > 1 {
				t.Fatalf("SECURITY LEAK: duplicate Set-Cookie %q distributed to multiple callers", c)
			}
		}
	}
}

// TestHostIsolationInCacheKey ensures multi-tenant virtual hosts are completely isolated
// and cannot poison or collide in each other's cache.
func TestHostIsolationInCacheKey(t *testing.T) {
	engine, err := precog.New(precog.Config{
		TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"host":"%s"}`, r.Host)))
	})

	wrapped := engine.Handler(handler)

	// 1. Prime cache for tenant1.example.com
	req1 := httptest.NewRequest("GET", "/api/info", nil)
	req1.Host = "tenant1.example.com"
	rec1 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec1, req1)

	if !strings.Contains(rec1.Body.String(), "tenant1.example.com") {
		t.Fatalf("unexpected body for tenant1: %s", rec1.Body.String())
	}

	// 2. Query tenant2.example.com with same path: must NOT return tenant1's cached response
	req2 := httptest.NewRequest("GET", "/api/info", nil)
	req2.Host = "tenant2.example.com"
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)

	if !strings.Contains(rec2.Body.String(), "tenant2.example.com") {
		t.Fatalf("HOST POISONING: tenant2 received tenant1 cached data: %s", rec2.Body.String())
	}
}

// TestVaryHeaderRFC9111Compliance verifies RFC 9111 Vary header behavior:
// content negotiation differences must result in cache misses and separate storage.
func TestVaryHeaderRFC9111Compliance(t *testing.T) {
	engine, err := precog.New(precog.Config{
		TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	var handlerCalls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalls.Add(1)
		enc := r.Header.Get("Accept-Encoding")
		w.Header().Set("Vary", "Accept-Encoding")
		if strings.Contains(enc, "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write([]byte("GZIP_DATA"))
		} else {
			_, _ = w.Write([]byte("PLAIN_DATA"))
		}
	})

	wrapped := engine.Handler(handler)

	// 1. Request with Accept-Encoding: gzip (Cache MISS -> stores gzip)
	reqGzip := httptest.NewRequest("GET", "/content", nil)
	reqGzip.Header.Set("Accept-Encoding", "gzip")
	recGzip := httptest.NewRecorder()
	wrapped.ServeHTTP(recGzip, reqGzip)
	if recGzip.Body.String() != "GZIP_DATA" {
		t.Fatalf("expected GZIP_DATA, got %s", recGzip.Body.String())
	}

	// 2. Request with Accept-Encoding: identity (Must NOT receive GZIP_DATA!)
	reqPlain := httptest.NewRequest("GET", "/content", nil)
	reqPlain.Header.Set("Accept-Encoding", "identity")
	recPlain := httptest.NewRecorder()
	wrapped.ServeHTTP(recPlain, reqPlain)

	if recPlain.Body.String() != "PLAIN_DATA" {
		t.Fatalf("VARY VIOLATION: plain client received gzip data: %s", recPlain.Body.String())
	}

	// 3. Re-request with Accept-Encoding: gzip (Must be a HIT from cache)
	recGzip2 := httptest.NewRecorder()
	wrapped.ServeHTTP(recGzip2, reqGzip)
	if recGzip2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected HIT on matching Vary request, got %s", recGzip2.Header().Get("X-Cache"))
	}
}

// TestVaryStarNeverCached asserts that responses declaring Vary: * are never cached (RFC 9111 §4.1).
func TestVaryStarNeverCached(t *testing.T) {
	engine, err := precog.New(precog.Config{
		TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Vary", "*")
		_, _ = w.Write([]byte("DYNAMIC"))
	})

	wrapped := engine.Handler(handler)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/vary-star", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Header().Get("X-Cache") == "HIT" {
			t.Fatalf("Vary: * response was cached on call %d", i)
		}
	}

	if calls.Load() != 3 {
		t.Fatalf("expected 3 handler calls for Vary: *, got %d", calls.Load())
	}
}

// TestClientCacheControlDirectives verifies that client requests with no-cache,
// max-age=0, or no-store bypass the cache.
func TestClientCacheControlDirectives(t *testing.T) {
	engine, err := precog.New(precog.Config{
		TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(fmt.Sprintf("response_%d", n)))
	})

	wrapped := engine.Handler(handler)

	// 1. Prime cache
	req1 := httptest.NewRequest("GET", "/refreshable", nil)
	rec1 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec1, req1)
	if rec1.Body.String() != "response_1" {
		t.Fatalf("expected response_1, got %s", rec1.Body.String())
	}

	// 2. Normal repeat request: Cache HIT
	req2 := httptest.NewRequest("GET", "/refreshable", nil)
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected HIT, got %s", rec2.Header().Get("X-Cache"))
	}

	// 3. Client sends Cache-Control: no-cache: Must bypass cache and run fresh handler
	reqNoCache := httptest.NewRequest("GET", "/refreshable", nil)
	reqNoCache.Header.Set("Cache-Control", "no-cache")
	recNoCache := httptest.NewRecorder()
	wrapped.ServeHTTP(recNoCache, reqNoCache)

	if recNoCache.Header().Get("X-Cache") == "HIT" {
		t.Fatalf("expected MISS on client no-cache, got HIT")
	}
	if recNoCache.Body.String() != "response_2" {
		t.Fatalf("expected fresh response_2, got %s", recNoCache.Body.String())
	}
}

// flushRecorder is a custom httptest recorder that implements http.Flusher.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flushRecorder) Flush() {
	f.flushed = true
}

// TestServerSentEventsStreamingCompatibility verifies that Server-Sent Events (SSE)
// stream continuously via http.Flusher without buffering or hanging.
func TestServerSentEventsStreamingCompatibility(t *testing.T) {
	engine, err := precog.New(precog.Config{
		TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("expected ResponseWriter to implement http.Flusher")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		for i := 1; i <= 3; i++ {
			_, _ = fmt.Fprintf(w, "data: event_%d\n\n", i)
			flusher.Flush()
		}
	})

	wrapped := engine.Handler(handler)

	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	wrapped.ServeHTTP(rec, req)

	if !rec.flushed {
		t.Errorf("expected flusher.Flush() to have been called")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event_1") || !strings.Contains(body, "event_3") {
		t.Fatalf("SSE stream output missing events: %s", body)
	}
}

// hijackerRecorder tests http.Hijacker protocol upgrade support.
type hijackerRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackerRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	c1, _ := net.Pipe()
	rw := bufio.NewReadWriter(bufio.NewReader(c1), bufio.NewWriter(c1))
	return c1, rw, nil
}

func TestHijackerCompatibility(t *testing.T) {
	engine, err := precog.New(precog.Config{
		TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "cannot hijack", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = conn.Close()
	})

	wrapped := engine.Handler(handler)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	rec := &hijackerRecorder{ResponseRecorder: httptest.NewRecorder()}

	wrapped.ServeHTTP(rec, req)

	if !rec.hijacked {
		t.Fatalf("expected Hijack() to succeed on WebSocket upgrade request")
	}
}

// TestInflightWaiterContextCancellation verifies that if a client cancels while waiting
// for an in-flight request, the waiting goroutine immediately unblocks and returns ctx.Err().
func TestInflightWaiterContextCancellation(t *testing.T) {
	g := precog.NewInflightGroup()

	leaderBlock := make(chan struct{})
	leaderStarted := make(chan struct{})

	// Start leader
	go func() {
		_, _ = g.Do("test_cancel_key", func() (*precog.ResponseEntry, error) {
			close(leaderStarted)
			<-leaderBlock
			return &precog.ResponseEntry{StatusCode: 200}, nil
		})
	}()

	<-leaderStarted

	// Follower with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := g.DoContext(ctx, "test_cancel_key", func() (*precog.ResponseEntry, error) {
		return nil, nil
	})
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("follower blocked too long: %v", elapsed)
	}

	close(leaderBlock)
}

// TestDensePredictorSaturationRescaling verifies that counter saturation guards in
// DenseMatrixPredictor smoothly rescale counters without overflow or panic.
func TestDensePredictorSaturationRescaling(t *testing.T) {
	p := precog.NewDenseMatrixPredictor(&precog.DensePredictorConfig{
		MaxRoutes: 16,
	})

	// Repeated transitions to verify stability under high volume
	for i := 0; i < 5000; i++ {
		p.Record("sess_volume", "GET", "/start", "")
		p.Record("sess_volume", "GET", "/finish", "")
	}

	preds := p.Predict("sess_volume", 3)
	if len(preds) == 0 {
		t.Fatalf("expected predictions, got none")
	}
	if preds[0].Probability <= 0 || preds[0].Probability > 1.0 {
		t.Fatalf("invalid probability score: %f", preds[0].Probability)
	}
}

// TestMaxResponseSizeBypass ensures responses larger than MaxResponseSize stream
// directly and are not cached in memory.
func TestMaxResponseSizeBypass(t *testing.T) {
	engine, err := precog.New(precog.Config{
		TTL:             5 * time.Minute,
		MaxResponseSize: 1024, // 1 KB limit
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	largePayload := make([]byte, 4096) // 4 KB > 1 KB limit
	for i := range largePayload {
		largePayload[i] = 'A'
	}

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(largePayload)
	})

	wrapped := engine.Handler(handler)

	// Call 1
	req1 := httptest.NewRequest("GET", "/large-file", nil)
	rec1 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec1, req1)

	if len(rec1.Body.Bytes()) != 4096 {
		t.Fatalf("expected 4096 bytes, got %d", len(rec1.Body.Bytes()))
	}

	// Call 2: Must NOT be cached
	req2 := httptest.NewRequest("GET", "/large-file", nil)
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Cache") == "HIT" {
		t.Fatalf("response larger than MaxResponseSize was cached")
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 handler calls, got %d", calls.Load())
	}
}

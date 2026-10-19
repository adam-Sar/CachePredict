package precog_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adam-Sar/precog"
)

func BenchmarkFastHash64_BarePath(b *testing.B) {
	method := "GET"
	path := "/api/v1/products/detail"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = precog.FastHash64(method, path, "", "")
	}
}

func BenchmarkFastHash64_WithQuery(b *testing.B) {
	method := "GET"
	path := "/api/v1/products/detail"
	query := "category=shoes&sort=price_desc&limit=20&page=2"
	body := ""

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = precog.FastHash64(method, path, query, body)
	}
}

func BenchmarkSHA256Baseline(b *testing.B) {
	data := "GET /api/v1/products/detail?category=shoes&limit=20&page=2&sort=price_desc"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := sha256.Sum256([]byte(data))
		_ = fmt.Sprintf("%x", h)
	}
}

func BenchmarkEndpointRegistryLookup(b *testing.B) {
	reg := precog.NewEndpointRegistry()
	pattern := "GET /api/v1/products/:id"
	reg.GetOrRegister(pattern)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reg.Lookup(pattern)
	}
}

func BenchmarkTieredStore_L1Hit(b *testing.B) {
	store := precog.NewTieredStore(precog.NewShardedStore(nil))
	defer store.Close()

	key := "benchmark_hot_key"
	entry := &precog.ResponseEntry{
		StatusCode:  200,
		ContentType: "application/json",
		Body:        []byte(`{"status":"ok"}`),
	}
	store.Set(key, entry, time.Hour)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			val, ok := store.Get(key)
			if !ok || val == nil {
				b.Fatal("unexpected miss")
			}
		}
	})
}

func BenchmarkShardedStore_Parallel(b *testing.B) {
	store := precog.NewShardedStore(&precog.ShardedConfig{
		NumShards: 256,
	})
	defer store.Close()

	for i := 0; i < 256; i++ {
		k := fmt.Sprintf("key_%d", i)
		store.Set(k, &precog.ResponseEntry{
			StatusCode:  200,
			ContentType: "application/json",
			Body:        []byte(`{"data":"test"}`),
		}, time.Hour)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			k := fmt.Sprintf("key_%d", idx&255)
			_, _ = store.Get(k)
			idx++
		}
	})
}

func BenchmarkDenseMatrixPredictor_Record(b *testing.B) {
	p := precog.NewDenseMatrixPredictor(&precog.DensePredictorConfig{
		MaxRoutes: 512,
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			session := fmt.Sprintf("sess_%d", idx&63)
			p.Record(session, "GET", "/items", "q=shoes")
			idx++
		}
	})
}

func BenchmarkDenseMatrixPredictor_Predict(b *testing.B) {
	p := precog.NewDenseMatrixPredictor(&precog.DensePredictorConfig{
		MaxRoutes: 512,
	})
	p.Record("sess_1", "GET", "/items", "")
	p.Record("sess_1", "GET", "/checkout", "")
	p.Record("sess_1", "GET", "/items", "")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Predict("sess_1", 3)
	}
}

func BenchmarkHTTPMiddleware_CacheHit(b *testing.B) {
	engine, err := precog.New(precog.Config{
		TTL: time.Hour,
	})
	if err != nil {
		b.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	wrapped := engine.Handler(handler)

	// Prime cache
	primeReq := httptest.NewRequest("GET", "/api/v1/bench", nil)
	primeRec := httptest.NewRecorder()
	wrapped.ServeHTTP(primeRec, primeReq)

	req := httptest.NewRequest("GET", "/api/v1/bench", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Header().Get("X-Cache") != "HIT" {
			b.Fatal("expected cache HIT")
		}
	}
}

func BenchmarkHTTPMiddleware_ParallelHits(b *testing.B) {
	engine, err := precog.New(precog.Config{
		TTL: time.Hour,
	})
	if err != nil {
		b.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	wrapped := engine.Handler(handler)

	// Prime cache
	primeReq := httptest.NewRequest("GET", "/api/v1/bench", nil)
	primeRec := httptest.NewRecorder()
	wrapped.ServeHTTP(primeRec, primeReq)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest("GET", "/api/v1/bench", nil)
		for pb.Next() {
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
		}
	})
}

func BenchmarkHTTPMiddleware_Coalescing(b *testing.B) {
	engine, err := precog.New(precog.Config{
		TTL: 10 * time.Millisecond,
	})
	if err != nil {
		b.Fatalf("failed to init engine: %v", err)
	}
	defer engine.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Millisecond) // Simulated fast DB query
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"coalesced":true}`))
	})
	wrapped := engine.Handler(handler)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/api/v1/coalesce", nil)
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
		}
	})
}

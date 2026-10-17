package precog_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adam-Sar/precog"
)

// The Speculation Rules Link hint must be present on the FIRST request (MISS),
// not only on subsequent HITs.
func TestLinkHeaderPresentOnMiss(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/catalog", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`catalog`))
	})
	mux.HandleFunc("/cart", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`cart`))
	})

	predictor := precog.NewAdaptiveMarkovPredictor()
	predictor.SeedTransition("GET /catalog", "GET /cart", 10)

	engine, err := precog.New(precog.Config{
		Predictor:           predictor,
		ConfidenceThreshold: 0.5,
		TTL:                 10 * time.Second,
		Workers:             2,
	})
	if err != nil {
		t.Fatalf("failed to init precog: %v", err)
	}
	defer engine.Close()

	server := httptest.NewServer(engine.Handler(mux))
	defer server.Close()

	resp, err := http.Get(server.URL + "/catalog")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if resp.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("expected MISS on first request, got %s", resp.Header.Get("X-Cache"))
	}
	link := resp.Header.Get("Link")
	if !strings.Contains(link, "/cart") || !strings.Contains(link, "rel=prefetch") {
		t.Errorf("expected prefetch Link hint on MISS response, got %q", link)
	}
}

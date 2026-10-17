package precog_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adam-Sar/precog"
)

func TestClientAssetsAndTag(t *testing.T) {
	tag := precog.ClientScriptTag()
	if !strings.Contains(tag, "/precog/precog.js") {
		t.Fatalf("expected script tag to reference /precog/precog.js, got %s", tag)
	}

	// Test serving precog.js directly
	req := httptest.NewRequest("GET", "/precog/precog.js", nil)
	rec := httptest.NewRecorder()

	handled := precog.ServeClientAssets(rec, req)
	if !handled {
		t.Fatalf("expected ServeClientAssets to handle /precog/precog.js")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Errorf("expected javascript content-type, got %s", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "Precog") {
		t.Errorf("expected body to contain Precog, got %s", rec.Body.String()[:100])
	}

	// Test serving the Go wasm runtime glue
	reqGlue := httptest.NewRequest("GET", "/precog/wasm_exec.js", nil)
	recGlue := httptest.NewRecorder()
	if !precog.ServeClientAssets(recGlue, reqGlue) {
		t.Fatalf("expected ServeClientAssets to handle /precog/wasm_exec.js")
	}
	if recGlue.Code != http.StatusOK || recGlue.Body.Len() == 0 {
		t.Errorf("expected wasm_exec.js to be served, got status %d len %d", recGlue.Code, recGlue.Body.Len())
	}
	if !strings.Contains(recGlue.Body.String(), "Go") {
		t.Errorf("expected wasm_exec.js glue content")
	}
}

func TestBuildSpeculationRulesHeader(t *testing.T) {
	predictions := []precog.Prediction{
		{Call: "GET /api/checkout", Probability: 0.9},
		{Call: "GET /products/42", Probability: 0.8},
	}

	header := precog.BuildSpeculationRulesHeader(predictions)
	expected := "</api/checkout>; rel=prefetch, </products/42>; rel=prefetch"
	if header != expected {
		t.Fatalf("expected '%s', got '%s'", expected, header)
	}
}

func TestMiddlewareClientAssetAndSpeculationIntegration(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"test"}`))
	})

	engine, err := precog.New(precog.Config{})
	if err != nil {
		t.Fatalf("failed to init precog: %v", err)
	}
	defer engine.Close()

	server := httptest.NewServer(engine.Handler(mux))
	defer server.Close()

	client := server.Client()

	// 1. Fetch precog.js via the engine middleware
	resp, err := client.Get(server.URL + "/precog/precog.js")
	if err != nil {
		t.Fatalf("failed to fetch precog.js: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for precog.js, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Precog") {
		t.Errorf("precog.js body missing Precog symbol")
	}

	// 2. Fetch API route
	respAPI, err := client.Get(server.URL + "/api/data")
	if err != nil {
		t.Fatalf("failed to fetch /api/data: %v", err)
	}
	respAPI.Body.Close()
}

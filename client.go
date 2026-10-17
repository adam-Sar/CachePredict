package precog

import (
	_ "embed"
	"net/http"
	"os"
	"strings"
	"sync"
)

//go:embed precog.js
var ClientScriptBytes []byte

//go:embed wasm_exec.js
var wasmExecScript []byte

// wasmCache reads precog.wasm once from the working directory and memoizes it.
var (
	wasmOnce   sync.Once
	wasmBytes  []byte
	wasmLoaded bool
)

// ClientScriptTag returns the HTML script tag for embedding Precog in any web page or template.
func ClientScriptTag() string {
	return `<script src="/precog/precog.js" defer></script>`
}

// ServeClientAssets handles HTTP requests for /precog/precog.js, /precog/precog.wasm,
// and /precog/wasm_exec.js. Returns true if the request was handled, false otherwise.
func ServeClientAssets(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/precog/precog.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(ClientScriptBytes)
		return true

	case "/precog/wasm_exec.js":
		// Go wasm runtime glue required to instantiate precog.wasm in the browser
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(wasmExecScript)
		return true

	case "/precog/precog.wasm":
		// Read precog.wasm from the working directory once, then serve from memory
		wasmOnce.Do(func() {
			wasmBytes, _ = os.ReadFile("precog.wasm")
			wasmLoaded = wasmBytes != nil
		})
		if wasmLoaded {
			w.Header().Set("Content-Type", "application/wasm")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			_, _ = w.Write(wasmBytes)
			return true
		}
		http.NotFound(w, r)
		return true
	}

	return false
}

// BuildSpeculationRulesHeader formats W3C Speculation Rules prefetch Link headers
// for modern browser C++ pre-warming.
func BuildSpeculationRulesHeader(predictions []Prediction) string {
	if len(predictions) == 0 {
		return ""
	}

	var parts []string
	for _, p := range predictions {
		callParts := strings.SplitN(p.Call, " ", 2)
		url := p.Call
		if len(callParts) == 2 {
			url = callParts[1]
		}
		if strings.HasPrefix(url, "/") {
			parts = append(parts, "<"+url+">; rel=prefetch")
		}
	}

	return strings.Join(parts, ", ")
}

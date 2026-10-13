package precog

import (
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"strings"
)

//go:embed precog.js
var ClientScriptBytes []byte

// ClientScriptTag returns the HTML script tag for embedding Precog in any web page or template.
func ClientScriptTag() string {
	return `<script src="/precog/precog.js" defer></script>`
}

// ServeClientAssets handles HTTP requests for /precog/precog.js and /precog/precog.wasm.
// Returns true if the request was handled, false otherwise.
func ServeClientAssets(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/precog/precog.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(ClientScriptBytes)
		return true

	case "/precog/precog.wasm":
		// Check if precog.wasm exists in working directory or binary dir
		if data, err := os.ReadFile("precog.wasm"); err == nil {
			w.Header().Set("Content-Type", "application/wasm")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			_, _ = w.Write(data)
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
			parts = append(parts, fmt.Sprintf("<%s>; rel=prefetch", url))
		}
	}

	return strings.Join(parts, ", ")
}

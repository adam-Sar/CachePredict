package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSplitCall(t *testing.T) {
	tests := []struct {
		call   string
		wantOK bool
		wantM  string
		wantP  string
		wantQ  string
	}{
		{"GET /products", true, "GET", "/products", ""},
		{"GET /products?id=44", true, "GET", "/products", "id=44"},
		{"POST /products/filter", true, "POST", "/products/filter", ""},
		{"POST /products/filter?a=1&b=2", true, "POST", "/products/filter", "a=1&b=2"},
		{"GET /products?", true, "GET", "/products", ""},
		{"GET", false, "", "", ""},
		{"", false, "", "", ""},
		{"GET ", false, "", "", ""},
		{"GET ?x=1", false, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			m, p, q, ok := splitCall(tt.call)
			if ok != tt.wantOK || m != tt.wantM || p != tt.wantP || q != tt.wantQ {
				t.Errorf("splitCall(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
					tt.call, m, p, q, ok, tt.wantM, tt.wantP, tt.wantQ, tt.wantOK)
			}
		})
	}
}

func TestPrefetchRegistryLookup(t *testing.T) {
	called := false
	reg := NewPrefetchRegistry()
	reg.Register("GET", "/products", func(ctx context.Context, query string) ([]byte, error) {
		called = true
		return []byte(`{"ok":true,"q":"` + query + `"}`), nil
	})

	t.Run("exact match", func(t *testing.T) {
		fn, query := reg.Lookup("GET /products")
		if fn == nil {
			t.Fatal("Lookup returned nil fn for registered path")
		}
		if query != "" {
			t.Errorf("query = %q, want empty", query)
		}
		body, err := fn(context.Background(), query)
		if err != nil {
			t.Fatalf("fn error: %v", err)
		}
		if !called || string(body) != `{"ok":true,"q":""}` {
			t.Errorf("fn not invoked correctly")
		}
	})

	t.Run("with query", func(t *testing.T) {
		_, query := reg.Lookup("GET /products?id=44")
		if query != "id=44" {
			t.Errorf("query = %q, want id=44", query)
		}
	})

	t.Run("unknown path", func(t *testing.T) {
		fn, _ := reg.Lookup("GET /unknown")
		if fn != nil {
			t.Errorf("Lookup returned fn for unregistered path")
		}
	})

	t.Run("unknown method", func(t *testing.T) {
		fn, _ := reg.Lookup("DELETE /products")
		if fn != nil {
			t.Errorf("Lookup returned fn for unregistered method")
		}
	})

	t.Run("malformed call", func(t *testing.T) {
		if fn, _ := reg.Lookup(""); fn != nil {
			t.Errorf("Lookup returned fn for empty call")
		}
		if fn, _ := reg.Lookup("GET"); fn != nil {
			t.Errorf("Lookup returned fn for method-only call")
		}
	})

	t.Run("method case insensitive", func(t *testing.T) {
		fn, _ := reg.Lookup("get /products")
		if fn == nil {
			t.Errorf("Lookup should be case-insensitive on method")
		}
	})

	t.Run("error propagates from fn", func(t *testing.T) {
		reg.Register("GET", "/err", func(ctx context.Context, query string) ([]byte, error) {
			return nil, errors.New("boom")
		})
		fn, _ := reg.Lookup("GET /err")
		if fn == nil {
			t.Fatal("Lookup returned nil for /err")
		}
		if _, err := fn(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("expected boom error, got %v", err)
		}
	})
}

func TestRecorderCapturesStatusAndBody(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		explicitHeader bool
		contentType    string
		body           string
		wantStatus     int
		wantCT         string
		wantBody       string
	}{
		{
			name:        "explicit 404",
			status:      http.StatusNotFound,
			body:        `{"error":"missing"}`,
			contentType: "application/json",
			wantStatus:  http.StatusNotFound,
			wantCT:      "application/json",
			wantBody:    `{"error":"missing"}`,
		},
		{
			name:       "implicit 200",
			body:       `{"ok":true}`,
			wantStatus: http.StatusOK,
			wantCT:     "application/json",
			wantBody:   `{"ok":true}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := &recorder{ResponseWriter: rec, buf: &bytes.Buffer{}}
			if tt.status != 0 {
				r.WriteHeader(tt.status)
			}
			if tt.contentType != "" {
				r.Header().Set("Content-Type", tt.contentType)
			}
			r.Write([]byte(tt.body))

			if r.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", r.status, tt.wantStatus)
			}
			if r.buf.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", r.buf.String(), tt.wantBody)
			}
		})
	}
}

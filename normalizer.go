package precog

import (
	"regexp"
	"sort"
	"strings"

	"github.com/cespare/xxhash/v2"
)

var (
	// Matches numeric IDs like /products/42 -> /products/:id
	numericRe = regexp.MustCompile(`/\d+(/|$)`)
	// Matches UUIDs like /orders/123e4567-e89b-12d3-a456-426614174000
	uuidRe = regexp.MustCompile(`/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}(/|$)`)
	// Matches hex IDs (e.g. Mongo ObjectIDs)
	hexIdRe = regexp.MustCompile(`/[0-9a-fA-F]{24}(/|$)`)
)

const hexDigits = "0123456789abcdef"

// NormalizeRoute converts concrete request paths into parameterized route patterns.
// Example: "GET /products/44" -> "GET /products/:id"
func NormalizeRoute(method, path string) string {
	m := strings.ToUpper(method)
	p := path

	// Normalize UUIDs
	p = uuidRe.ReplaceAllString(p, "/:uuid$1")
	// Normalize Mongo / 24-char hex IDs
	p = hexIdRe.ReplaceAllString(p, "/:hex$1")
	// Normalize numeric IDs
	p = numericRe.ReplaceAllString(p, "/:id$1")

	return m + " " + p
}

// FastHash64 computes a 64-bit non-cryptographic hash using xxHash with
// zero allocations. Query parameters are canonicalized by sorting raw
// "&"-delimited segments, so parameter order never affects the hash.
func FastHash64(method, path, rawQuery, body string) uint64 {
	method = strings.ToUpper(method)

	d := xxhash.New()
	d.WriteString(method)
	d.WriteString(" ")
	d.WriteString(path)

	if rawQuery != "" {
		d.WriteString("?")
		if !strings.ContainsRune(rawQuery, '&') {
			d.WriteString(rawQuery)
		} else {
			parts := strings.Split(rawQuery, "&")
			sort.Strings(parts)
			for i, p := range parts {
				if i > 0 {
					d.WriteString("&")
				}
				d.WriteString(p)
			}
		}
	}

	// For GET / HEAD, body is ignored
	if body != "" && method != "GET" && method != "HEAD" {
		d.WriteString(" body=")
		d.WriteString(body)
	}

	return d.Sum64()
}

// CanonicalCacheKey computes a deterministic 16-character hexadecimal hash key.
func CanonicalCacheKey(method, path, rawQuery, body string) string {
	h := FastHash64(method, path, rawQuery, body)

	var out [16]byte
	for i := 15; i >= 0; i-- {
		out[i] = hexDigits[h&0x0f]
		h >>= 4
	}
	return string(out[:])
}

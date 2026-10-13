package precog

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var (
	// Matches numeric IDs like /products/42 -> /products/:id
	numericRe = regexp.MustCompile(`/\d+(/|$)`)
	// Matches UUIDs like /orders/123e4567-e89b-12d3-a456-426614174000
	uuidRe = regexp.MustCompile(`/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}(/|$)`)
	// Matches hex IDs (e.g. Mongo ObjectIDs)
	hexIdRe = regexp.MustCompile(`/[0-9a-fA-F]{24}(/|$)`)
)

// NormalizeRoute converts a concrete path into an abstract pattern token for AI training.
// e.g. "GET /products/44" -> "GET /products/:id"
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

// CanonicalCacheKey computes a deterministic, collision-resistant hash key.
// Query parameters are sorted alphabetically so order does not cause cache misses.
func CanonicalCacheKey(method, path, rawQuery, body string) string {
	method = strings.ToUpper(method)
	sortedQuery := SortQuery(rawQuery)

	var sb strings.Builder
	sb.WriteString(method)
	sb.WriteString(" ")
	sb.WriteString(path)
	if sortedQuery != "" {
		sb.WriteString("?")
		sb.WriteString(sortedQuery)
	}

	// For GET / HEAD, body is ignored
	if body != "" && method != "GET" && method != "HEAD" {
		sb.WriteString(" body=")
		sb.WriteString(body)
	}

	hash := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(hash[:])
}

// SortQuery takes a raw query string and returns it with keys sorted lexicographically.
func SortQuery(rawQuery string) string {
	rawQuery = strings.TrimPrefix(rawQuery, "?")
	if rawQuery == "" {
		return ""
	}

	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}

	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		v := vals[k]
		sort.Strings(v)
		for _, val := range v {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(val))
		}
	}
	return strings.Join(parts, "&")
}

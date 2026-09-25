package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

// CacheKey builds a stable cache key from a request method, path, raw query,
// and body. The returned string is what callers pass to ristretto.Set / Get.
//
//   - Sort query params so ?a=1&b=2 and ?b=2&a=1 collide. Uses url.ParseQuery
//     and joins key=value pairs in sorted-key order. Accepts the raw query
//     with or without the leading "?" so callers can pass either
//     c.Request().URL.RawQuery or the full RequestURI.
//   - Includes the request body so different POST filter combinations
//     cache separately.
//   - Hashes the canonical string with SHA-256 and hex-encodes it so the
//     key is a fixed 64-char string and sensitive body fragments don't
//     leak into logs.
//   - Returns "" on query parse errors so callers can detect and skip
//     caching that request.
func CacheKey(method, path, rawQuery, body string) string {
	if rawQuery != "" {
		rawQuery = strings.TrimPrefix(rawQuery, "?")
		if rawQuery != "" {
			v, err := url.ParseQuery(rawQuery)
			if err != nil {
				return ""
			}
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				for _, val := range v[k] {
					parts = append(parts, k+"="+val)
				}
			}
			rawQuery = strings.Join(parts, "&")
		}
	}

	canonical := method + " " + path
	if rawQuery != "" {
		canonical += "?" + rawQuery
	}
	if body != "" {
		canonical += " body=" + body
	}

	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

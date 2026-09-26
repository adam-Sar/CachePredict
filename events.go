package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/labstack/echo/v5"
)

// Event is a single activity message emitted by the middleware. Label and
// Resource carry human-readable strings for the frontend — the raw method/path
// are kept for debugging but never rendered to end users.
type Event struct {
	Seq         int64             `json:"seq"`
	Type        string            `json:"type"`
	SID         string            `json:"sid"`
	Method      string            `json:"method,omitempty"`
	Path        string            `json:"path,omitempty"`
	Query       string            `json:"query,omitempty"`
	Key         string            `json:"key,omitempty"`
	CacheStatus string            `json:"cache_status,omitempty"`
	Bytes       int               `json:"bytes,omitempty"`
	TTLMs       int64             `json:"ttl_ms,omitempty"`
	Label       string            `json:"label,omitempty"`
	Resource    string            `json:"resource,omitempty"`
	History     []string          `json:"history,omitempty"`
	Predictions []EventPrediction `json:"predictions,omitempty"`
}

type EventPrediction struct {
	Call    string  `json:"call"`
	Prob    float32 `json:"prob"`
	Stored  bool    `json:"stored"`
	Reason  string  `json:"reason,omitempty"`
	Bytes   int     `json:"bytes,omitempty"`
	Label   string  `json:"label,omitempty"`
	Resource string  `json:"resource,omitempty"`
}

const eventLogCap = 60
const nameCacheMax = 200

var (
	eventMu   sync.RWMutex
	eventSubs = map[chan Event]struct{}{}
	eventLog  []Event
	nameMu    sync.RWMutex
	nameCache = map[int64]string{}
	nameOrder []int64
	eventSeq  int64
)

func SubscribeEvents() chan Event {
	ch := make(chan Event, 64)
	eventMu.Lock()
	eventSubs[ch] = struct{}{}
	eventMu.Unlock()
	return ch
}

func UnsubscribeEvents(ch chan Event) {
	eventMu.Lock()
	delete(eventSubs, ch)
	eventMu.Unlock()
	close(ch)
}

func publishEvent(e Event) {
	eventMu.Lock()
	eventSeq++
	e.Seq = eventSeq
	eventLog = append(eventLog, e)
	if len(eventLog) > eventLogCap {
		eventLog = eventLog[len(eventLog)-eventLogCap:]
	}
	eventMu.Unlock()

	eventMu.RLock()
	defer eventMu.RUnlock()
	for ch := range eventSubs {
		select {
		case ch <- e:
		default:
		}
	}
}

// isInternalEvent reports whether an event is infrastructure noise that
// shouldn't clutter the user-facing activity feed (poll endpoints, health
// checks). They still hit the Go log via existing log.Printf calls.
func isInternalEvent(e Event) bool {
	if e.Type != "request" {
		return false
	}
	if strings.HasPrefix(e.Path, "/api/") {
		return true
	}
	return e.Path == "/healthz"
}

// rememberName caches a product name keyed by id with FIFO eviction. A name is
// remembered forever once learned; the only way it leaves the cache is by
// being bumped out by newer entries.
func rememberName(id int64, name string) {
	if name == "" {
		return
	}
	nameMu.Lock()
	defer nameMu.Unlock()
	if _, ok := nameCache[id]; ok {
		return
	}
	nameCache[id] = name
	nameOrder = append(nameOrder, id)
	if len(nameOrder) > nameCacheMax {
		evicted := nameOrder[0]
		nameOrder = nameOrder[1:]
		delete(nameCache, evicted)
	}
}

func lookupName(id int64) string {
	nameMu.RLock()
	defer nameMu.RUnlock()
	return nameCache[id]
}

// rememberNameFromBody parses a product or product-list JSON body and remembers
// any product names it contains. Used to enrich both real-request events
// (response body captured by middleware) and prefetch responses.
func rememberNameFromBody(body []byte, ids ...int64) {
	if len(body) == 0 {
		return
	}
	var single struct {
		Product struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"product"`
	}
	if err := json.Unmarshal(body, &single); err == nil && single.Product.Name != "" {
		rememberName(single.Product.ID, single.Product.Name)
	}
	var list struct {
		Products []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"products"`
	}
	if err := json.Unmarshal(body, &list); err == nil {
		for _, p := range list.Products {
			rememberName(p.ID, p.Name)
		}
	}
	_ = ids
}

// extractResourceFromQuery returns a product name for ?id=N queries when one
// has been learned from a previous response; otherwise the empty string. Used
// by the middleware so live events show real names as soon as they've been
// observed once anywhere in this process.
func extractResourceFromQuery(query string) string {
	if query == "" {
		return ""
	}
	vals, _ := url.ParseQuery(query)
	idStr := vals.Get("id")
	if idStr == "" {
		return ""
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return ""
	}
	return lookupName(id)
}

// humanLabel returns a short verb phrase describing what the call does, with
// the product name appended when known. Example outputs:
//
//	"View product — Leather Bag"
//	"Filter products (search: leather, max $100)"
//	"Browse products"
func humanLabel(method, path, query, body string) string {
	switch {
	case path == "/products":
		if query != "" {
			vals, _ := url.ParseQuery(query)
			if idStr := vals.Get("id"); idStr != "" {
				if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
					if name := lookupName(id); name != "" {
						return "View product — " + name
					}
					return fmt.Sprintf("View product #%s", idStr)
				}
				return "View product #" + idStr
			}
		}
		return "Browse products"
	case path == "/home":
		return "Browse home"
	case path == "/search":
		vals, _ := url.ParseQuery(query)
		if q := vals.Get("q"); q != "" {
			return "Search for " + q
		}
		return "Search products"
	case path == "/category":
		vals, _ := url.ParseQuery(query)
		if t := vals.Get("type"); t != "" {
			return "Browse " + t
		}
		return "Browse category"
	case path == "/reviews":
		vals, _ := url.ParseQuery(query)
		if pid := vals.Get("product_id"); pid != "" {
			if id, err := strconv.ParseInt(pid, 10, 64); err == nil {
				if name := lookupName(id); name != "" {
					return "Reviews — " + name
				}
			}
			return "Reviews — product #" + pid
		}
		return "View reviews"
	case path == "/cart":
		if method == "POST" {
			var opts struct {
				ProductID int64 `json:"product_id"`
			}
			_ = json.Unmarshal([]byte(body), &opts)
			if opts.ProductID != 0 {
				if name := lookupName(opts.ProductID); name != "" {
					return "Add to cart — " + name
				}
			}
			return "Add to cart"
		}
		return "View cart"
	case path == "/checkout":
		return "View checkout"
	case path == "/payment":
		var opts struct {
			Amount float64 `json:"amount"`
		}
		_ = json.Unmarshal([]byte(body), &opts)
		return fmt.Sprintf("Pay $%.2f", opts.Amount)
	case path == "/wishlist":
		if method == "POST" {
			var opts struct {
				ProductID int64 `json:"product_id"`
			}
			_ = json.Unmarshal([]byte(body), &opts)
			if opts.ProductID != 0 {
				if name := lookupName(opts.ProductID); name != "" {
					return "Wishlist — " + name
				}
			}
			return "Wishlist add"
		}
		return "View wishlist"
	case path == "/products/filter" && method == "POST":
		var opts struct {
			NameSubstr string   `json:"name_substr"`
			MinPrice   *float64 `json:"min_price"`
			MaxPrice   *float64 `json:"max_price"`
			Limit      int      `json:"limit"`
		}
		_ = json.Unmarshal([]byte(body), &opts)
		var parts []string
		if opts.NameSubstr != "" {
			parts = append(parts, "search: "+opts.NameSubstr)
		}
		if opts.MinPrice != nil {
			parts = append(parts, fmt.Sprintf("min $%.0f", *opts.MinPrice))
		}
		if opts.MaxPrice != nil {
			parts = append(parts, fmt.Sprintf("max $%.0f", *opts.MaxPrice))
		}
		if len(parts) == 0 {
			return "Filter products"
		}
		return "Filter products (" + strings.Join(parts, ", ") + ")"
	case path == "/products/filter":
		return "Filter page"
	case path == "/healthz":
		return "Health check"
	case strings.HasPrefix(path, "/api/"):
		return "Internal poll"
	}
	return strings.ToUpper(method) + " " + path
}

// RecentHandler returns the last N events for the caller's session as JSON.
// Internal-only events (api/*, healthz) are filtered out so the user-facing
// feed only shows actions that matter.
func RecentHandler(c *echo.Context) error {
	sid := sessionIDFromRequest(c)
	if sid == "" {
		sid = NewSessionID()
		writeSessionCookie(c, sid, false)
	}

	eventMu.RLock()
	out := make([]Event, 0, len(eventLog))
	for _, e := range eventLog {
		if e.SID != sid {
			continue
		}
		if isInternalEvent(e) {
			continue
		}
		out = append(out, e)
	}
	eventMu.RUnlock()

	if len(out) > 40 {
		out = out[len(out)-40:]
	}
	return c.JSON(200, map[string]any{"events": out})
}
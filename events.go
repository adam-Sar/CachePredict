package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/labstack/echo/v5"
)

// Event is a single SSE message published by the middleware. Type drives
// downstream rendering; SID scopes every event to one caller's session so
// multiple browser tabs don't see each other's traffic.
type Event struct {
	Type        string            `json:"type"`
	SID         string            `json:"sid"`
	Method      string            `json:"method,omitempty"`
	Path        string            `json:"path,omitempty"`
	Key         string            `json:"key,omitempty"`
	CacheStatus string            `json:"cache_status,omitempty"`
	Bytes       int               `json:"bytes,omitempty"`
	TTLMs       int64             `json:"ttl_ms,omitempty"`
	History     []string          `json:"history,omitempty"`
	Predictions []EventPrediction `json:"predictions,omitempty"`
}

type EventPrediction struct {
	Call   string  `json:"call"`
	Prob   float32 `json:"prob"`
	Stored bool    `json:"stored"`
	Reason string  `json:"reason,omitempty"`
	Bytes  int     `json:"bytes,omitempty"`
}

var (
	eventMu   sync.RWMutex
	eventSubs = map[chan Event]struct{}{}
	eventLog  []Event // ring-buffered; capped at 200, oldest dropped
)

const eventLogCap = 200

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

// flushWriter sends any buffered bytes to the client. http.NewResponseController
// (Go 1.20+) handles the Unwrap chain so we don't care how many wrappers echo
// or the middleware put between us and the socket.
func flushWriter(w http.ResponseWriter) error {
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// RecentHandler returns the last N events for the caller's session as JSON.
// The frontend polls this every second; it replaces SSE for the demo because
// HTTP/1.1 polling works through every proxy without SSE-specific quirks.
// Returns at most 100 events, newest last.
func RecentHandler(c *echo.Context) error {
	sid := sessionIDFromRequest(c)
	if sid == "" {
		sid = NewSessionID()
		writeSessionCookie(c, sid, false)
	}

	eventMu.RLock()
	out := make([]Event, 0, len(eventLog))
	for _, e := range eventLog {
		if e.SID == sid {
			out = append(out, e)
		}
	}
	eventMu.RUnlock()

	if len(out) > 100 {
		out = out[len(out)-100:]
	}
	return c.JSON(200, map[string]any{"events": out})
}

// EventsHandler streams middleware events to the browser as Server-Sent
// Events. A session_id cookie is created on first hit and used to filter the
// stream so each caller only sees their own activity.
func EventsHandler(c *echo.Context) error {
	h := c.Response().Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")

	sid := sessionIDFromRequest(c)
	if sid == "" {
		sid = NewSessionID()
		writeSessionCookie(c, sid, false)
	}

	ch := SubscribeEvents()
	defer UnsubscribeEvents(ch)

	ctx := c.Request().Context()
	w := c.Response()

	fmt.Fprintf(w, ": connected sid=%s\n\n", sid[:8])
	if err := flushWriter(w); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-ch:
			if e.SID != sid {
				continue
			}
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
			if err := flushWriter(w); err != nil {
				return err
			}
		}
	}
}
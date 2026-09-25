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
	eventMu.RLock()
	defer eventMu.RUnlock()
	for ch := range eventSubs {
		select {
		case ch <- e:
		default:
		}
	}
}

// EventsHandler streams middleware events to the browser as Server-Sent
// Events. A session_id cookie is created on first hit and used to filter the
// stream so each caller only sees their own activity.
func EventsHandler(c *echo.Context) error {
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")

	flusher, ok := c.Response().(http.Flusher)
	if !ok {
		return errors.New("streaming not supported")
	}

	sid := sessionIDFromRequest(c)
	if sid == "" {
		sid = NewSessionID()
		writeSessionCookie(c, sid, false)
	}

	ch := SubscribeEvents()
	defer UnsubscribeEvents(ch)

	ctx := c.Request().Context()

	fmt.Fprintf(c.Response(), ": connected sid=%s\n\n", sid[:8])
	flusher.Flush()

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
			fmt.Fprintf(c.Response(), "event: %s\ndata: %s\n\n", e.Type, data)
			flusher.Flush()
		}
	}
}
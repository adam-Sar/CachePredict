package precog

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// capturingResponseWriter is a high-performance ResponseWriter wrapper that
// captures headers, status code, and response body for caching while maintaining
// full compatibility with http.Flusher, http.Hijacker, and http.ResponseController.
type capturingResponseWriter struct {
	w             http.ResponseWriter
	header        http.Header
	buf           bytes.Buffer
	statusCode    int
	maxBytes      int64
	headerWritten bool
	streaming     bool
	overflow      bool
}

func newCapturingResponseWriter(w http.ResponseWriter, maxBytes int64) *capturingResponseWriter {
	return &capturingResponseWriter{
		w:        w,
		header:   make(http.Header),
		maxBytes: maxBytes,
	}
}

// Header returns the response header map.
func (c *capturingResponseWriter) Header() http.Header {
	return c.header
}

// WriteHeader records the status code or immediately commits if in streaming mode.
func (c *capturingResponseWriter) WriteHeader(code int) {
	if c.headerWritten {
		return
	}
	c.statusCode = code

	// Detect Server-Sent Events or upgrade headers immediately
	ct := c.header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") || c.header.Get("Upgrade") != "" {
		c.streaming = true
		c.commit()
	}
}

// Write writes data to the buffer or directly to the client if streaming/overflowing.
func (c *capturingResponseWriter) Write(b []byte) (int, error) {
	// If already in streaming mode, write straight through to client
	if c.streaming {
		c.commit()
		return c.w.Write(b)
	}

	// Check if this write exceeds the maximum allowed cacheable response size
	if c.maxBytes > 0 && int64(c.buf.Len()+len(b)) > c.maxBytes {
		c.overflow = true
		c.streaming = true
		c.commit()
		// Flush already buffered bytes to client first
		if c.buf.Len() > 0 {
			_, _ = c.w.Write(c.buf.Bytes())
			c.buf.Reset()
		}
		return c.w.Write(b)
	}

	return c.buf.Write(b)
}

// Flush implements http.Flusher, automatically transitioning into live streaming mode.
func (c *capturingResponseWriter) Flush() {
	c.streaming = true
	c.commit()
	if c.buf.Len() > 0 {
		_, _ = c.w.Write(c.buf.Bytes())
		c.buf.Reset()
	}
	if f, ok := c.w.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker for protocol upgrades (e.g. WebSockets).
func (c *capturingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c.streaming = true
	if hj, ok := c.w.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("precog: underlying ResponseWriter %T does not implement http.Hijacker", c.w)
}

// Unwrap supports Go 1.20+ http.ResponseController inspection.
func (c *capturingResponseWriter) Unwrap() http.ResponseWriter {
	return c.w
}

// commit sends recorded headers and status code to the underlying ResponseWriter.
func (c *capturingResponseWriter) commit() {
	if c.headerWritten {
		return
	}
	c.headerWritten = true
	for k, vv := range c.header {
		for _, v := range vv {
			c.w.Header().Add(k, v)
		}
	}
	if c.statusCode > 0 {
		c.w.WriteHeader(c.statusCode)
	} else {
		c.w.WriteHeader(http.StatusOK)
	}
}

// BodyBytes returns the recorded response body slice.
func (c *capturingResponseWriter) BodyBytes() []byte {
	return c.buf.Bytes()
}

// StatusCode returns the recorded status code (defaults to 200 OK).
func (c *capturingResponseWriter) StatusCode() int {
	if c.statusCode > 0 {
		return c.statusCode
	}
	return http.StatusOK
}

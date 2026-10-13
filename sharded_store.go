package precog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
)

const (
	// DefaultShards is 256 striped partitions to eliminate lock contention.
	DefaultShards = 256
	// DefaultMaxShardSize is 1 MB per shard = 256 MB total.
	DefaultMaxShardSize = 1024 * 1024
	// HeaderSize for binary entry serialization:
	// expireNano(8) + keyHash(8) + statusCode(2) + contentTypeLen(2) + bodyLen(4) + wireLen(4) = 28 bytes
	entryHeaderSize = 28
)

// ShardedConfig configures the ShardedStore.
type ShardedConfig struct {
	NumShards    int
	MaxCostBytes int64
}

// ShardedStore is a partitioned byte-arena cache implementing Store.
// It uses primitive uint64 keys and uint32 offsets to minimize garbage collector scanning.
type ShardedStore struct {
	shards    []*cacheShard
	numShards uint64
}

type cacheShard struct {
	mu      sync.RWMutex
	entries map[uint64]uint32
	ring    []byte
	tail    uint32
	maxSize uint32
}

// NewShardedStore initializes a new sharded byte store.
func NewShardedStore(cfg *ShardedConfig) *ShardedStore {
	numShards := DefaultShards
	var maxBytes int64 = DefaultShards * DefaultMaxShardSize

	if cfg != nil {
		if cfg.NumShards > 0 {
			numShards = cfg.NumShards
		}
		if cfg.MaxCostBytes > 0 {
			maxBytes = cfg.MaxCostBytes
		}
	}

	shardMaxSize := uint32(maxBytes / int64(numShards))
	if shardMaxSize < 64*1024 {
		shardMaxSize = 64 * 1024 // 64KB minimum per shard
	}

	shards := make([]*cacheShard, numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = &cacheShard{
			entries: make(map[uint64]uint32),
			ring:    make([]byte, 0, shardMaxSize/4),
			maxSize: shardMaxSize,
		}
	}

	return &ShardedStore{
		shards:    shards,
		numShards: uint64(numShards),
	}
}

func (s *ShardedStore) shardFor(h uint64) *cacheShard {
	return s.shards[h%s.numShards]
}

// PreCookWireResponse serializes an HTTP response into a ready-to-stream wire format.
func PreCookWireResponse(statusCode int, contentType string, body []byte) []byte {
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = "OK"
	}
	var buf bytes.Buffer
	buf.Grow(160 + len(body))
	fmt.Fprintf(&buf, "HTTP/1.1 %d %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nX-Cache: HIT\r\nX-Precog: ram-cache\r\nConnection: keep-alive\r\n\r\n",
		statusCode, statusText, contentType, len(body))
	buf.Write(body)
	return buf.Bytes()
}

// WriteResponse streams the response payload, leveraging connection hijacking when supported.
func (e *ResponseEntry) WriteResponse(w http.ResponseWriter) (int64, error) {
	if hj, ok := w.(http.Hijacker); ok && len(e.WireBytes) > 0 {
		conn, bufrw, err := hj.Hijack()
		if err == nil {
			defer conn.Close()
			n, err := bufrw.Write(e.WireBytes)
			if err == nil {
				_ = bufrw.Flush()
				return int64(n), nil
			}
		}
	}

	// Standard ResponseWriter streaming
	if e.ContentType != "" {
		w.Header().Set("Content-Type", e.ContentType)
	}
	w.Header().Set("X-Cache", "HIT")
	w.Header().Set("X-Precog", "ram-cache")
	w.WriteHeader(e.StatusCode)
	n, err := w.Write(e.Body)
	return int64(n), err
}

// Get implements Store.
func (s *ShardedStore) Get(key string) (*ResponseEntry, bool) {
	h := xxhash.Sum64String(key)
	return s.GetByHash(h)
}

// GetByHash retrieves directly by uint64 hash, avoiding string overhead.
func (s *ShardedStore) GetByHash(h uint64) (*ResponseEntry, bool) {
	sh := s.shardFor(h)
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	offset, exists := sh.entries[h]
	if !exists {
		return nil, false
	}

	if int(offset)+entryHeaderSize > len(sh.ring) {
		return nil, false
	}

	// Read header
	expireNano := int64(binary.LittleEndian.Uint64(sh.ring[offset : offset+8]))
	storedHash := binary.LittleEndian.Uint64(sh.ring[offset+8 : offset+16])
	statusCode := int(binary.LittleEndian.Uint16(sh.ring[offset+16 : offset+18]))
	ctLen := int(binary.LittleEndian.Uint16(sh.ring[offset+18 : offset+20]))
	bodyLen := int(binary.LittleEndian.Uint32(sh.ring[offset+20 : offset+24]))
	wireLen := int(binary.LittleEndian.Uint32(sh.ring[offset+24 : offset+28]))

	if storedHash != h {
		return nil, false
	}

	// Check TTL expiration
	if expireNano > 0 && time.Now().UnixNano() > expireNano {
		return nil, false
	}

	cursor := int(offset) + entryHeaderSize
	if cursor+ctLen+bodyLen+wireLen > len(sh.ring) {
		return nil, false
	}

	contentType := string(sh.ring[cursor : cursor+ctLen])
	cursor += ctLen

	body := make([]byte, bodyLen)
	copy(body, sh.ring[cursor:cursor+bodyLen])
	cursor += bodyLen

	var wireBytes []byte
	if wireLen > 0 {
		wireBytes = make([]byte, wireLen)
		copy(wireBytes, sh.ring[cursor:cursor+wireLen])
	}

	entry := &ResponseEntry{
		StatusCode:  statusCode,
		ContentType: contentType,
		Body:        body,
		WireBytes:   wireBytes,
		CreatedAt:   time.Now(),
	}

	return entry, true
}

// Set implements Store.
func (s *ShardedStore) Set(key string, entry *ResponseEntry, ttl time.Duration) bool {
	h := xxhash.Sum64String(key)
	return s.SetByHash(h, entry, ttl)
}

// SetByHash writes directly using uint64 key hash with synchronous write consistency.
func (s *ShardedStore) SetByHash(h uint64, entry *ResponseEntry, ttl time.Duration) bool {
	if entry == nil {
		return false
	}

	// Ensure pre-cooked wire bytes exist
	wire := entry.WireBytes
	if len(wire) == 0 && len(entry.Body) > 0 {
		wire = PreCookWireResponse(entry.StatusCode, entry.ContentType, entry.Body)
	}

	var expireNano int64
	if ttl > 0 {
		expireNano = time.Now().Add(ttl).UnixNano()
	}

	ctBytes := []byte(entry.ContentType)
	ctLen := len(ctBytes)
	bodyLen := len(entry.Body)
	wireLen := len(wire)

	totalEntryLen := entryHeaderSize + ctLen + bodyLen + wireLen

	sh := s.shardFor(h)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	// If entry exceeds max shard size entirely, skip
	if uint32(totalEntryLen) > sh.maxSize {
		return false
	}

	// Ring capacity check & reset if full
	if uint32(len(sh.ring))+uint32(totalEntryLen) > sh.maxSize {
		// Reset ring buffer and clear index when max capacity reached (FIFO flush)
		sh.ring = sh.ring[:0]
		sh.entries = make(map[uint64]uint32)
	}

	offset := uint32(len(sh.ring))

	// Serialize entry binary header
	var hdr [entryHeaderSize]byte
	binary.LittleEndian.PutUint64(hdr[0:8], uint64(expireNano))
	binary.LittleEndian.PutUint64(hdr[8:16], h)
	binary.LittleEndian.PutUint16(hdr[16:18], uint16(entry.StatusCode))
	binary.LittleEndian.PutUint16(hdr[18:20], uint16(ctLen))
	binary.LittleEndian.PutUint32(hdr[20:24], uint32(bodyLen))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(wireLen))

	sh.ring = append(sh.ring, hdr[:]...)
	sh.ring = append(sh.ring, ctBytes...)
	sh.ring = append(sh.ring, entry.Body...)
	sh.ring = append(sh.ring, wire...)

	sh.entries[h] = offset
	return true
}

// Del implements Store.
func (s *ShardedStore) Del(key string) {
	h := xxhash.Sum64String(key)
	sh := s.shardFor(h)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	delete(sh.entries, h)
}

// Close implements Store.
func (s *ShardedStore) Close() error {
	for _, sh := range s.shards {
		sh.mu.Lock()
		sh.ring = nil
		sh.entries = nil
		sh.mu.Unlock()
	}
	return nil
}

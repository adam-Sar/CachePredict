package precog

import (
	"encoding/binary"
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
	// expireNano(8) + keyHash(8) + statusCode(2) + contentTypeLen(2) + headersLen(4) + bodyLen(4) = 28 bytes
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
	maxSize uint32
	closed  bool
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

// WriteResponse streams the cached response via the standard HTTP ResponseWriter.
func (e *ResponseEntry) WriteResponse(w http.ResponseWriter) (int64, error) {
	// Replay original response headers
	for k, vv := range e.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	if e.ContentType != "" && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", e.ContentType)
	}
	w.Header().Set("X-Cache", "HIT")
	w.Header().Set("X-Precog", "ram-cache")

	if e.StatusCode > 0 {
		w.WriteHeader(e.StatusCode)
	} else {
		w.WriteHeader(http.StatusOK)
	}
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

	if sh.closed || sh.entries == nil {
		return nil, false
	}

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
	hdrLen := int(binary.LittleEndian.Uint32(sh.ring[offset+20 : offset+24]))
	bodyLen := int(binary.LittleEndian.Uint32(sh.ring[offset+24 : offset+28]))

	if storedHash != h {
		return nil, false
	}

	// Check TTL expiration
	if expireNano > 0 && time.Now().UnixNano() > expireNano {
		return nil, false
	}

	cursor := int(offset) + entryHeaderSize
	if cursor+ctLen+hdrLen+bodyLen > len(sh.ring) {
		return nil, false
	}

	contentType := string(sh.ring[cursor : cursor+ctLen])
	cursor += ctLen

	hdr := decodeHeaders(sh.ring[cursor : cursor+hdrLen])
	cursor += hdrLen

	body := make([]byte, bodyLen)
	copy(body, sh.ring[cursor:cursor+bodyLen])

	var expTime time.Time
	if expireNano > 0 {
		expTime = time.Unix(0, expireNano)
	}

	entry := &ResponseEntry{
		StatusCode:  statusCode,
		ContentType: contentType,
		Header:      hdr,
		Body:        body,
		CreatedAt:   time.Now(),
		ExpiresAt:   expTime,
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

	var expireNano int64
	if ttl > 0 {
		expireNano = time.Now().Add(ttl).UnixNano()
		entry.ExpiresAt = time.Unix(0, expireNano)
	}

	ctBytes := []byte(entry.ContentType)
	ctLen := len(ctBytes)
	hdrBytes := encodeHeaders(entry.Header)
	hdrLen := len(hdrBytes)
	bodyLen := len(entry.Body)

	totalEntryLen := entryHeaderSize + ctLen + hdrLen + bodyLen

	sh := s.shardFor(h)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if sh.closed || sh.entries == nil {
		return false
	}

	// If entry exceeds max shard size entirely, skip
	if uint32(totalEntryLen) > sh.maxSize {
		return false
	}

	// Capacity check & reset if full
	if uint32(len(sh.ring))+uint32(totalEntryLen) > sh.maxSize {
		sh.ring = sh.ring[:0]
		sh.entries = make(map[uint64]uint32)
	}

	offset := uint32(len(sh.ring))

	// Serialize entry binary header (28 bytes)
	var hdr [entryHeaderSize]byte
	binary.LittleEndian.PutUint64(hdr[0:8], uint64(expireNano))
	binary.LittleEndian.PutUint64(hdr[8:16], h)
	binary.LittleEndian.PutUint16(hdr[16:18], uint16(entry.StatusCode))
	binary.LittleEndian.PutUint16(hdr[18:20], uint16(ctLen))
	binary.LittleEndian.PutUint32(hdr[20:24], uint32(hdrLen))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(bodyLen))

	sh.ring = append(sh.ring, hdr[:]...)
	sh.ring = append(sh.ring, ctBytes...)
	sh.ring = append(sh.ring, hdrBytes...)
	sh.ring = append(sh.ring, entry.Body...)

	sh.entries[h] = offset
	return true
}

func encodeHeaders(h http.Header) []byte {
	if len(h) == 0 {
		return nil
	}
	size := 2 // uint16 header count
	for k, vv := range h {
		size += 2 + len(k) + 2
		for _, v := range vv {
			size += 4 + len(v)
		}
	}
	buf := make([]byte, size)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(len(h)))
	idx := 2
	for k, vv := range h {
		kLen := len(k)
		binary.LittleEndian.PutUint16(buf[idx:idx+2], uint16(kLen))
		idx += 2
		copy(buf[idx:idx+kLen], k)
		idx += kLen

		binary.LittleEndian.PutUint16(buf[idx:idx+2], uint16(len(vv)))
		idx += 2
		for _, v := range vv {
			vLen := len(v)
			binary.LittleEndian.PutUint32(buf[idx:idx+4], uint32(vLen))
			idx += 4
			copy(buf[idx:idx+vLen], v)
			idx += vLen
		}
	}
	return buf
}

func decodeHeaders(b []byte) http.Header {
	if len(b) < 2 {
		return make(http.Header)
	}
	numHeaders := int(binary.LittleEndian.Uint16(b[0:2]))
	idx := 2
	h := make(http.Header, numHeaders)
	for i := 0; i < numHeaders && idx+2 <= len(b); i++ {
		kLen := int(binary.LittleEndian.Uint16(b[idx : idx+2]))
		idx += 2
		if idx+kLen > len(b) {
			break
		}
		key := string(b[idx : idx+kLen])
		idx += kLen

		if idx+2 > len(b) {
			break
		}
		numValues := int(binary.LittleEndian.Uint16(b[idx : idx+2]))
		idx += 2

		vals := make([]string, 0, numValues)
		for j := 0; j < numValues && idx+4 <= len(b); j++ {
			vLen := int(binary.LittleEndian.Uint32(b[idx : idx+4]))
			idx += 4
			if idx+vLen > len(b) {
				break
			}
			val := string(b[idx : idx+vLen])
			idx += vLen
			vals = append(vals, val)
		}
		h[key] = vals
	}
	return h
}

// Del implements Store.
func (s *ShardedStore) Del(key string) {
	h := xxhash.Sum64String(key)
	sh := s.shardFor(h)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if !sh.closed && sh.entries != nil {
		delete(sh.entries, h)
	}
}

// Close implements Store.
func (s *ShardedStore) Close() error {
	for _, sh := range s.shards {
		sh.mu.Lock()
		sh.closed = true
		sh.ring = nil
		sh.entries = nil
		sh.mu.Unlock()
	}
	return nil
}

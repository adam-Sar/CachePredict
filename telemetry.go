package precog

import (
	"sync/atomic"
)

// Stats represents real-time operational metrics for Precog.
type Stats struct {
	TotalRequests      uint64  `json:"total_requests"`
	CacheHits          uint64  `json:"cache_hits"`
	CacheMisses        uint64  `json:"cache_misses"`
	PrefetchesExecuted uint64  `json:"prefetches_executed"`
	HitRate            float64 `json:"hit_rate"`
}

// Telemetry tracks cache efficiency and speculative accuracy atomically.
type Telemetry struct {
	totalRequests      uint64
	cacheHits          uint64
	cacheMisses        uint64
	prefetchesExecuted uint64
}

func NewTelemetry() *Telemetry {
	return &Telemetry{}
}

func (t *Telemetry) RecordHit() {
	atomic.AddUint64(&t.totalRequests, 1)
	atomic.AddUint64(&t.cacheHits, 1)
}

func (t *Telemetry) RecordMiss() {
	atomic.AddUint64(&t.totalRequests, 1)
	atomic.AddUint64(&t.cacheMisses, 1)
}

func (t *Telemetry) RecordPrefetch() {
	atomic.AddUint64(&t.prefetchesExecuted, 1)
}

// Stats returns a point-in-time snapshot of the cache metrics.
func (t *Telemetry) Stats() Stats {
	total := atomic.LoadUint64(&t.totalRequests)
	hits := atomic.LoadUint64(&t.cacheHits)
	misses := atomic.LoadUint64(&t.cacheMisses)
	prefetches := atomic.LoadUint64(&t.prefetchesExecuted)

	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return Stats{
		TotalRequests:      total,
		CacheHits:          hits,
		CacheMisses:        misses,
		PrefetchesExecuted: prefetches,
		HitRate:            hitRate,
	}
}

package precog

import (
	"net/http"
	"time"
)

// Config configures the Precog SDK engine.
type Config struct {
	// Store is the cache storage backend. Defaults to RistrettoStore (in-memory) if nil.
	Store Store

	// Predictor is the AI navigation sequence predictor. Defaults to AdaptiveMarkovPredictor if nil.
	Predictor Predictor

	// ConfidenceThreshold is the minimum probability (0.0 to 1.0) required to trigger speculative pre-warming.
	// Default: 0.65
	ConfidenceThreshold float64

	// TTL is the duration responses remain in the cache.
	// Default: 60 seconds
	TTL time.Duration

	// SessionCookieName is the cookie key used to identify sessions.
	// Default: "precog_sid"
	SessionCookieName string

	// CookieSecure sets the Secure flag on the session cookie.
	CookieSecure bool

	// SessionExtractor optionally overrides session resolution (e.g. from JWT or custom header).
	SessionExtractor func(r *http.Request) string

	// SkipRoutes is a list of path prefixes that will never be cached or speculatively prefetched.
	SkipRoutes []string

	// Workers is the number of concurrent speculative prefetch goroutines.
	// Default: 4
	Workers int
}

// Engine represents the active Precog predictive caching runtime.
type Engine struct {
	config     Config
	store      Store
	predictor  Predictor
	prefetcher *Prefetcher
	telemetry  *Telemetry
}

// New initializes the Precog SDK with the given configuration.
func New(cfg Config) (*Engine, error) {
	if cfg.ConfidenceThreshold <= 0 {
		cfg.ConfidenceThreshold = 0.65
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 60 * time.Second
	}
	if cfg.SessionCookieName == "" {
		cfg.SessionCookieName = "precog_sid"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}

	// Initialize default store if not provided
	store := cfg.Store
	if store == nil {
		var err error
		store, err = NewRistrettoStore(0)
		if err != nil {
			return nil, err
		}
	}

	// Initialize default predictor if not provided
	predictor := cfg.Predictor
	if predictor == nil {
		predictor = NewAdaptiveMarkovPredictor()
	}

	telemetry := NewTelemetry()

	engine := &Engine{
		config:    cfg,
		store:     store,
		predictor: predictor,
		telemetry: telemetry,
	}

	return engine, nil
}

// Handler wraps the root HTTP handler with Precog predictive caching and registers it with the prefetcher.
func (e *Engine) Handler(rootHandler http.Handler) http.Handler {
	// Initialize prefetcher tied to this root handler
	if e.prefetcher == nil {
		e.prefetcher = NewPrefetcher(
			rootHandler,
			e.store,
			e.config.TTL,
			e.config.ConfidenceThreshold,
			e.config.Workers,
		)
		e.prefetcher.SetOnWarmed(func(job PrefetchJob, key string, bytes int) {
			e.telemetry.RecordPrefetch()
		})
	}
	return e.Middleware(rootHandler)
}

// Stats returns a snapshot of runtime metrics (hit rate, prefetches, latency saved).
func (e *Engine) Stats() Stats {
	return e.telemetry.Stats()
}

// Store returns the underlying cache storage.
func (e *Engine) Store() Store {
	return e.store
}

// Predictor returns the active prediction engine.
func (e *Engine) Predictor() Predictor {
	return e.predictor
}

// Close gracefully flushes background workers and closes the cache store.
func (e *Engine) Close() error {
	if e.prefetcher != nil {
		e.prefetcher.Close()
	}
	return e.store.Close()
}

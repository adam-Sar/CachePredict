package precog

import (
	"net/http"
	"sync"
	"time"
)

// Config configures the Precog SDK engine.
type Config struct {
	// Store is the cache storage backend. Defaults to TieredStore wrapping ShardedStore if nil.
	Store Store

	// Predictor is the navigation sequence predictor. Defaults to DenseMatrixPredictor if nil.
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

	// IsAuthenticated optionally determines whether a request carries authentication credentials.
	// Responses to authenticated requests will NOT be stored in the shared cache unless
	// the response explicitly contains `Cache-Control: public`.
	// If nil, requests with Authorization, Proxy-Authorization, token headers, or cookies
	// other than SessionCookieName are treated as authenticated.
	IsAuthenticated func(r *http.Request) bool

	// AuthCookieNames is an optional list of cookie names that indicate an authenticated session.
	// If empty, any cookie other than SessionCookieName is considered an auth credential.
	AuthCookieNames []string

	// SkipRoutes is a list of path prefixes that will never be cached or speculatively prefetched.
	SkipRoutes []string

	// Workers is the number of concurrent speculative prefetch goroutines.
	// Default: 4
	Workers int
}

// Engine represents the active Precog predictive caching runtime.
type Engine struct {
	config       Config
	store        Store
	predictor    Predictor
	prefetcher   *Prefetcher
	prefetchOnce sync.Once
	telemetry    *Telemetry
	inflight     *InflightGroup
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

	// Initialize default store if not provided (two-tier L1/L2 sharded store)
	store := cfg.Store
	if store == nil {
		store = NewTieredStore(NewShardedStore(nil))
	}

	// Initialize default predictor if not provided (dense transition matrix)
	predictor := cfg.Predictor
	if predictor == nil {
		predictor = NewDenseMatrixPredictor(nil)
	}

	telemetry := NewTelemetry()
	inflight := NewInflightGroup()

	engine := &Engine{
		config:    cfg,
		store:     store,
		predictor: predictor,
		telemetry: telemetry,
		inflight:  inflight,
	}

	return engine, nil
}

// Handler wraps the root HTTP handler with Precog predictive caching and registers it with the prefetcher.
func (e *Engine) Handler(rootHandler http.Handler) http.Handler {
	// Initialize prefetcher tied to this root handler safely once
	e.prefetchOnce.Do(func() {
		e.prefetcher = NewPrefetcher(
			rootHandler,
			e.store,
			e.inflight,
			e.config.TTL,
			e.config.ConfidenceThreshold,
			e.config.Workers,
		)
		e.prefetcher.SetAuthChecker(e.IsRequestAuthenticated)
		e.prefetcher.SetOnWarmed(func(job PrefetchJob, key string, bytes int) {
			e.telemetry.RecordPrefetch()
		})
	})
	return e.Middleware(rootHandler)
}

// IsRequestAuthenticated determines whether an incoming HTTP request carries user authentication or credentials.
func (e *Engine) IsRequestAuthenticated(r *http.Request) bool {
	if e.config.IsAuthenticated != nil {
		return e.config.IsAuthenticated(r)
	}
	return isDefaultAuthenticated(r, e.config.SessionCookieName, e.config.AuthCookieNames)
}

func isDefaultAuthenticated(r *http.Request, sessionCookieName string, authCookieNames []string) bool {
	if r == nil {
		return false
	}
	if r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
		return true
	}
	if r.Header.Get("X-Auth-Token") != "" || r.Header.Get("X-API-Key") != "" {
		return true
	}

	cookies := r.Cookies()
	if len(cookies) == 0 {
		return false
	}

	if len(authCookieNames) > 0 {
		for _, c := range cookies {
			for _, name := range authCookieNames {
				if c.Name == name && c.Value != "" {
					return true
				}
			}
		}
		return false
	}

	for _, c := range cookies {
		if (sessionCookieName == "" || c.Name != sessionCookieName) && c.Value != "" {
			return true
		}
	}
	return false
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

package precog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// PrefetchJob holds information needed to execute a speculative background fetch.
type PrefetchJob struct {
	SessionID   string
	Prediction  Prediction
	AuthHeaders http.Header
	Cookies     []*http.Cookie
}

// Prefetcher executes speculative API requests in the background to warm the cache.
type Prefetcher struct {
	handler    http.Handler
	store      Store
	inflight   *InflightGroup
	ttl        time.Duration
	threshold  float64
	jobChan    chan PrefetchJob
	workers    int
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	onWarmed   func(job PrefetchJob, key string, bytes int)
}

// NewPrefetcher initializes a worker pool for speculative cache warming.
func NewPrefetcher(handler http.Handler, store Store, inflight *InflightGroup, ttl time.Duration, threshold float64, workers int) *Prefetcher {
	if workers <= 0 {
		workers = 4
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Prefetcher{
		handler:   handler,
		store:     store,
		inflight:  inflight,
		ttl:       ttl,
		threshold: threshold,
		jobChan:   make(chan PrefetchJob, 256),
		workers:   workers,
		ctx:       ctx,
		cancel:    cancel,
	}

	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// SetOnWarmed registers a callback invoked whenever an endpoint is speculatively warmed.
func (p *Prefetcher) SetOnWarmed(fn func(job PrefetchJob, key string, bytes int)) {
	p.onWarmed = fn
}

// Submit queues predictions that meet the confidence threshold for background warming.
func (p *Prefetcher) Submit(sessionID string, preds []Prediction, origReq *http.Request) {
	if p.handler == nil {
		return
	}

	// Capture auth headers and cookies from the active session
	headers := make(http.Header)
	if auth := origReq.Header.Get("Authorization"); auth != "" {
		headers.Set("Authorization", auth)
	}
	if cookieHeader := origReq.Header.Get("Cookie"); cookieHeader != "" {
		headers.Set("Cookie", cookieHeader)
	}
	cookies := origReq.Cookies()

	for _, pred := range preds {
		if pred.Probability < p.threshold {
			continue
		}

		select {
		case p.jobChan <- PrefetchJob{
			SessionID:   sessionID,
			Prediction:  pred,
			AuthHeaders: headers,
			Cookies:     cookies,
		}:
		default:
			// Queue full; drop speculative job to preserve resources
		}
	}
}

func (p *Prefetcher) worker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobChan:
			if !ok {
				return
			}
			p.execute(job)
		}
	}
}

func (p *Prefetcher) execute(job PrefetchJob) {
	parts := strings.SplitN(job.Prediction.Call, " ", 2)
	if len(parts) != 2 {
		return
	}
	method := strings.ToUpper(parts[0])
	targetURI := parts[1]

	// Only speculatively execute safe, idempotent methods
	if method != "GET" && method != "HEAD" {
		return
	}

	targetPath := targetURI
	rawQuery := ""
	if qIdx := strings.Index(targetURI, "?"); qIdx >= 0 {
		targetPath = targetURI[:qIdx]
		rawQuery = targetURI[qIdx+1:]
	}

	cacheKey := CanonicalCacheKey(method, targetPath, rawQuery, "")
	// If already in RAM, avoid redundant compute
	if _, exists := p.store.Get(cacheKey); exists {
		return
	}

	execFn := func() (*ResponseEntry, error) {
		// Double-check cache inside singleflight
		if cached, exists := p.store.Get(cacheKey); exists {
			return cached, nil
		}

		// Build in-process synthetic request
		reqCtx, reqCancel := context.WithTimeout(p.ctx, 3*time.Second)
		defer reqCancel()

		syntheticReq := httptest.NewRequest(method, targetURI, nil).WithContext(reqCtx)
		for k, v := range job.AuthHeaders {
			syntheticReq.Header[k] = v
		}
		for _, c := range job.Cookies {
			syntheticReq.AddCookie(c)
		}
		syntheticReq.Header.Set("X-Precog-Speculative", "true")

		// Execute through application handler with recorder
		rec := httptest.NewRecorder()
		func() {
			defer func() {
				_ = recover()
			}()
			p.handler.ServeHTTP(rec, syntheticReq)
		}()

		if rec.Code == http.StatusOK {
			body := rec.Body.Bytes()
			contentType := rec.Header().Get("Content-Type")
			entry := &ResponseEntry{
				Body:        body,
				ContentType: contentType,
				StatusCode:  rec.Code,
				CreatedAt:   time.Now(),
				WireBytes:   PreCookWireResponse(rec.Code, contentType, body),
			}
			p.store.Set(cacheKey, entry, p.ttl)
			if p.onWarmed != nil {
				p.onWarmed(job, cacheKey, len(body))
			}
			return entry, nil
		}
		return nil, nil
	}

	if p.inflight != nil {
		_, _ = p.inflight.Do(cacheKey, execFn)
	} else {
		_, _ = execFn()
	}
}

// Close gracefully terminates the worker pool.
func (p *Prefetcher) Close() {
	p.cancel()
	close(p.jobChan)
	p.wg.Wait()
}

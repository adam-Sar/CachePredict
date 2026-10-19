package precog

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cespare/xxhash/v2"
)

const (
	// DefaultMaxDenseRoutes defines the maximum route matrix dimension (1024 x 1024).
	DefaultMaxDenseRoutes = 1024
	// NumSessionShards partitions user sessions to minimize mutex contention.
	NumSessionShards = 64
)

// DenseMatrixPredictor models endpoint transition probabilities using a dense contiguous matrix.
// State transitions are updated atomically using atomic CPU instructions.
type DenseMatrixPredictor struct {
	registry   *EndpointRegistry
	maxRoutes  uint32
	matrix     []uint32 // Contiguous flat array: index = fromID * maxRoutes + toID
	totals     []uint32 // Total transitions out of fromID: index = fromID
	shards     [NumSessionShards]sessionShard
	maxHistory int
	concreteMu sync.RWMutex
	concrete   map[uint16]string
}

type sessionShard struct {
	mu       sync.RWMutex
	sessions map[string][]denseStep
}

type denseStep struct {
	id       uint16
	concrete string
	pattern  string
	path     string
	query    string
}

// DensePredictorConfig configures the DenseMatrixPredictor.
type DensePredictorConfig struct {
	MaxRoutes  int
	MaxHistory int
	Registry   *EndpointRegistry
}

// NewDenseMatrixPredictor creates a zero-GC contiguous matrix predictor.
func NewDenseMatrixPredictor(cfg *DensePredictorConfig) *DenseMatrixPredictor {
	maxRoutes := uint32(DefaultMaxDenseRoutes)
	maxHistory := 10
	var reg *EndpointRegistry

	if cfg != nil {
		if cfg.MaxRoutes > 0 {
			maxRoutes = uint32(cfg.MaxRoutes)
		}
		if cfg.MaxHistory > 0 {
			maxHistory = cfg.MaxHistory
		}
		reg = cfg.Registry
	}

	if reg == nil {
		reg = NewEndpointRegistry()
	}

	p := &DenseMatrixPredictor{
		registry:   reg,
		maxRoutes:  maxRoutes,
		matrix:     make([]uint32, maxRoutes*maxRoutes),
		totals:     make([]uint32, maxRoutes),
		maxHistory: maxHistory,
		concrete:   make(map[uint16]string),
	}

	for i := 0; i < NumSessionShards; i++ {
		p.shards[i].sessions = make(map[string][]denseStep)
	}

	return p
}

func (p *DenseMatrixPredictor) shardFor(sessionID string) *sessionShard {
	h := xxhash.Sum64String(sessionID)
	return &p.shards[h%NumSessionShards]
}

// Record observes an API interaction and updates the contiguous transition matrix
// using lock-free atomic instructions.
func (p *DenseMatrixPredictor) Record(sessionID string, method, path, rawQuery string) {
	method = strings.ToUpper(method)
	pattern := NormalizeRoute(method, path)

	toID := p.registry.GetOrRegister(pattern)
	if toID == InvalidRouteID || uint32(toID) >= p.maxRoutes {
		return
	}

	concrete := method + " " + path
	if rawQuery != "" {
		concrete += "?" + rawQuery
	}

	step := denseStep{
		id:       toID,
		concrete: concrete,
		pattern:  pattern,
		path:     path,
		query:    rawQuery,
	}

	// Only cache concrete paths for purely static routes (routes with no parameters).
	// Parameterized routes (:id, :uuid, :hex) must never be globally cached to prevent cross-session leaks.
	if !strings.Contains(pattern, ":id") && !strings.Contains(pattern, ":uuid") && !strings.Contains(pattern, ":hex") {
		p.concreteMu.Lock()
		p.concrete[toID] = concrete
		p.concreteMu.Unlock()
	}

	sh := p.shardFor(sessionID)
	sh.mu.Lock()

	hist := sh.sessions[sessionID]
	var fromID uint16
	hasPrior := len(hist) > 0
	if hasPrior {
		fromID = hist[len(hist)-1].id
	}

	// Bound total tracked sessions in shard to prevent memory leak
	if len(hist) == 0 && len(sh.sessions) >= 500 {
		for k := range sh.sessions {
			delete(sh.sessions, k)
			break
		}
	}

	// Append to session history (capped at maxHistory)
	hist = append(hist, step)
	if len(hist) > p.maxHistory {
		hist = hist[len(hist)-p.maxHistory:]
	}
	sh.sessions[sessionID] = hist
	sh.mu.Unlock()

	// Update transition matrix lock-free via atomic instructions
	if hasPrior && uint32(fromID) < p.maxRoutes {
		idx := uint32(fromID)*p.maxRoutes + uint32(toID)
		atomic.AddUint32(&p.matrix[idx], 1)
		atomic.AddUint32(&p.totals[fromID], 1)
	}
}

type candidate struct {
	toID  uint16
	count uint32
}

// Predict calculates top-K candidate routes for the active session.
func (p *DenseMatrixPredictor) Predict(sessionID string, topK int) []Prediction {
	sh := p.shardFor(sessionID)
	sh.mu.RLock()
	hist := sh.sessions[sessionID]
	if len(hist) == 0 {
		sh.mu.RUnlock()
		return nil
	}
	lastStep := hist[len(hist)-1]
	sh.mu.RUnlock()

	fromID := lastStep.id
	if uint32(fromID) >= p.maxRoutes {
		return nil
	}

	total := atomic.LoadUint32(&p.totals[fromID])
	if total == 0 {
		return nil
	}

	// Scan contiguous row from contiguous memory
	registeredCount := uint32(p.registry.Count())
	if registeredCount > p.maxRoutes {
		registeredCount = p.maxRoutes
	}

	rowStart := uint32(fromID) * p.maxRoutes
	candidates := make([]candidate, 0, 16)

	for toID := uint32(0); toID < registeredCount; toID++ {
		count := atomic.LoadUint32(&p.matrix[rowStart+toID])
		if count > 0 {
			candidates = append(candidates, candidate{
				toID:  uint16(toID),
				count: count,
			})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].count > candidates[j].count
	})

	if topK > len(candidates) {
		topK = len(candidates)
	}

	predictions := make([]Prediction, 0, topK)
	for i := 0; i < topK; i++ {
		cand := candidates[i]
		prob := float64(cand.count) / float64(total)
		pat := p.registry.Pattern(cand.toID)

		concreteCall := p.hydrateCall(cand.toID, pat, lastStep)

		predictions = append(predictions, Prediction{
			Call:        concreteCall,
			Pattern:     pat,
			Probability: prob,
		})
	}

	return predictions
}

func (p *DenseMatrixPredictor) hydrateCall(candToID uint16, pattern string, lastStep denseStep) string {
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) < 2 {
		return pattern
	}
	method := parts[0]
	pathPattern := parts[1]

	call := method + " " + pathPattern

	var activeNumID string
	var activeUUID string
	var activeHexID string

	// 1. Extract IDs from lastStep.path segments (from right to left)
	segments := strings.Split(lastStep.path, "/")
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if seg == "" {
			continue
		}
		if activeNumID == "" && isNumeric(seg) {
			activeNumID = seg
		}
		if activeUUID == "" && isUUID(seg) {
			activeUUID = seg
		}
		if activeHexID == "" && isHexID(seg) {
			activeHexID = seg
		}
	}

	// 2. Extract IDs from lastStep.query parameters
	if (activeNumID == "" || activeUUID == "" || activeHexID == "") && lastStep.query != "" {
		for _, qp := range strings.Split(lastStep.query, "&") {
			kv := strings.SplitN(qp, "=", 2)
			if len(kv) == 2 {
				val := kv[1]
				if activeNumID == "" && (kv[0] == "id" || kv[0] == "product_id" || kv[0] == "order_id" || kv[0] == "item_id") && isNumeric(val) {
					activeNumID = val
				}
				if activeUUID == "" && isUUID(val) {
					activeUUID = val
				}
				if activeHexID == "" && isHexID(val) {
					activeHexID = val
				}
			}
		}
	}

	// 3. Hydrate placeholders from the active session context
	if activeNumID != "" && strings.Contains(call, ":id") {
		call = strings.Replace(call, ":id", activeNumID, 1)
	}
	if activeUUID != "" && strings.Contains(call, ":uuid") {
		call = strings.Replace(call, ":uuid", activeUUID, 1)
	}
	if activeHexID != "" && strings.Contains(call, ":hex") {
		call = strings.Replace(call, ":hex", activeHexID, 1)
	}

	// 4. For purely static routes (routes with NO placeholders), if there is a known static concrete URL
	// with query parameters from p.concrete, we can carry forward the query context safely.
	// We deliberately NEVER look up in p.concrete for parameterized routes to prevent cross-session ID leaks.
	if !strings.Contains(pattern, ":id") && !strings.Contains(pattern, ":uuid") && !strings.Contains(pattern, ":hex") {
		p.concreteMu.RLock()
		known := p.concrete[candToID]
		p.concreteMu.RUnlock()
		if known != "" && !strings.Contains(call, "?") && strings.Contains(known, "?") {
			qIdx := strings.Index(known, "?")
			call += known[qIdx:]
		}
	}

	return call
}

func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ResetSession clears navigation history for a session.
func (p *DenseMatrixPredictor) ResetSession(sessionID string) {
	sh := p.shardFor(sessionID)
	sh.mu.Lock()
	delete(sh.sessions, sessionID)
	sh.mu.Unlock()
}

// Registry returns the underlying dynamic endpoint registry.
func (p *DenseMatrixPredictor) Registry() *EndpointRegistry {
	return p.registry
}

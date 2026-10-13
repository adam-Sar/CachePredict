package precog

import (
	"sort"
	"strings"
	"sync"
)

// Prediction represents a speculative next API endpoint.
type Prediction struct {
	Call        string  // e.g. "GET /cart" or "GET /reviews?product_id=44"
	Pattern     string  // e.g. "GET /reviews"
	Probability float64 // Confidence score between 0.0 and 1.0
}

// Predictor models the user's sequential API access patterns.
type Predictor interface {
	Record(sessionID string, method, path, rawQuery string)
	Predict(sessionID string, topK int) []Prediction
	ResetSession(sessionID string)
}

// AdaptiveMarkovPredictor models navigation sequences using a map-based Markov chain.
type AdaptiveMarkovPredictor struct {
	mu          sync.RWMutex
	maxHistory  int
	sessions    map[string][]historyStep
	transitions map[string]map[string]int
	totals      map[string]int
}

type historyStep struct {
	concrete string // e.g. "GET /products?id=44"
	pattern  string // e.g. "GET /products/:id"
	path     string
	query    string
}

// NewAdaptiveMarkovPredictor creates a map-based Markov chain predictor.
func NewAdaptiveMarkovPredictor() *AdaptiveMarkovPredictor {
	return &AdaptiveMarkovPredictor{
		maxHistory:  10,
		sessions:    make(map[string][]historyStep),
		transitions: make(map[string]map[string]int),
		totals:      make(map[string]int),
	}
}

// Record observes an API call for a given session and updates the Markov transition matrix.
func (p *AdaptiveMarkovPredictor) Record(sessionID string, method, path, rawQuery string) {
	method = strings.ToUpper(method)
	pattern := NormalizeRoute(method, path)

	concrete := method + " " + path
	if rawQuery != "" {
		concrete += "?" + rawQuery
	}

	step := historyStep{
		concrete: concrete,
		pattern:  pattern,
		path:     path,
		query:    rawQuery,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	hist := p.sessions[sessionID]
	if len(hist) > 0 {
		lastStep := hist[len(hist)-1]
		fromState := lastStep.pattern
		toState := pattern

		// Update transition matrix
		if p.transitions[fromState] == nil {
			p.transitions[fromState] = make(map[string]int)
		}
		p.transitions[fromState][toState]++
		p.totals[fromState]++
	}

	// Append to session history (capped at maxHistory)
	hist = append(hist, step)
	if len(hist) > p.maxHistory {
		hist = hist[len(hist)-p.maxHistory:]
	}
	p.sessions[sessionID] = hist
}

// Predict calculates the top-K highest-probability next endpoints for the active session.
func (p *AdaptiveMarkovPredictor) Predict(sessionID string, topK int) []Prediction {
	p.mu.RLock()
	defer p.mu.RUnlock()

	hist := p.sessions[sessionID]
	if len(hist) == 0 {
		return nil
	}

	lastStep := hist[len(hist)-1]
	fromState := lastStep.pattern

	nextCounts, ok := p.transitions[fromState]
	if !ok || len(nextCounts) == 0 {
		return nil
	}

	total := float64(p.totals[fromState])
	if total <= 0 {
		return nil
	}

	preds := make([]Prediction, 0, len(nextCounts))
	for nextPattern, count := range nextCounts {
		prob := float64(count) / total
		call := p.reconstructCall(nextPattern, lastStep)
		preds = append(preds, Prediction{
			Call:        call,
			Pattern:     nextPattern,
			Probability: prob,
		})
	}

	// Sort by highest probability descending
	sort.Slice(preds, func(i, j int) bool {
		return preds[i].Probability > preds[j].Probability
	})

	if topK > 0 && len(preds) > topK {
		preds = preds[:topK]
	}
	return preds
}

// reconstructCall converts an abstract pattern back into a concrete actionable route
// using context from the previous step.
func (p *AdaptiveMarkovPredictor) reconstructCall(targetPattern string, last historyStep) string {
	parts := strings.SplitN(targetPattern, " ", 2)
	if len(parts) != 2 {
		return targetPattern
	}
	method := parts[0]
	targetPath := parts[1]

	// If the pattern has :id and the last call had an ID or query, propagate it
	if strings.Contains(targetPath, ":id") {
		// Replace :id with the ID from previous call if available
		partsLast := strings.Split(last.path, "/")
		if len(partsLast) > 0 {
			lastID := partsLast[len(partsLast)-1]
			targetPath = strings.Replace(targetPath, ":id", lastID, 1)
		}
	}

	call := method + " " + targetPath
	// If query was relevant and target is query-dependent
	if last.query != "" && strings.Contains(targetPath, last.path) {
		call += "?" + last.query
	}
	return call
}

// SeedTransition manually adds a known transition probability (useful for bootstrap/config).
func (p *AdaptiveMarkovPredictor) SeedTransition(fromPattern, toPattern string, weight int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.transitions[fromPattern] == nil {
		p.transitions[fromPattern] = make(map[string]int)
	}
	p.transitions[fromPattern][toPattern] += weight
	p.totals[fromPattern] += weight
}

// ResetSession clears trajectory memory for a finished or logged-out session.
func (p *AdaptiveMarkovPredictor) ResetSession(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sessions, sessionID)
}

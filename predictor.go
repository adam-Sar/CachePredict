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
	maxSessions int
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
		maxSessions: 5000,
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

	// Bound total tracked sessions to prevent memory leak
	if len(hist) == 0 && len(p.sessions) >= p.maxSessions {
		for k := range p.sessions {
			delete(p.sessions, k)
			break
		}
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

	// Extract identifier segment from previous call if available
	segments := strings.Split(last.path, "/")
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if seg == "" {
			continue
		}
		if isNumeric(seg) && strings.Contains(targetPath, ":id") {
			targetPath = strings.Replace(targetPath, ":id", seg, 1)
			break
		} else if isUUID(seg) && strings.Contains(targetPath, ":uuid") {
			targetPath = strings.Replace(targetPath, ":uuid", seg, 1)
			break
		} else if isHexID(seg) && strings.Contains(targetPath, ":hex") {
			targetPath = strings.Replace(targetPath, ":hex", seg, 1)
			break
		}
	}

	call := method + " " + targetPath
	// If query was relevant and target is query-dependent
	if last.query != "" && strings.Contains(targetPath, last.path) {
		call += "?" + last.query
	}
	return call
}

func isUUID(s string) bool {
	return len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}

func isHexID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
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

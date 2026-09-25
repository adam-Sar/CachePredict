package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

// Prediction is one next-call prediction.
type Prediction struct {
	Call string  // e.g. "GET /cart?product_id=44"
	Prob float32 // softmax probability
}

// Tokenizer holds the vocab loaded from tokenizer.json.
type Tokenizer struct {
	MaxLen     int
	VocabSize  int
	StringToID map[string]int
	IDToString map[int]string
}

// tokenizerFile mirrors the JSON shape written by convert_to_onnx.py.
type tokenizerFile struct {
	MaxLen     int            `json:"max_len"`
	VocabSize  int            `json:"vocab_size"`
	PadValue   int            `json:"pad_value"`
	StringToID map[string]int `json:"string_to_id"`
	IDToString map[int]string `json:"id_to_string"`
	UNKToken   string         `json:"unk_token"`
}

// LoadTokenizer reads tokenizer.json from disk and returns a populated Tokenizer.
// Returns an error if the file is missing or its max_len is not positive.
func LoadTokenizer(path string) (*Tokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tokenizer: %w", err)
	}
	var f tokenizerFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse tokenizer: %w", err)
	}
	if f.MaxLen <= 0 {
		return nil, errors.New("tokenizer: max_len must be > 0")
	}
	return &Tokenizer{
		MaxLen:     f.MaxLen,
		VocabSize:  f.VocabSize,
		StringToID: f.StringToID,
		IDToString: f.IDToString,
	}, nil
}

// Encode converts history into a padded []int of length MaxLen, right-aligned
// (oldest at the end if truncated). Unknown tokens become 0 (PAD).
func (t *Tokenizer) Encode(history []string) []int {
	if len(history) > t.MaxLen {
		history = history[len(history)-t.MaxLen:]
	}
	ids := make([]int, t.MaxLen)
	for i, call := range history {
		id, ok := t.StringToID[call]
		if !ok {
			id = 0
		}
		ids[t.MaxLen-len(history)+i] = id
	}
	return ids
}

// predictorBackend is the implementation interface selected at build time:
// the ONNX-backed implementation lives in predictor_onnx.go (build tag cgo),
// the stub lives in predictor_stub.go (build tag !cgo).
type predictorBackend interface {
	Predict(history []string, topK int) ([]Prediction, error)
	Close() error
}

// Predictor wraps the loaded ONNX model and tokenizer. Safe for concurrent
// use: Predict takes an RLock (multiple inferences in parallel); Close takes
// a write lock.
type Predictor struct {
	mu        sync.RWMutex
	closed    bool
	backend   predictorBackend
	tokenizer *Tokenizer
}

// NewPredictor loads the .onnx model and tokenizer and returns a ready-to-use
// Predictor. Call ONCE at startup. The ONNX backend needs CGO; non-CGO builds
// fall back to a deterministic stub (see predictor_stub.go).
func NewPredictor(onnxPath, tokenizerPath string) (*Predictor, error) {
	tok, err := LoadTokenizer(tokenizerPath)
	if err != nil {
		return nil, err
	}
	be, err := newBackend(onnxPath, tok)
	if err != nil {
		return nil, err
	}
	return &Predictor{backend: be, tokenizer: tok}, nil
}

// Predict runs inference on history and returns the top-K predictions sorted
// by probability (highest first). Only the last MaxLen entries of history are
// used; older ones are dropped.
func (p *Predictor) Predict(history []string, topK int) ([]Prediction, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, errors.New("predictor: closed")
	}
	if p.backend == nil {
		p.mu.RUnlock()
		return nil, errors.New("predictor: not initialized")
	}
	backend := p.backend
	p.mu.RUnlock()
	return backend.Predict(history, topK)
}

// Close releases backend resources. Safe to call multiple times.
func (p *Predictor) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.backend == nil {
		return nil
	}
	return p.backend.Close()
}

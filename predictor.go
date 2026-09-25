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

// tokenizerFile is the on-disk JSON shape produced by convert_to_onnx.py.
// Field tags match tokenizer.json's keys.
type tokenizerFile struct {
	MaxLen     int            `json:"max_len"`
	VocabSize  int            `json:"vocab_size"`
	PadValue   int            `json:"pad_value"`
	StringToID map[string]int `json:"string_to_id"`
	IDToString map[int]string `json:"id_to_string"`
	UNKToken   string         `json:"unk_token"`
}

// LoadTokenizer reads tokenizer.json from disk and returns a usable Tokenizer.
// Inputs: path — path to tokenizer.json. Output: *Tokenizer, error.
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

// Encode converts a slice of API call strings into a padded int slice of
// length MaxLen, right-aligned (oldest at the end if truncated). Unknown
// strings become 0 (the PAD token id).
// Inputs: history — past call strings, oldest first. Output: []int of length MaxLen.
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

// Predictor wraps the loaded ONNX model and tokenizer. Safe for concurrent use.
type Predictor struct {
	mu        sync.Mutex
	closed    bool
	backend   predictorBackend
	tokenizer *Tokenizer
}

// NewPredictor loads the .onnx file and tokenizer from disk and returns a
// ready-to-use *Predictor. Call this ONCE at startup. The actual ONNX
// session creation requires CGO; on non-CGO builds a stub backend is used.
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

// Predict runs the model on the given history and returns top-K predictions
// sorted by probability (highest first). Inputs: history — past calls, oldest
// first (only last MaxLen used); topK — number of predictions to return.
// Output: []Prediction, error.
func (p *Predictor) Predict(history []string, topK int) ([]Prediction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("predictor: closed")
	}
	if p.backend == nil {
		return nil, errors.New("predictor: not initialized")
	}
	return p.backend.Predict(history, topK)
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

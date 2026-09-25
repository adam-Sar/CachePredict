package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// writeTokenizerFile drops a small tokenizer.json into t.TempDir() and
// returns its path.
func writeTokenizerFile(t *testing.T, dir string, tf tokenizerFile) string {
	t.Helper()
	data, err := json.Marshal(tf)
	if err != nil {
		t.Fatalf("marshal tokenizer: %v", err)
	}
	path := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	return path
}

func sampleTokenizer() tokenizerFile {
	return tokenizerFile{
		MaxLen:    8,
		VocabSize: 5,
		PadValue:  0,
		StringToID: map[string]int{
			"END":             0,
			"GET /products":   1,
			"GET /cart":       2,
			"POST /cart":      3,
			"GET /products/1": 4,
		},
		IDToString: map[int]string{
			0: "END",
			1: "GET /products",
			2: "GET /cart",
			3: "POST /cart",
			4: "GET /products/1",
		},
		UNKToken: "END",
	}
}

func TestLoadTokenizer(t *testing.T) {
	dir := t.TempDir()
	path := writeTokenizerFile(t, dir, sampleTokenizer())

	tok, err := LoadTokenizer(path)
	if err != nil {
		t.Fatalf("LoadTokenizer: %v", err)
	}
	if tok.MaxLen != 8 || tok.VocabSize != 5 {
		t.Errorf("MaxLen=%d VocabSize=%d; want 8/5", tok.MaxLen, tok.VocabSize)
	}
	if tok.StringToID["END"] != 0 {
		t.Errorf("END id = %d, want 0", tok.StringToID["END"])
	}
	if tok.IDToString[4] != "GET /products/1" {
		t.Errorf("id 4 = %q, want GET /products/1", tok.IDToString[4])
	}
}

func TestLoadTokenizerErrors(t *testing.T) {
	if _, err := LoadTokenizer(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("expected error for missing file")
	}

	dir := t.TempDir()
	path := writeTokenizerFile(t, dir, tokenizerFile{MaxLen: 0})
	if _, err := LoadTokenizer(path); err == nil {
		t.Error("expected error for max_len <= 0")
	}

	path = filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json"), 0o644)
	if _, err := LoadTokenizer(path); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestEncodeEmpty(t *testing.T) {
	tok := &Tokenizer{MaxLen: 4, VocabSize: 3, StringToID: map[string]int{}}
	ids := tok.Encode(nil)
	if len(ids) != 4 {
		t.Fatalf("len=%d, want 4", len(ids))
	}
	for i, v := range ids {
		if v != 0 {
			t.Errorf("ids[%d]=%d, want 0 (PAD)", i, v)
		}
	}
}

func TestEncodeShort(t *testing.T) {
	tok := &Tokenizer{
		MaxLen:     4,
		VocabSize:  3,
		StringToID: map[string]int{"A": 1, "B": 2},
	}
	ids := tok.Encode([]string{"A"})
	if want := []int{0, 0, 0, 1}; !equalInts(ids, want) {
		t.Errorf("ids=%v, want %v (right-aligned)", ids, want)
	}
}

func TestEncodeExact(t *testing.T) {
	tok := &Tokenizer{
		MaxLen:     4,
		VocabSize:  3,
		StringToID: map[string]int{"A": 1, "B": 2, "C": 3, "D": 4},
	}
	ids := tok.Encode([]string{"A", "B", "C", "D"})
	if want := []int{1, 2, 3, 4}; !equalInts(ids, want) {
		t.Errorf("ids=%v, want %v", ids, want)
	}
}

func TestEncodeTruncatesToLastN(t *testing.T) {
	tok := &Tokenizer{
		MaxLen:     3,
		VocabSize:  5,
		StringToID: map[string]int{"A": 1, "B": 2, "C": 3, "D": 4, "E": 5},
	}
	ids := tok.Encode([]string{"A", "B", "C", "D", "E"})
	if want := []int{3, 4, 5}; !equalInts(ids, want) {
		t.Errorf("ids=%v, want %v (last 3 of A,B,C,D,E)", ids, want)
	}
}

func TestEncodeUnknownIsZero(t *testing.T) {
	tok := &Tokenizer{
		MaxLen:     4,
		VocabSize:  3,
		StringToID: map[string]int{"A": 1},
	}
	ids := tok.Encode([]string{"A", "ZZZ", "A"})
	if want := []int{0, 1, 0, 1}; !equalInts(ids, want) {
		t.Errorf("ids=%v, want %v", ids, want)
	}
}

func TestNewPredictorStubBackend(t *testing.T) {
	dir := t.TempDir()
	path := writeTokenizerFile(t, dir, sampleTokenizer())

	p, err := NewPredictor("", path) // onnxPath ignored by stub
	if err != nil {
		t.Fatalf("NewPredictor: %v", err)
	}
	if p == nil || p.backend == nil {
		t.Fatal("predictor or backend is nil")
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestPredictStubReturnsSortedTopK(t *testing.T) {
	dir := t.TempDir()
	path := writeTokenizerFile(t, dir, sampleTokenizer())
	p, err := NewPredictor("", path)
	if err != nil {
		t.Fatalf("NewPredictor: %v", err)
	}
	defer p.Close()

	preds, err := p.Predict([]string{"GET /products"}, 3)
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if len(preds) != 3 {
		t.Fatalf("len(preds)=%d, want 3", len(preds))
	}
	for i := 1; i < len(preds); i++ {
		if preds[i].Prob > preds[i-1].Prob {
			t.Errorf("preds not sorted by prob desc: %+v", preds)
		}
	}
	if preds[0].Call != "GET /products" {
		t.Errorf("top prediction = %q, want GET /products (last history call)", preds[0].Call)
	}
}

func TestPredictStubClampsTopK(t *testing.T) {
	dir := t.TempDir()
	path := writeTokenizerFile(t, dir, sampleTokenizer())
	p, _ := NewPredictor("", path)
	defer p.Close()

	preds, err := p.Predict(nil, 999)
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if len(preds) != 5 {
		t.Errorf("len(preds)=%d, want VocabSize=5", len(preds))
	}
}

func TestPredictStubEmptyHistoryUsesEnd(t *testing.T) {
	dir := t.TempDir()
	path := writeTokenizerFile(t, dir, sampleTokenizer())
	p, _ := NewPredictor("", path)
	defer p.Close()

	preds, err := p.Predict(nil, 3)
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if preds[0].Call != "END" {
		t.Errorf("top prediction with empty history = %q, want END", preds[0].Call)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := writeTokenizerFile(t, dir, sampleTokenizer())
	p, _ := NewPredictor("", path)
	if err := p.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("second Close: %v (want nil on stub)", err)
	}
}

func TestClosedPredictorPredictReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := writeTokenizerFile(t, dir, sampleTokenizer())
	p, _ := NewPredictor("", path)
	p.Close()
	if _, err := p.Predict([]string{"x"}, 1); err == nil {
		t.Error("expected error from Predict after Close")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// silence sort if unused in some builds
var _ = sort.Strings

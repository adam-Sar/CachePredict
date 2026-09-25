//go:build !cgo

package main

import (
"testing"
)

// TestNewPredictorStubBackend verifies NewPredictor works with the stub.
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

// TestPredictStubReturnsSortedTopK checks the stub returns sorted predictions.
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

// TestPredictStubClampsTopK verifies topK > VocabSize clamps to VocabSize.
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

// TestPredictStubEmptyHistoryUsesEnd: empty history falls back to END.
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

// TestCloseIsIdempotent: Close called twice does not error.
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

// TestClosedPredictorPredictReturnsError: Predict after Close returns an error.
func TestClosedPredictorPredictReturnsError(t *testing.T) {
dir := t.TempDir()
path := writeTokenizerFile(t, dir, sampleTokenizer())
p, _ := NewPredictor("", path)
p.Close()
if _, err := p.Predict([]string{"x"}, 1); err == nil {
t.Error("expected error from Predict after Close")
}
}
//go:build cgo

package main

import (
	"os"
	"testing"
)

func TestRealPredictorEndToEnd(t *testing.T) {
	onnxPath := "cache_predict_model.onnx"
	tokPath := "tokenizer.json"
	if _, err := os.Stat(onnxPath); err != nil {
		t.Skipf("model not present: %v", err)
	}
	if _, err := os.Stat(tokPath); err != nil {
		t.Skipf("tokenizer not present: %v", err)
	}

	p, err := NewPredictor(onnxPath, tokPath)
	if err != nil {
		t.Fatalf("NewPredictor: %v", err)
	}
	defer p.Close()
	t.Log("predictor loaded ok")

	history := []string{"GET /products", "GET /products/1", "GET /products/2"}
	for _, k := range []int{1, 3} {
		preds, err := p.Predict(history, k)
		if err != nil {
			t.Fatalf("Predict(k=%d): %v", k, err)
		}
		if len(preds) != k {
			t.Fatalf("top-%d: got %d preds", k, len(preds))
		}
		for i := 1; i < len(preds); i++ {
			if preds[i].Prob > preds[i-1].Prob {
				t.Errorf("top-%d not sorted: %+v", k, preds)
				break
			}
		}
		t.Logf("top-%d:", k)
		for _, p := range preds {
			t.Logf("  %s (%.4f)", p.Call, p.Prob)
		}
	}
}

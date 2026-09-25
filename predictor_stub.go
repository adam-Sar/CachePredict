//go:build !cgo

package main

import (
"fmt"
"sort"
)

// stubBackend is the non-CGO fallback. It returns deterministic predictions
// derived from the tokenizer vocab so the rest of the pipeline (cache keys,
// prefetch synthesis) works end-to-end without an actual ONNX model or
// C compiler installed. Predictions are not meaningful for real inference.
//
// Behaviour:
//   - top prediction is the last history call (if known and not "END"), or
//     "END" if the history is empty;
//   - remaining slots are filled from the rest of the vocab in stable order;
//   - probabilities decay as 1/(i+1) so the order is preserved.
type stubBackend struct {
tokenizer *Tokenizer
}

func newBackend(onnxPath string, tok *Tokenizer) (predictorBackend, error) {
if tok == nil {
return nil, fmt.Errorf("stub backend: nil tokenizer")
}
return &stubBackend{tokenizer: tok}, nil
}

func (b *stubBackend) Predict(history []string, topK int) ([]Prediction, error) {
if topK <= 0 || topK > b.tokenizer.VocabSize {
topK = b.tokenizer.VocabSize
}

// Pick the "headline" prediction: last history call, or "END" if empty.
head := "END"
if n := len(history); n > 0 {
head = history[n-1]
if _, ok := b.tokenizer.StringToID[head]; !ok {
head = "END"
}
}

preds := make([]Prediction, 0, topK)
if _, ok := b.tokenizer.StringToID[head]; ok {
preds = append(preds, Prediction{Call: head, Prob: 0.5})
}

// Stable vocab iteration order via sorted keys.
keys := make([]string, 0, len(b.tokenizer.IDToString))
for _, k := range b.tokenizer.IDToString {
keys = append(keys, k)
}
sort.Strings(keys)

for _, k := range keys {
if len(preds) >= topK {
break
}
if k == head {
continue
}
preds = append(preds, Prediction{Call: k, Prob: 0})
}

// Decay: first prediction gets 0.5, the rest split the remaining 0.5.
if len(preds) > 1 {
step := 0.5 / float32(len(preds)-1)
for i := 1; i < len(preds); i++ {
preds[i].Prob = 0.5 - step*float32(i-1)
}
}
return preds, nil
}

func (b *stubBackend) Close() error { return nil }
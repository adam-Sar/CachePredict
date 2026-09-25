//go:build cgo

package main

import (
"errors"
"fmt"
"sort"

ort "github.com/yalue/onnxruntime_go"
)

// onnxBackend runs inference against a real ONNX model via the
// onnxruntime_go library. Requires CGO and a working C toolchain.
type onnxBackend struct {
session    *ort.DynamicAdvancedSession
tokenizer  *Tokenizer
inputShape []int64
}

func newBackend(onnxPath string, tok *Tokenizer) (predictorBackend, error) {
sess, err := ort.NewDynamicAdvancedSession(
onnxPath,
[]string{"input_layer"},
[]string{"output_0"},
nil,
)
if err != nil {
return nil, fmt.Errorf("create onnx session: %w", err)
}
return &onnxBackend{
session:    sess,
tokenizer: tok,
inputShape: []int64{1, int64(tok.MaxLen)},
}, nil
}

func (b *onnxBackend) Predict(history []string, topK int) ([]Prediction, error) {
if b.session == nil {
return nil, errors.New("onnx backend: session not initialized")
}
if topK <= 0 || topK > b.tokenizer.VocabSize {
topK = b.tokenizer.VocabSize
}

ids := b.tokenizer.Encode(history)
flatData := make([]float32, len(ids))
for i, id := range ids {
flatData[i] = float32(id)
}

inputTensor, err := ort.NewTensor(b.inputShape, flatData)
if err != nil {
return nil, fmt.Errorf("create input tensor: %w", err)
}
defer inputTensor.Destroy()

outputs := []ort.Value{}
if err := b.session.Run([]ort.Value{inputTensor}, outputs); err != nil {
return nil, fmt.Errorf("run session: %w", err)
}
defer outputs[0].Destroy()

outputTensor, ok := outputs[0].(*ort.Tensor[float32])
if !ok {
return nil, fmt.Errorf("unexpected output tensor type %T", outputs[0])
}
probs := outputTensor.GetData()

idxs := make([]int, b.tokenizer.VocabSize)
for i := range idxs {
idxs[i] = i
}
sort.Slice(idxs, func(i, j int) bool {
return probs[idxs[i]] > probs[idxs[j]]
})

preds := make([]Prediction, topK)
for i := 0; i < topK; i++ {
preds[i] = Prediction{
Call: b.tokenizer.IDToString[idxs[i]],
Prob: probs[idxs[i]],
}
}
return preds, nil
}

func (b *onnxBackend) Close() error {
if b.session == nil {
return nil
}
return b.session.Destroy()
}
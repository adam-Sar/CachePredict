package main

import (
	"errors"
	"sort"
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

// LoadTokenizer reads tokenizer.json from disk.
func LoadTokenizer(path string) (*Tokenizer, error) {
	// TODO:
	//   1. os.ReadFile(path)
	//   2. json.Unmarshal into tokenizerFile
	//   3. wrap in *Tokenizer and return
	return nil, errors.New("not implemented")
}

// Encode converts a slice of API call strings into a padded int slice of
// length MaxLen, right-aligned (oldest at the end if truncated).
// Unknown strings become 0 (the END/PAD token's id).
func (t *Tokenizer) Encode(history []string) []int {
	// TODO:
	//   1. if len(history) > MaxLen, take the LAST MaxLen entries
	//   2. allocate [MaxLen]int filled with 0 (pad value)
	//   3. for i, call := range windowed history:
	//        ids[MaxLen - len(window) + i] = t.StringToID[call] (or 0 if missing)
	out := make([]int, t.MaxLen)
	_ = out
	return out
}

// Predictor wraps the loaded ONNX model and tokenizer.
// It is safe for concurrent use (onnxruntime_go sessions are thread-safe).
type Predictor struct {
	tokenizer  *Tokenizer
	inputName  string
	outputName string
	inputShape []int64
}

// NewPredictor loads the .onnx file and tokenizer from disk.
// Call this ONCE at startup — loading is expensive (~100-200ms).
func NewPredictor(onnxPath, tokenizerPath string) (*Predictor, error) {
	// TODO:
	//   1. tok, err := LoadTokenizer(tokenizerPath)
	//   2. sess, err := ort.NewDynamicAdvancedSession(
	//         onnxPath,
	//         []string{"input_layer"},
	//         []string{"output_0"},
	//      )
	//   3. return &Predictor{ session: sess, tokenizer: tok,
	//         inputName: "input_layer", outputName: "output_0",
	//         inputShape: []int64{1, int64(tok.MaxLen)} }
	return nil, errors.New("not implemented")
}

// Predict runs the model on the given history and returns top-K predictions
// sorted by probability (highest first).
//   history: past calls, oldest first (we keep only the last MaxLen)
//   topK:    number of predictions to return (typically 3)
func (p *Predictor) Predict(history []string, topK int) ([]Prediction, error) {
	// TODO:
	//   1. ids := p.tokenizer.Encode(history)
	//   2. convert []int -> []float32 (ONNX expects float32, not int32)
	//   3. wrap in [][]float32 of size [1][MaxLen]
	//   4. inputTensor, _ := ort.NewTensor(p.inputShape, flatData)
	//   5. outputs, _ := p.session.Run([]ort.Value{inputTensor})
	//   6. extract output[0] as []float32 of length VocabSize
	//   7. arg-sort topK indices, look each up in tokenizer.IDToString,
	//      build []Prediction sorted by Prob desc
	//
	//   Tip for step 7:
	//     idxs := make([]int, VocabSize); for i := range idxs { idxs[i] = i }
	//     sort.Slice(idxs, func(i, j int) bool { return probs[idxs[i]] > probs[idxs[j]] })
	//     top := idxs[:topK]
	_ = sort.Slice
	return nil, errors.New("not implemented")
}

// Close releases ONNX runtime resources. Call on shutdown.
func (p *Predictor) Close() error {
	// TODO: p.session.Destroy()
	return nil
}
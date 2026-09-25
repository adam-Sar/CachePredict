//go:build cgo && windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/sys/windows"
)

// preferredDLLPath, when set, is added to the Windows DLL search order before
// InitializeEnvironment() runs. The System32 onnxruntime.dll is older than
// what onnxruntime_go v1.36.0 expects (API 29), so we have to point at a
// newer copy ourselves. Resolution order:
//  1. ONNXRUNTIME_DLL env var (full path to the dll).
//  2. ./onnxruntime.dll next to the working directory.
var preferredDLLPath string

func init() {
	if v := os.Getenv("ONNXRUNTIME_DLL"); v != "" {
		preferredDLLPath = v
	} else if wd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(wd, "onnxruntime.dll")
		if _, err := os.Stat(candidate); err == nil {
			preferredDLLPath = candidate
		}
	}
	if preferredDLLPath == "" {
		fmt.Fprintln(os.Stderr, "warning: onnxruntime.dll not found; set ONNXRUNTIME_DLL or place onnxruntime.dll next to the working directory. Falling back to system DLL search order.")
		return
	}
	// SetDllDirectory adds this dir to the DLL search path *before* System32,
	// so the newer onnxruntime.dll wins over the older one in System32.
	if err := windows.SetDllDirectory(filepath.Dir(preferredDLLPath)); err != nil {
		fmt.Fprintf(os.Stderr, "warning: SetDllDirectory(%q): %v\n", filepath.Dir(preferredDLLPath), err)
	}
}

type onnxBackend struct {
	session    *ort.DynamicAdvancedSession
	tokenizer  *Tokenizer
	inputShape []int64
}

var initOnce sync.Once

func newBackend(onnxPath string, tok *Tokenizer) (predictorBackend, error) {
	var initErr error
	initOnce.Do(func() {
		initErr = ort.InitializeEnvironment()
	})
	if initErr != nil {
		return nil, fmt.Errorf("initialize onnx runtime: %w", initErr)
	}

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
		tokenizer:  tok,
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

	// Pre-allocate the output tensor so the library has somewhere to write.
	// The shape must match the model's output shape ([1, VocabSize] for a
	// per-token classifier). We let NewTensor allocate the buffer.
	outShape := []int64{1, int64(b.tokenizer.VocabSize)}
	outputTensor, err := ort.NewTensor[float32](outShape, make([]float32, b.tokenizer.VocabSize))
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	outputs := []ort.Value{outputTensor}
	if err := b.session.Run([]ort.Value{inputTensor}, outputs); err != nil {
		return nil, fmt.Errorf("run session: %w", err)
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

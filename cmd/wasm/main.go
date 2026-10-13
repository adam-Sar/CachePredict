//go:build js && wasm

package main

import (
	"encoding/json"
	"strings"
	"syscall/js"

	"github.com/adam-Sar/precog"
)

var (
	predictor = precog.NewDenseMatrixPredictor(&precog.DensePredictorConfig{
		MaxRoutes: 512,
	})
	sessionID = "browser_client"
)

func record(this js.Value, args []js.Value) any {
	if len(args) < 2 {
		return nil
	}
	method := args[0].String()
	fullPath := args[1].String()

	path := fullPath
	rawQuery := ""
	if idx := strings.Index(fullPath, "?"); idx != -1 {
		path = fullPath[:idx]
		rawQuery = fullPath[idx+1:]
	}

	predictor.Record(sessionID, method, path, rawQuery)
	return nil
}

func predict(this js.Value, args []js.Value) any {
	topK := 3
	if len(args) > 0 && args[0].Type() == js.TypeNumber {
		topK = args[0].Int()
	}

	preds := predictor.Predict(sessionID, topK)
	out, err := json.Marshal(preds)
	if err != nil {
		return "[]"
	}
	return string(out)
}

func speculationRules(this js.Value, args []js.Value) any {
	topK := 3
	if len(args) > 0 && args[0].Type() == js.TypeNumber {
		topK = args[0].Int()
	}

	preds := predictor.Predict(sessionID, topK)
	urls := make([]string, 0, len(preds))
	for _, p := range preds {
		parts := strings.SplitN(p.Call, " ", 2)
		if len(parts) == 2 {
			urls = append(urls, parts[1])
		} else {
			urls = append(urls, p.Call)
		}
	}

	rules := map[string]any{
		"prefetch": []map[string]any{
			{
				"source":    "list",
				"urls":      urls,
				"eagerness": "moderate",
			},
		},
	}

	out, err := json.Marshal(rules)
	if err != nil {
		return "{}"
	}
	return string(out)
}

func main() {
	precogObj := js.Global().Get("Object").New()
	precogObj.Set("record", js.FuncOf(record))
	precogObj.Set("predict", js.FuncOf(predict))
	precogObj.Set("getSpeculationRules", js.FuncOf(speculationRules))

	js.Global().Set("PrecogWasm", precogObj)

	// Keep alive
	select {}
}

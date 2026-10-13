#!/usr/bin/env bash
set -euo pipefail

echo "Building Precog WebAssembly module..."
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o precog.wasm ./cmd/wasm
echo "WebAssembly build complete: precog.wasm"

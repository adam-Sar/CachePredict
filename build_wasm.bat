@echo off
echo Building Precog WebAssembly module...
set GOOS=js
set GOARCH=wasm
go build -ldflags="-s -w" -o precog.wasm ./cmd/wasm
echo WebAssembly build complete: precog.wasm

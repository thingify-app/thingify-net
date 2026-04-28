#!/bin/bash

cd golang
GOOS=js GOARCH=wasm go build -o ../dist/main.wasm
cp /usr/local/go/lib/wasm/wasm_exec.js ../dist

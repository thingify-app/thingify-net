#!/bin/bash

cd golang

tinygo build -target=wasm -o ../dist/main.wasm
cp "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" ../dist

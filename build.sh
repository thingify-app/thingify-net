#!/bin/bash
set -e

mkdir -p packaging/build
GOOS=linux GOARCH=arm64 go build -buildvcs=false -o packaging/build/thingify-net
cd packaging
nfpm package -p deb -f nfpm.yaml -t build

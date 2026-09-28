#!/bin/sh
# Run the Go toolchain in a container (the host has no Go install).
# Usage: ./go.sh test ./...   |   ./go.sh mod tidy
cd "$(dirname "$0")" && exec docker run --rm -u "$(id -u):$(id -g)" \
  -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOFLAGS=-buildvcs=false \
  -v onion-gocache:/cache -v "$PWD":/src -w /src golang:1.26-alpine go "$@"

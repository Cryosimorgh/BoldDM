#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p dist
go test ./...
go vet ./...
go build -trimpath -ldflags='-s -w' -o dist/boltdm ./cmd/boltdm
echo "Built dist/boltdm"

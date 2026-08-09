#!/bin/bash
# Compila Bilingua. Sin CGo → cross-compila a Windows desde cualquier sistema.
set -e
cd "$(dirname "$0")"
mkdir -p dist
echo "→ Windows (.exe)…"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o dist/Bilingua.exe .
echo "→ macOS…"
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o dist/Bilingua-mac .
echo "→ Linux…"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o dist/Bilingua-linux .
echo "✓ Listo en dist/  ($(ls -1 dist | tr '\n' ' '))"

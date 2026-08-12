#!/bin/bash
# Compila Bilingua. Sin CGo → cross-compila a Windows desde cualquier sistema.
set -e
cd "$(dirname "$0")"
mkdir -p dist

echo "→ Windows (doble clic, sin ventana de consola)…"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -H windowsgui" -o dist/Bilingua.exe .
# Un binario -H windowsgui NO recibe consola: invocado desde cmd/PowerShell no
# imprime nada y el shell no lo espera. Para la terminal va un .exe aparte.
echo "→ Windows (terminal: bilingua-cli.exe)…"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o dist/bilingua-cli.exe .

echo "→ macOS (universal: Intel + Apple Silicon)…"
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o dist/.mac-amd64 .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o dist/.mac-arm64 .
if LIPO=$(xcrun --find lipo 2>/dev/null); then
  "$LIPO" -create -output dist/Bilingua-mac dist/.mac-amd64 dist/.mac-arm64
  rm -f dist/.mac-amd64 dist/.mac-arm64
else
  # Sin herramientas de Apple (compilando en Linux/Windows): dos binarios sueltos.
  mv dist/.mac-amd64 dist/Bilingua-mac-intel
  mv dist/.mac-arm64 dist/Bilingua-mac-applesilicon
fi

echo "→ Linux…"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o dist/Bilingua-linux .

echo "✓ Listo en dist/  ($(ls -1 dist | tr '\n' ' '))"

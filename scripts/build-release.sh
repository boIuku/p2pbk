#!/usr/bin/env sh
set -eu

VERSION="${VERSION:-dev}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-X p2pbk/internal/version.Version=${VERSION} -X p2pbk/internal/version.Commit=${COMMIT} -X p2pbk/internal/version.Date=${DATE}"

mkdir -p dist
EXE=""
if [ "$(go env GOOS)" = "windows" ]; then
  EXE=".exe"
fi
go build -ldflags "$LDFLAGS" -o "dist/p2pbk${EXE}" ./cmd/p2pbk

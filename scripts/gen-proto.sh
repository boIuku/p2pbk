#!/usr/bin/env sh
set -eu

if command -v protoc >/dev/null 2>&1 && command -v protoc-gen-go >/dev/null 2>&1; then
  protoc --go_out=. --go_opt=paths=source_relative api/wire.proto
else
  echo "protoc and protoc-gen-go are required to regenerate protobuf code" >&2
  exit 1
fi

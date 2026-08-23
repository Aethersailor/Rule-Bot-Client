#!/bin/sh
set -eu

: "${TARGET_ARCH:?TARGET_ARCH is required}"
: "${OUTPUT:?OUTPUT is required}"

TARGET_OS=${TARGET_OS:-linux}
TARGET_LABEL=${TARGET_LABEL:-${TARGET_OS}-${TARGET_ARCH}}
VERSION=${VERSION:-dev}
COMMIT=${COMMIT:-$(git rev-parse --short=12 HEAD)}
BUILD_DATE=${BUILD_DATE:-$(git show -s --format=%cI HEAD)}

export CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH"
if [ -n "${TARGET_ARM:-}" ]; then export GOARM="$TARGET_ARM"; fi
if [ -n "${TARGET_MIPS:-}" ]; then export GOMIPS="$TARGET_MIPS"; fi
if [ -n "${TARGET_AMD64:-}" ]; then export GOAMD64="$TARGET_AMD64"; fi

mkdir -p "$(dirname "$OUTPUT")"
go build -buildvcs=false -trimpath \
  -ldflags="-s -w -buildid= \
    -X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildVersion=$VERSION \
    -X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildCommit=$COMMIT \
    -X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildDate=$BUILD_DATE \
    -X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildTarget=$TARGET_LABEL" \
  -o "$OUTPUT" ./cmd/rule-bot-client

size=$(wc -c < "$OUTPUT")
if [ "$size" -gt 8388608 ]; then
  echo "binary exceeds 8 MiB gate: $OUTPUT ($size bytes)" >&2
  exit 1
fi

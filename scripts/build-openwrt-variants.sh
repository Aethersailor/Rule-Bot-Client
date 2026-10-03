#!/usr/bin/env bash
set -euo pipefail

SOURCE="$(realpath "${1:?source checkout is required}")"
OUTPUT="$(realpath -m "${2:?output directory is required}")"
SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
: "${COMMIT:?full source commit is required}"
: "${VERSION:?package version is required}"
BUILD_DATE="${BUILD_DATE:-1970-01-01T00:00:00Z}"
export COMMIT VERSION BUILD_DATE

mkdir -p "$OUTPUT"
python3 "$SCRIPT_DIR/openwrt-targets.py" variants > "$OUTPUT/variants.tsv"
while IFS=$'\t' read -r variant arch arm go386 gomips gomips64; do
  mkdir -p "$OUTPUT/$variant"
  (
    cd "$SOURCE"
    export TARGET_ARCH="$arch" TARGET_ARM="${arm#-}" GO386="$go386" \
      TARGET_MIPS="$gomips" GOMIPS64="$gomips64" GOAMD64=v1
    OUTPUT="$OUTPUT/$variant/rule-bot-client" sh scripts/build-one.sh
    OUTPUT="$OUTPUT/$variant/rule-bot-client-openwrt" sh scripts/build-openwrt-helper.sh
  )
  for binary in rule-bot-client rule-bot-client-openwrt; do
    python3 "$SCRIPT_DIR/openwrt-targets.py" check-binary "$OUTPUT/$variant/$binary" "$variant" "$COMMIT" "$VERSION"
  done
done < "$OUTPUT/variants.tsv"

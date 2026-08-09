#!/bin/sh
set -eu

if [ ! -f go.mod ] || [ ! -d scripts ] || [ ! -d deploy ]; then
  echo "build-release.sh must run from the repository root" >&2
  exit 2
fi

VERSION=${VERSION:-$(git describe --tags --exact-match)}
COMMIT=${COMMIT:-$(git rev-parse HEAD)}
BUILD_DATE=${BUILD_DATE:-$(git show -s --format=%cI HEAD)}
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct HEAD)}
export VERSION COMMIT BUILD_DATE SOURCE_DATE_EPOCH

case "$VERSION" in
  ''|*[!0-9A-Za-z._-]*)
    echo "VERSION contains unsupported characters: $VERSION" >&2
    exit 2
    ;;
esac

rm -rf dist
mkdir -p dist

build_archive() {
  label=$1
  arch=$2
  arm=${3:-}
  mips=${4:-}
  package="rule-bot-client_${VERSION}_linux_${label}"
  root="dist/$package"
  mkdir -p "$root"
  TARGET_ARCH="$arch" TARGET_ARM="$arm" TARGET_MIPS="$mips" OUTPUT="$root/rule-bot-client" \
    sh scripts/build-one.sh
  cp LICENSE README.md PRIVACY.md SECURITY.md config.example.json "$root/"
  chmod 0755 "$root/rule-bot-client"
  chmod 0600 "$root/config.example.json"
  tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
    -C dist -czf "dist/$package.tar.gz" "$package"
  rm -rf "$root"
}

build_archive amd64 amd64
build_archive 386 386
build_archive arm64 arm64
build_archive armv7 arm 7
build_archive armv6 arm 6
build_archive armv5 arm 5
build_archive mips-softfloat mips '' softfloat
build_archive mipsle-softfloat mipsle '' softfloat
build_archive mips-hardfloat mips '' hardfloat
build_archive mipsle-hardfloat mipsle '' hardfloat
build_archive mips64 mips64
build_archive mips64le mips64le
build_archive riscv64 riscv64

for spec in amd64:amd64 arm64:arm64 armhf:armv7; do
  deb_arch=${spec%%:*}
  binary_label=${spec#*:}
  archive="dist/rule-bot-client_${VERSION}_linux_${binary_label}.tar.gz"
  work=$(mktemp -d)
  tar -xzf "$archive" -C "$work"
  binary=$(find "$work" -type f -name rule-bot-client -print -quit)
  sh scripts/package-deb.sh "$deb_arch" "$binary" "$VERSION" "dist"
  rm -rf "$work"
done

(cd dist && sha256sum ./* > checksums.txt)

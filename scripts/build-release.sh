#!/bin/sh
set -eu

if [ ! -f go.mod ] || [ ! -d scripts ] || [ ! -d deploy ]; then
  echo "build-release.sh must run from the repository root" >&2
  exit 2
fi
command -v jq >/dev/null 2>&1 || { echo 'jq is required to build release metadata' >&2; exit 2; }
command -v zip >/dev/null 2>&1 || { echo 'zip is required to build Windows portable archives' >&2; exit 2; }

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
  TARGET_ARCH="$arch" TARGET_ARM="$arm" TARGET_MIPS="$mips" TARGET_LABEL="linux-$label" OUTPUT="$root/rule-bot-client" \
    sh scripts/build-one.sh
  cp LICENSE README.md PRIVACY.md SECURITY.md "$root/"
  cp deploy/linux/config.json "$root/config.example.json"
  mkdir -p "$root/systemd"
  cp deploy/systemd/rule-bot-client.service deploy/systemd/rule-bot-client-update.service deploy/systemd/rule-bot-client-update.timer "$root/systemd/"
  chmod 0755 "$root/rule-bot-client"
  chmod 0600 "$root/config.example.json"
  tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
    -C dist -czf "dist/$package.tar.gz" "$package"
  rm -rf "$root"
}

build_windows_archive() {
  label=$1
  arch=$2
  package="rule-bot-client_${VERSION}_windows_${label}"
  root="dist/$package"
  mkdir -p "$root"
  TARGET_OS=windows TARGET_ARCH="$arch" TARGET_LABEL="windows-$label" OUTPUT="$root/rule-bot-client.exe" \
    sh scripts/build-one.sh
  cp LICENSE README.md PRIVACY.md SECURITY.md "$root/"
  cp deploy/windows/config.json "$root/config.example.json"
  chmod 0600 "$root/config.example.json"
  find "$root" -exec touch -h -d "@$SOURCE_DATE_EPOCH" {} +
  (cd dist && find "$package" -type f -print | LC_ALL=C sort | zip -X -q "$package.zip" -@)
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

build_windows_archive amd64 amd64
build_windows_archive arm64 arm64

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

entries=$(mktemp)
trap 'rm -f "$entries"' EXIT
add_update_asset() {
  target=$1
  kind=$2
  file=$3
  sha256=$(sha256sum "dist/$file" | awk '{print $1}')
  size=$(wc -c < "dist/$file" | tr -d ' ')
  printf '%s\t%s\t%s\t%s\t%s\n' "$target" "$kind" "$file" "$sha256" "$size" >> "$entries"
}
for label in amd64 386 arm64 armv7 armv6 armv5 mips-softfloat mipsle-softfloat mips-hardfloat mipsle-hardfloat mips64 mips64le riscv64; do
  add_update_asset "linux-$label" archive "rule-bot-client_${VERSION}_linux_${label}.tar.gz"
done
for spec in amd64:amd64 arm64:arm64 armhf:armv7; do
  add_update_asset "linux-${spec#*:}" deb "rule-bot-client_${VERSION#v}_${spec%%:*}.deb"
done
for label in amd64 arm64; do
  add_update_asset "windows-$label" archive "rule-bot-client_${VERSION}_windows_${label}.zip"
done
jq -Rn --arg version "$VERSION" --arg commit "$COMMIT" '
  [inputs | split("\t") | {target:.[0],kind:.[1],name:.[2],sha256:.[3],size:(.[4]|tonumber)}] as $assets |
  {schema:1,version:$version,commit:$commit,assets:$assets}
' < "$entries" > dist/client-update-manifest.json

(cd dist && sha256sum ./* > checksums.txt)

#!/usr/bin/env bash
set -euo pipefail

MANAGER="${1:?package manager is required}"
SOURCE="$(realpath "${2:?source checkout is required}")"
BINARIES="$(realpath "${3:?compiled variants are required}")"
OUTPUT="$(realpath -m "${4:?artifact output directory is required}")"
SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
: "${COMMIT:?full source commit is required}"
: "${VERSION:?package version is required}"
case "$MANAGER" in ipk|apk) ;; *) exit 2 ;; esac
mkdir -p "$OUTPUT"
WORK="$(mktemp -d "${RUNNER_TEMP:-/var/tmp}/codex-openwrt-batch-XXXXXX")"
cleanup() {
  local target
  target="$(realpath "$WORK")"
  if [ ! -L "$WORK" ] && [[ "$target" == "${RUNNER_TEMP:-/var/tmp}"/codex-openwrt-batch-* ]]; then
    rm -rf -- "$target"
  fi
}
trap cleanup EXIT

base="$(python3 "$SCRIPT_DIR/openwrt-targets.py" sdk "$MANAGER")"
curl --fail --location --retry 2 --max-time 60 "$base/sha256sums" -o "$WORK/sha256sums"
sdk="$(awk '{name=$2; sub(/^\*/, "", name); if (name ~ /^openwrt-sdk-.*\.Linux-x86_64\.tar\.zst$/) {print name; exit}}' "$WORK/sha256sums")"
test -n "$sdk"
sdk_sha256="$(awk -v name="$sdk" '{file=$2; sub(/^\*/, "", file); if (file == name) {print $1; exit}}' "$WORK/sha256sums")"
curl --fail --location --retry 2 --max-time 300 "$base/$sdk" -o "$WORK/sdk.tar.zst"
printf '%s  %s\n' "$sdk_sha256" "$WORK/sdk.tar.zst" | sha256sum -c -
tar --zstd -xf "$WORK/sdk.tar.zst" -C "$WORK"
SDK_DIR="$(find "$WORK" -maxdepth 1 -type d -name 'openwrt-sdk-*' -print -quit)"
test -n "$SDK_DIR"
(
  cd "$SDK_DIR"
  ./scripts/feeds update luci
  make -C feeds/luci/modules/luci-base/src po2lmo CC=cc
  install -D -m 0755 feeds/luci/modules/luci-base/src/po2lmo staging_dir/hostpkg/bin/po2lmo
)
cp -a "$SOURCE/openwrt/package/luci-app-rule-bot-client" "$SDK_DIR/package/luci-app-rule-bot-client"
cp "$SCRIPT_DIR/../openwrt/package/luci-app-rule-bot-client/Makefile" "$SDK_DIR/package/luci-app-rule-bot-client/Makefile"
mkdir -p "$SDK_DIR/package/luci-app-rule-bot-client/src"
printf '%s\n' 'CONFIG_PACKAGE_luci-app-rule-bot-client=y' >> "$SDK_DIR/.config"
make -C "$SDK_DIR" defconfig RULE_BOT_CLIENT_VERSION="$VERSION"
test -x "$SDK_DIR/staging_dir/hostpkg/bin/po2lmo"
if [ "$MANAGER" = apk ]; then
  APK_TOOL="$(find "$SDK_DIR/staging_dir" -type f -path '*/bin/apk' -perm -u+x -print -quit)"
  test -n "$APK_TOOL"
  export APK_TOOL
fi

python3 "$SCRIPT_DIR/openwrt-targets.py" packages "$MANAGER" > "$WORK/packages.tsv"
while IFS=$'\t' read -r architecture variant; do
  directory="$OUTPUT/$MANAGER-$architecture"
  mkdir -p "$directory"
  for binary in rule-bot-client rule-bot-client-openwrt; do
    python3 "$SCRIPT_DIR/openwrt-targets.py" check-binary "$BINARIES/$variant/$binary" "$variant" "$COMMIT" "$VERSION"
    install -m 0755 "$BINARIES/$variant/$binary" "$SDK_DIR/package/luci-app-rule-bot-client/src/$binary"
  done
  # Go already strips these static binaries. Preserve their verified ABI and
  # build metadata; the SDK's target-specific strip tool cannot handle all ABIs.
  make -C "$SDK_DIR" package/luci-app-rule-bot-client/clean \
    RULE_BOT_CLIENT_VERSION="$VERSION" RULE_BOT_CLIENT_PACKAGE_ARCH="$architecture"
  make -C "$SDK_DIR" package/luci-app-rule-bot-client/compile V=s \
    RULE_BOT_CLIENT_VERSION="$VERSION" RULE_BOT_CLIENT_PACKAGE_ARCH="$architecture"
  package="$(find "$SDK_DIR/bin" -type f -name "luci-app-rule-bot-client*.$MANAGER" -print -quit)"
  test -n "$package"
  EXPECTED_VARIANT="$variant" COMMIT="$COMMIT" VERSION="$VERSION" \
    sh "$SCRIPT_DIR/assert-openwrt-package.sh" "$MANAGER" "$package" "$architecture"
  cp "$package" "$directory/"
  package_name="$(basename "$package")"
  package_sha256="$(sha256sum "$package" | awk '{print $1}')"
  package_size="$(wc -c < "$package")"
  jq -n --arg head_sha "$COMMIT" --arg workflow_run "${GITHUB_RUN_ID:-local}" \
    --arg manager "$MANAGER" --arg package_arch "$architecture" --arg variant "$variant" \
    --arg package "$package_name" --arg sha256 "$package_sha256" --argjson size "$package_size" \
    --arg sdk_url "$base/$sdk" --arg sdk_sha256 "$sdk_sha256" \
    '{head_sha:$head_sha,workflow_run:$workflow_run,manager:$manager,package_arch:$package_arch,variant:$variant,package:$package,sha256:$sha256,size:$size,sdk_url:$sdk_url,sdk_sha256:$sdk_sha256}' \
    > "$directory/manifest.json"
done < "$WORK/packages.tsv"

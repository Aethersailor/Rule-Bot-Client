#!/bin/sh
set -eu

work=$(mktemp -d)
cleanup() { rm -rf "$work"; }
trap cleanup EXIT HUP INT TERM

artifacts="$work/artifacts"
output="$work/release"
commit=0123456789abcdef0123456789abcdef01234567
mkdir -p "$artifacts"

for manager in ipk apk; do
	for architecture in x86_64 aarch64_generic mips_24kc mipsel_24kc; do
		directory="$artifacts/luci-app-rule-bot-client-${manager}-${architecture}"
		mkdir -p "$directory"
		if [ "$manager" = ipk ]; then
			package="luci-app-rule-bot-client_0.2.0-r1_${architecture}.ipk"
			release=24.10.4
		else
			package=luci-app-rule-bot-client-0.2.0-r1.apk
			release=25.12.0
		fi
		printf '%s\n' "$manager-$architecture" > "$directory/$package"
		sha256=$(sha256sum "$directory/$package" | awk '{ print $1 }')
		size=$(wc -c < "$directory/$package" | tr -d ' ')
		jq -n \
			--arg head_sha "$commit" \
			--arg manager "$manager" \
			--arg package_arch "$architecture" \
			--arg package "$package" \
			--arg sha256 "$sha256" \
			--argjson size "$size" \
			--arg sdk_url "https://downloads.openwrt.org/releases/${release}/targets/test/generic/sdk.tar.zst" \
			'{head_sha:$head_sha,manager:$manager,package_arch:$package_arch,package:$package,sha256:$sha256,size:$size,sdk_url:$sdk_url}' \
			> "$directory/manifest.json"
	done
done

GITHUB_REPOSITORY=Aethersailor/Rule-Bot-Client \
	sh scripts/prepare-openwrt-release.sh v0.2.0 "$commit" "$artifacts" "$output"

test "$(find "$output" -maxdepth 1 -type f | wc -l)" -eq 11
test "$(find "$output" -maxdepth 1 -type f -name '*.ipk' | wc -l)" -eq 4
test "$(find "$output" -maxdepth 1 -type f -name '*.apk' | wc -l)" -eq 4
test "$(cut -f1,2 "$output/openwrt-manifest.tsv" | tail -n +2 | sort -u | wc -l)" -eq 8
if grep -F '@VERSION@' "$output/install-rule-bot-client-openwrt.sh"; then
	echo 'generated installer still contains the version placeholder' >&2
	exit 1
fi
grep -F "version='0.2.0'" "$output/install-rule-bot-client-openwrt.sh"

unsafe_artifacts="$work/unsafe-artifacts"
cp -R "$artifacts" "$unsafe_artifacts"
unsafe_manifest=$(find "$unsafe_artifacts" -type f -name manifest.json | head -n 1)
jq '.package = "../escaped.ipk"' "$unsafe_manifest" > "$work/unsafe-manifest.json"
mv "$work/unsafe-manifest.json" "$unsafe_manifest"
if GITHUB_REPOSITORY=Aethersailor/Rule-Bot-Client \
	sh scripts/prepare-openwrt-release.sh v0.2.0 "$commit" "$unsafe_artifacts" "$work/unsafe-output" \
	>"$work/unsafe.out" 2>"$work/unsafe.err"; then
	echo 'release preparation accepted an unsafe package path' >&2
	exit 1
fi
grep -F 'unsafe package filename' "$work/unsafe.err"
test ! -e "$work/escaped.ipk"

mock_bin="$work/mock-apk"
mkdir -p "$mock_bin"
# The single-quoted strings intentionally become a separate mock shell script.
# shellcheck disable=SC2016
printf '%s\n' \
	'#!/bin/sh' \
	'set -eu' \
	'test "$1" = -O' \
	'destination=$2' \
	'url=$3' \
	'cp "$FIXTURE_RELEASE/${url##*/}" "$destination"' \
	> "$mock_bin/uclient-fetch"
# shellcheck disable=SC2016
printf '%s\n' \
	'#!/bin/sh' \
	'set -eu' \
	'if [ "$1" = --print-arch ]; then echo x86_64; exit 0; fi' \
	'test "$1:$2" = add:--allow-untrusted' \
	'printf "%s\n" "$3" > "$INSTALL_MARKER"' \
	> "$mock_bin/apk"
chmod 0755 "$mock_bin/uclient-fetch" "$mock_bin/apk"

PATH="$mock_bin:$PATH" FIXTURE_RELEASE="$output" INSTALL_MARKER="$work/apk-installed" \
	sh "$output/install-rule-bot-client-openwrt.sh"
test -s "$work/apk-installed"

immortalwrt_snapshot="$work/immortalwrt-snapshot-release"
printf '%s\n' \
	"DISTRIB_ID='ImmortalWrt'" \
	"DISTRIB_RELEASE='SNAPSHOT'" \
	> "$immortalwrt_snapshot"
rm -f "$work/apk-installed"
PATH="$mock_bin:$PATH" FIXTURE_RELEASE="$output" INSTALL_MARKER="$work/apk-installed" \
	RULE_BOT_CLIENT_TEST_RELEASE_FILE="$immortalwrt_snapshot" \
	sh "$output/install-rule-bot-client-openwrt.sh" \
	>"$work/immortalwrt-snapshot.out" 2>"$work/immortalwrt-snapshot.err"
test -s "$work/apk-installed"
grep -F 'ImmortalWrt SNAPSHOT detected; installing the hash-verified OpenWrt 25.12.0 APK compatibility build.' \
	"$work/immortalwrt-snapshot.err"

openwrt_snapshot="$work/openwrt-snapshot-release"
printf '%s\n' \
	"DISTRIB_ID='OpenWrt'" \
	"DISTRIB_RELEASE='SNAPSHOT'" \
	> "$openwrt_snapshot"
rm -f "$work/apk-installed"
if PATH="$mock_bin:$PATH" FIXTURE_RELEASE="$output" INSTALL_MARKER="$work/apk-installed" \
	RULE_BOT_CLIENT_TEST_RELEASE_FILE="$openwrt_snapshot" \
	sh "$output/install-rule-bot-client-openwrt.sh" \
	>"$work/openwrt-snapshot.out" 2>"$work/openwrt-snapshot.err"; then
	echo 'installer accepted an unsupported OpenWrt SNAPSHOT package' >&2
	exit 1
fi
grep -F 'built for OpenWrt 25.12.0, but OpenWrt reports SNAPSHOT' "$work/openwrt-snapshot.err"
test ! -e "$work/apk-installed"

mock_bin="$work/mock-opkg"
mkdir -p "$mock_bin"
cp "$work/mock-apk/uclient-fetch" "$mock_bin/uclient-fetch"
# shellcheck disable=SC2016
printf '%s\n' \
	'#!/bin/sh' \
	'set -eu' \
	'if [ "$1" = print-architecture ]; then' \
	'  printf "%s\n" "arch all 1" "arch mipsel_24kc 10"' \
	'  exit 0' \
	'fi' \
	'test "$1" = install' \
	'printf "%s\n" "$2" > "$INSTALL_MARKER"' \
	> "$mock_bin/opkg"
chmod 0755 "$mock_bin/uclient-fetch" "$mock_bin/opkg"

PATH="$mock_bin:/usr/bin:/bin" FIXTURE_RELEASE="$output" INSTALL_MARKER="$work/opkg-installed" \
	sh "$output/install-rule-bot-client-openwrt.sh"
test -s "$work/opkg-installed"

if PATH="$mock_bin:/usr/bin:/bin" sh scripts/install-openwrt.sh >"$work/template.out" 2>"$work/template.err"; then
	echo 'source installer template unexpectedly ran' >&2
	exit 1
fi
grep -F 'source template is not installable' "$work/template.err"

bad_release="$work/bad-release"
cp -R "$output" "$bad_release"
bad_asset=$(awk -F '\t' '$1 == "ipk" && $2 == "mipsel_24kc" { print $3 }' "$bad_release/openwrt-manifest.tsv")
printf '%s\n' 'tampered package' >> "$bad_release/$bad_asset"
rm -f "$work/hash-install-called"
if PATH="$mock_bin:/usr/bin:/bin" FIXTURE_RELEASE="$bad_release" INSTALL_MARKER="$work/hash-install-called" \
	sh "$bad_release/install-rule-bot-client-openwrt.sh" >"$work/hash.out" 2>"$work/hash.err"; then
	echo 'installer accepted a tampered package' >&2
	exit 1
fi
grep -E 'Package (size|SHA256) mismatch' "$work/hash.err"
test ! -e "$work/hash-install-called"

mock_bin="$work/mock-unsupported"
mkdir -p "$mock_bin"
cp "$work/mock-apk/uclient-fetch" "$mock_bin/uclient-fetch"
# shellcheck disable=SC2016
printf '%s\n' \
	'#!/bin/sh' \
	'set -eu' \
	'if [ "$1" = --print-arch ]; then echo riscv64; exit 0; fi' \
	'exit 99' \
	> "$mock_bin/apk"
chmod 0755 "$mock_bin/uclient-fetch" "$mock_bin/apk"
rm -f "$work/unsupported-install-called"
if PATH="$mock_bin:/usr/bin:/bin" FIXTURE_RELEASE="$output" INSTALL_MARKER="$work/unsupported-install-called" \
	sh "$output/install-rule-bot-client-openwrt.sh" >"$work/unsupported.out" 2>"$work/unsupported.err"; then
	echo 'installer accepted an unsupported architecture' >&2
	exit 1
fi
grep -F 'no unique package for manager=apk architecture=riscv64' "$work/unsupported.err"
test ! -e "$work/unsupported-install-called"

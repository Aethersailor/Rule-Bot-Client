#!/bin/sh
set -eu

if [ "$#" -ne 4 ]; then
	echo 'usage: prepare-openwrt-release.sh <vX.Y.Z> <commit> <artifact-root> <output-dir>' >&2
	exit 2
fi

tag=$1
commit=$2
artifact_root=$3
output=$4
repository=${GITHUB_REPOSITORY:-Aethersailor/Rule-Bot-Client}

printf '%s' "$tag" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$' || {
	echo "invalid release tag: $tag" >&2
	exit 2
}
printf '%s' "$commit" | grep -Eq '^[0-9a-f]{40}$' || { echo 'invalid release commit' >&2; exit 2; }
printf '%s' "$repository" | grep -Eq '^[0-9A-Za-z_.-]+/[0-9A-Za-z_.-]+$' || { echo 'invalid GitHub repository' >&2; exit 2; }
[ -d "$artifact_root" ] || { echo "artifact root is missing: $artifact_root" >&2; exit 2; }
[ ! -e "$output" ] || { echo "output already exists: $output" >&2; exit 2; }

mkdir -p "$output"
entries="$output/.manifest.entries"
: > "$entries"

find "$artifact_root" -type f -name manifest.json -print | sort | while IFS= read -r manifest; do
	manager=$(jq -r '.manager' "$manifest")
	architecture=$(jq -r '.package_arch' "$manifest")
	package_name=$(jq -r '.package' "$manifest")
	package_sha256=$(jq -r '.sha256' "$manifest")
	package_size=$(jq -r '.size' "$manifest")
	head_sha=$(jq -r '.head_sha' "$manifest")
	sdk_url=$(jq -r '.sdk_url' "$manifest")

	[ "$head_sha" = "$commit" ] || { echo "artifact commit mismatch in $manifest" >&2; exit 1; }
	case "$manager:$architecture" in
		ipk:x86_64|ipk:aarch64_generic|ipk:mips_24kc|ipk:mipsel_24kc|apk:x86_64|apk:aarch64_generic|apk:mips_24kc|apk:mipsel_24kc) ;;
		*) echo "unexpected package identity $manager:$architecture" >&2; exit 1 ;;
	esac
	printf '%s' "$package_name" | grep -Eq '^luci-app-rule-bot-client[-_+.0-9A-Za-z]+\.(ipk|apk)$' || {
		echo "unsafe package filename in $manifest" >&2
		exit 1
	}
	case "$manager:$package_name" in
		ipk:*.ipk|apk:*.apk) ;;
		*) echo "package format mismatch in $manifest" >&2; exit 1 ;;
	esac
	printf '%s' "$package_sha256" | grep -Eq '^[0-9a-f]{64}$' || { echo "invalid package SHA256 in $manifest" >&2; exit 1; }
	printf '%s' "$package_size" | grep -Eq '^[1-9][0-9]*$' || { echo "invalid package size in $manifest" >&2; exit 1; }
	case "$sdk_url" in
		https://downloads.openwrt.org/releases/*) ;;
		*) echo "unexpected SDK URL in $manifest" >&2; exit 1 ;;
	esac
	package=$(dirname "$manifest")/$package_name
	[ -f "$package" ] || { echo "package is missing beside $manifest" >&2; exit 1; }
	printf '%s  %s\n' "$package_sha256" "$package" | sha256sum -c - >/dev/null
	[ "$(wc -c < "$package" | tr -d ' ')" = "$package_size" ] || { echo "package size mismatch for $package" >&2; exit 1; }

	if [ "$manager" = apk ]; then
		asset=${package_name%.apk}_${architecture}.apk
	else
		asset=$package_name
	fi
	[ ! -e "$output/$asset" ] || { echo "duplicate release asset: $asset" >&2; exit 1; }
	cp "$package" "$output/$asset"
	printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$manager" "$architecture" "$asset" "$package_sha256" "$package_size" "$sdk_url" >> "$entries"
done

[ "$(wc -l < "$entries" | tr -d ' ')" -eq 8 ] || { echo 'expected exactly eight OpenWrt package manifests' >&2; exit 1; }
[ "$(cut -f1,2 "$entries" | sort -u | wc -l | tr -d ' ')" -eq 8 ] || { echo 'duplicate manager/architecture pair' >&2; exit 1; }

{
	printf 'format\tarchitecture\tasset\tsha256\tsize\tsdk_url\n'
	sort -k1,1 -k2,2 "$entries"
} > "$output/openwrt-manifest.tsv"
rm -f "$entries"

(
	cd "$output"
	sha256sum ./*.ipk ./*.apk > openwrt-checksums.txt
)

version=${tag#v}
sed -e "s/@VERSION@/$version/g" -e "s#@REPOSITORY@#$repository#g" \
	scripts/install-openwrt.sh > "$output/install-rule-bot-client-openwrt.sh"
chmod 0755 "$output/install-rule-bot-client-openwrt.sh"

test "$(find "$output" -maxdepth 1 -type f | wc -l)" -eq 11

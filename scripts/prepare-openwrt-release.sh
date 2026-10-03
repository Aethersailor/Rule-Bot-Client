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
		apk:aarch64_cortex-a53|apk:aarch64_cortex-a72|apk:aarch64_cortex-a76|\
		apk:aarch64_generic|apk:arm_arm1176jzf-s_vfp|apk:arm_arm926ej-s|\
		apk:arm_cortex-a15_neon-vfpv4|apk:arm_cortex-a5_vfpv4|apk:arm_cortex-a7|\
		apk:arm_cortex-a7_neon-vfpv4|apk:arm_cortex-a7_vfpv4|apk:arm_cortex-a8_vfpv3|\
		apk:arm_cortex-a9|apk:arm_cortex-a9_neon|apk:arm_cortex-a9_vfpv3-d16|\
		apk:arm_xscale|apk:i386_pentium-mmx|apk:i386_pentium4|\
		apk:loongarch64_generic|apk:mips64_mips64r2|apk:mips64_octeonplus|\
		apk:mips64el_mips64r2|apk:mips_24kc|apk:mips_mips32|\
		apk:mipsel_24kc|apk:mipsel_24kc_24kf|apk:mipsel_74kc|\
		apk:mipsel_mips32|apk:riscv64_generic|apk:x86_64|\
		ipk:aarch64_cortex-a53|ipk:aarch64_cortex-a72|ipk:aarch64_cortex-a76|\
		ipk:aarch64_generic|ipk:arm_arm1176jzf-s_vfp|ipk:arm_arm926ej-s|\
		ipk:arm_cortex-a15_neon-vfpv4|ipk:arm_cortex-a5_vfpv4|ipk:arm_cortex-a7|\
		ipk:arm_cortex-a7_neon-vfpv4|ipk:arm_cortex-a7_vfpv4|ipk:arm_cortex-a8_vfpv3|\
		ipk:arm_cortex-a9|ipk:arm_cortex-a9_neon|ipk:arm_cortex-a9_vfpv3-d16|\
		ipk:arm_xscale|ipk:i386_pentium-mmx|ipk:i386_pentium4|\
		ipk:loongarch64_generic|ipk:mips64_mips64r2|ipk:mips64_octeonplus|\
		ipk:mips64el_mips64r2|ipk:mips_24kc|ipk:mips_4kec|\
		ipk:mips_mips32|ipk:mipsel_24kc|ipk:mipsel_24kc_24kf|\
		ipk:mipsel_74kc|ipk:mipsel_mips32|ipk:riscv64_riscv64|\
		ipk:x86_64 ) ;;
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

[ "$(wc -l < "$entries" | tr -d ' ')" -eq 61 ] || { echo 'expected exactly 61 OpenWrt package manifests' >&2; exit 1; }
[ "$(cut -f1,2 "$entries" | sort -u | wc -l | tr -d ' ')" -eq 61 ] || { echo 'duplicate manager/architecture pair' >&2; exit 1; }

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

test "$(find "$output" -maxdepth 1 -type f | wc -l)" -eq 64

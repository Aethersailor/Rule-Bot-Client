#!/bin/sh
set -eu

version='@VERSION@'
repository='@REPOSITORY@'

case "$version:$repository" in
	*@*)
		echo 'This source template is not installable; download the generated installer from a GitHub Release.' >&2
		exit 2
		;;
esac

base_url="https://github.com/${repository}/releases/download/v${version}"
work=$(mktemp -d /tmp/rule-bot-client-install.XXXXXX)
cleanup() { rm -rf "$work"; }
trap cleanup EXIT HUP INT TERM

fetch() {
	url=$1
	destination=$2
	case "$url" in
		https://*) ;;
		*) echo "Refusing non-HTTPS URL: $url" >&2; exit 1 ;;
	esac
	if command -v uclient-fetch >/dev/null 2>&1; then
		uclient-fetch -O "$destination" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -O "$destination" "$url"
	else
		echo 'Neither uclient-fetch nor wget is available.' >&2
		exit 1
	fi
}

manifest="$work/openwrt-manifest.tsv"
fetch "$base_url/openwrt-manifest.tsv" "$manifest"

manager=
architecture=
if command -v apk >/dev/null 2>&1; then
	manager=apk
	architecture=$(apk --print-arch)
elif command -v opkg >/dev/null 2>&1; then
	manager=ipk
	opkg print-architecture > "$work/opkg-architectures"
	architecture=$(awk '
		NR == FNR { if ($1 == "arch") priority[$2] = $3 + 0; next }
		$1 == "ipk" && ($2 in priority) && priority[$2] >= best {
			best = priority[$2]; selected = $2
		}
		END { print selected }
	' "$work/opkg-architectures" "$manifest")
else
	echo 'Neither apk nor opkg is available.' >&2
	exit 1
fi

[ -n "$architecture" ] || {
	echo "No supported Rule-Bot Client architecture was found for $manager." >&2
	exit 1
}

entry=$(awk -F '\t' -v manager="$manager" -v architecture="$architecture" '
	$1 == manager && $2 == architecture { print; found++ }
	END { if (found != 1) exit 1 }
' "$manifest") || {
	echo "The release has no unique package for manager=$manager architecture=$architecture." >&2
	exit 1
}

tab=$(printf '\t')
old_ifs=$IFS
IFS=$tab
# The manifest entry must be split into its six tab-delimited fields.
# shellcheck disable=SC2086
set -- $entry
IFS=$old_ifs
[ "$#" -eq 6 ] || { echo 'The release manifest entry is invalid.' >&2; exit 1; }
asset=$3
expected_sha256=$4
expected_size=$5
sdk_url=$6

printf '%s' "$asset" | grep -Eq '^luci-app-rule-bot-client[-_+.0-9A-Za-z]+\.(ipk|apk)$' || {
	echo "Unsafe package asset name: $asset" >&2
	exit 1
}
printf '%s' "$expected_sha256" | grep -Eq '^[0-9a-f]{64}$' || {
	echo 'The release manifest contains an invalid SHA256.' >&2
	exit 1
}
printf '%s' "$expected_size" | grep -Eq '^[1-9][0-9]*$' || {
	echo 'The release manifest contains an invalid package size.' >&2
	exit 1
}
case "$sdk_url" in
	https://downloads.openwrt.org/releases/*) ;;
	*) echo "Unexpected SDK identity: $sdk_url" >&2; exit 1 ;;
esac
if [ -r /etc/openwrt_release ]; then
	detected_release=$(sed -n "s/^DISTRIB_RELEASE=['\"]\{0,1\}\([^'\"]*\)['\"]\{0,1\}$/\1/p" /etc/openwrt_release | head -n 1)
	sdk_release=$(printf '%s\n' "$sdk_url" | sed -n 's#^https://downloads\.openwrt\.org/releases/\([^/]*\)/.*#\1#p')
	detected_series=$(printf '%s\n' "$detected_release" | cut -d. -f1,2)
	sdk_series=$(printf '%s\n' "$sdk_release" | cut -d. -f1,2)
	[ -n "$detected_series" ] && [ "$detected_series" = "$sdk_series" ] || {
		echo "This package was built for OpenWrt $sdk_release, but this device reports $detected_release." >&2
		exit 1
	}
fi

package="$work/$asset"
fetch "$base_url/$asset" "$package"
actual_size=$(wc -c < "$package" | tr -d ' ')
[ "$actual_size" = "$expected_size" ] || {
	echo "Package size mismatch: expected $expected_size, got $actual_size." >&2
	exit 1
}
actual_sha256=$(sha256sum "$package" | awk '{ print $1 }')
[ "$actual_sha256" = "$expected_sha256" ] || {
	echo 'Package SHA256 mismatch.' >&2
	exit 1
}

echo "Installing Rule-Bot Client v$version for manager=$manager architecture=$architecture"
if [ "$manager" = apk ]; then
	# Release packages are hash-verified above but are not yet signed by an
	# OpenWrt repository key, so apk must be told to accept this local package.
	apk add --allow-untrusted "$package"
else
	opkg install "$package"
fi

echo 'Rule-Bot Client installation completed.'

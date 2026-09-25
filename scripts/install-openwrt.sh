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
	if command -v wget >/dev/null 2>&1; then
		if wget -O "$destination" "$url"; then
			return 0
		fi
		echo 'wget failed; trying uclient-fetch.' >&2
	fi
	if command -v uclient-fetch >/dev/null 2>&1; then
		uclient-fetch -O "$destination" "$url"
		return 0
	fi
	echo 'Neither uclient-fetch nor wget is available.' >&2
	exit 1
}

manifest="$work/openwrt-manifest.tsv"
fetch "$base_url/openwrt-manifest.tsv" "$manifest"

manager=
architecture=
release_file=${RULE_BOT_CLIENT_TEST_RELEASE_FILE:-/etc/openwrt_release}
if command -v apk >/dev/null 2>&1; then
	manager=apk
	accepted_architectures="$work/apk-architectures"
	apk_arch_file=${RULE_BOT_CLIENT_TEST_APK_ARCH_FILE:-/etc/apk/arch}
	if [ -r "$apk_arch_file" ]; then
		cp "$apk_arch_file" "$accepted_architectures"
	elif [ -r "$release_file" ]; then
		sed -n "s/^DISTRIB_ARCH=['\"]\{0,1\}\([^'\"]*\)['\"]\{0,1\}$/\1/p" "$release_file" > "$accepted_architectures"
	else
		: > "$accepted_architectures"
	fi
	architecture=$(awk -F '\t' '
		FILENAME == ARGV[1] {
			candidate = $1
			gsub(/^[[:space:]]+|[[:space:]]+$/, "", candidate)
			if (candidate ~ /^[0-9A-Za-z_+][-0-9A-Za-z_+]*$/ && !seen[candidate]++) {
				accepted[++count] = candidate
			}
			next
		}
		$1 == "apk" { available[$2]++ }
		END {
			for (position = 1; position <= count; position++) {
				if (available[accepted[position]] == 1) {
					print accepted[position]
					exit
				}
			}
		}
	' "$accepted_architectures" "$manifest")
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
if [ -r "$release_file" ]; then
	detected_distribution=$(sed -n "s/^DISTRIB_ID=['\"]\{0,1\}\([^'\"]*\)['\"]\{0,1\}$/\1/p" "$release_file" | head -n 1)
	detected_release=$(sed -n "s/^DISTRIB_RELEASE=['\"]\{0,1\}\([^'\"]*\)['\"]\{0,1\}$/\1/p" "$release_file" | head -n 1)
	sdk_release=$(printf '%s\n' "$sdk_url" | sed -n 's#^https://downloads\.openwrt\.org/releases/\([^/]*\)/.*#\1#p')
	case "$detected_distribution:$detected_release:$manager" in
		ImmortalWrt:SNAPSHOT:apk)
			echo "ImmortalWrt SNAPSHOT detected; installing the hash-verified OpenWrt $sdk_release APK compatibility build." >&2
			;;
		*)
			detected_series=$(printf '%s\n' "$detected_release" | cut -d. -f1,2)
			sdk_series=$(printf '%s\n' "$sdk_release" | cut -d. -f1,2)
			if [ -z "$detected_series" ] || [ "$detected_series" != "$sdk_series" ]; then
				echo "This package was built for OpenWrt $sdk_release, but $detected_distribution reports $detected_release." >&2
				exit 1
			fi
			;;
	esac
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

#!/bin/sh
set -eu

usage() {
	echo "usage: /etc/rule-bot-client/recover.sh <local-package-or-https-url> [sha256]" >&2
	exit 2
}

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	usage
fi
source=$1
expected=${2:-}
temporary=

cleanup() {
	[ -n "$temporary" ] && rm -f "$temporary"
}
trap cleanup EXIT

if command -v apk >/dev/null 2>&1; then
	manager=apk
	extension=.apk
	apk_arch_file=${RULE_BOT_CLIENT_TEST_APK_ARCH_FILE:-/etc/apk/arch}
	release_file=${RULE_BOT_CLIENT_TEST_RELEASE_FILE:-/etc/openwrt_release}
	architecture=
	if [ -r "$apk_arch_file" ]; then
		architecture=$(grep -E '^[0-9A-Za-z_+][-0-9A-Za-z_+]*$' "$apk_arch_file" | head -n 1)
	fi
	if [ -z "$architecture" ] && [ -r "$release_file" ]; then
		architecture=$(sed -n "s/^DISTRIB_ARCH=['\"]\{0,1\}\([^'\"]*\)['\"]\{0,1\}$/\1/p" "$release_file" | head -n 1)
	fi
	[ -n "$architecture" ] || architecture=$(apk --print-arch)
elif command -v opkg >/dev/null 2>&1; then
	manager=opkg
	extension=.ipk
	architecture=$(opkg print-architecture | awk 'END { print $2 }')
else
	echo "Neither apk nor opkg is available." >&2
	exit 1
fi

case "$source" in
	https://*)
		[ -n "$expected" ] || { echo "A SHA256 is required for URL recovery." >&2; exit 1; }
		temporary="/tmp/rule-bot-client-recover-${architecture}${extension}"
		wget -O "$temporary" "$source"
		package=$temporary
		;;
	*://*)
		echo "Only HTTPS package URLs are accepted." >&2
		exit 1
		;;
	*) package=$source ;;
esac

[ -f "$package" ] || { echo "Package not found: $package" >&2; exit 1; }
case "$package" in
	*"$extension") ;;
	*) echo "The $manager package must use the $extension extension." >&2; exit 1 ;;
esac

if [ -n "$expected" ]; then
	actual=$(sha256sum "$package" | awk '{ print $1 }')
	[ "$actual" = "$expected" ] || { echo "SHA256 mismatch." >&2; exit 1; }
fi

echo "Restoring Rule-Bot Client for manager=$manager architecture=$architecture"
if [ "$manager" = apk ]; then
	apk add --allow-untrusted "$package"
else
	opkg install "$package"
fi
/usr/libexec/rule-bot-client-openwrt initialize >/dev/null
/etc/init.d/rule-bot-client enable
/etc/init.d/rule-bot-client restart
/etc/init.d/rule-bot-client status
echo "Configuration, output, and Rule-Bot state remain on their preserved lifecycle path."

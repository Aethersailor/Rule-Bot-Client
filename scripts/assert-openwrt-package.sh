#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: assert-openwrt-package.sh <ipk|apk> <package> <expected-arch>" >&2
  exit 2
fi

manager=$1
package=$2
expected_arch=$3
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

required_paths='usr/bin/rule-bot-client
usr/libexec/rule-bot-client-openwrt
etc/init.d/rule-bot-client
etc/config/rule_bot_client
etc/rule-bot-client/recover.sh
lib/upgrade/keep.d/rule-bot-client
usr/share/rpcd/ucode/luci.rule_bot_client
usr/share/rpcd/acl.d/luci-app-rule-bot-client.json
usr/share/luci/menu.d/luci-app-rule-bot-client.json
usr/lib/lua/luci/i18n/rule_bot_client.zh-cn.lmo
www/luci-static/resources/view/rule_bot_client/overview.js
www/luci-static/resources/view/rule_bot_client/sources.js
www/luci-static/resources/view/rule_bot_client/collection.js
www/luci-static/resources/view/rule_bot_client/results.js
www/luci-static/resources/view/rule_bot_client/backup.js
www/luci-static/resources/view/rule_bot_client/diagnostics.js'
required_dependencies='ca-bundle
rpcd
ucode
ucode-mod-fs
luci-base'

case "$manager" in
  ipk)
    if ar t "$package" >/dev/null 2>&1; then
      (cd "$work" && ar x "$package")
    else
      # OpenWrt 24.10 emits the modern gzip-compressed tar container while
      # older opkg feeds may still use the Debian-style ar container.
      (cd "$work" && tar -xf "$package")
    fi
    control=$(find "$work" -maxdepth 1 -type f -name 'control.tar*' -print -quit)
    data=$(find "$work" -maxdepth 1 -type f -name 'data.tar*' -print -quit)
    [ -n "$control" ] && [ -n "$data" ]
    mkdir "$work/control"
    tar -xf "$control" -C "$work/control"
    grep -Fx "Package: luci-app-rule-bot-client" "$work/control/control"
    grep -Fx "Architecture: $expected_arch" "$work/control/control"
    sed -n 's/^Depends:[[:space:]]*//p' "$work/control/control" | tr ',' '\n' | sed 's/^[[:space:]]*//; s/[[:space:]].*$//' > "$work/dependencies"
    tar -tf "$data" | sed 's#^\./##; s#/$##' > "$work/paths"
    ;;
  apk)
    : "${APK_TOOL:?APK_TOOL is required for APK v3 inspection}"
    "$APK_TOOL" adbdump --format json "$package" > "$work/apk.json"
    jq -e --arg arch "$expected_arch" '.info.name == "luci-app-rule-bot-client" and .info.arch == $arch' "$work/apk.json" >/dev/null
    jq -r '.info.depends[]?' "$work/apk.json" > "$work/dependencies"
    jq -r '.paths[] | select(.name != null) | .name as $directory | .files[]? | $directory + "/" + .name' "$work/apk.json" > "$work/paths"
    ;;
  *)
    echo "unknown package manager: $manager" >&2
    exit 2
    ;;
esac

printf '%s\n' "$required_dependencies" | while IFS= read -r dependency; do
  grep -Fx "$dependency" "$work/dependencies" >/dev/null || {
    echo "missing runtime dependency: $dependency" >&2
    exit 1
  }
done

printf '%s\n' "$required_paths" | while IFS= read -r path; do
  grep -F "$path" "$work/paths" >/dev/null || {
    echo "missing packaged path: $path" >&2
    exit 1
  }
done

if grep -F 'usr/share/rule_bot_client/i18n/' "$work/paths" >/dev/null; then
  echo 'package must ship compiled LuCI catalogs instead of source PO files' >&2
  exit 1
fi

case "$package" in
  *.ipk) [ "$manager" = ipk ] ;;
  *.apk) [ "$manager" = apk ] ;;
  *) echo "package has an unexpected extension" >&2; exit 1 ;;
esac

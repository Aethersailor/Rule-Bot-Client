#!/bin/sh
set -eu

root=openwrt/package/luci-app-rule-bot-client/files

test -f openwrt/package/luci-app-rule-bot-client/Makefile
test "$(grep -c '^[[:space:]]*- manager: ipk$' .github/workflows/openwrt-packages.yml)" -eq 4
test "$(grep -c '^[[:space:]]*- manager: apk$' .github/workflows/openwrt-packages.yml)" -eq 4
test "$(grep -c '^[[:space:]]*- manager:' .github/workflows/openwrt-packages.yml)" -eq 8
test -x "$root/etc/init.d/rule-bot-client" || test -f "$root/etc/init.d/rule-bot-client"
test -f "$root/etc/config/rule_bot_client"
test -f "$root/lib/upgrade/keep.d/rule-bot-client"
test -f "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
test -f "$root/usr/share/rpcd/acl.d/luci-app-rule-bot-client.json"
test -f "$root/usr/share/luci/menu.d/luci-app-rule-bot-client.json"
test -f openwrt/package/luci-app-rule-bot-client/po/zh_Hans/rule_bot_client.po
grep -F 'PKG_BUILD_DEPENDS:=luci-base/host' openwrt/package/luci-app-rule-bot-client/Makefile
# These are intentional literal build-variable references.
# shellcheck disable=SC2016
grep -F 'PKG_VERSION:=$(if $(RULE_BOT_CLIENT_VERSION),$(RULE_BOT_CLIENT_VERSION),0.1.0)' openwrt/package/luci-app-rule-bot-client/Makefile
# shellcheck disable=SC2016
grep -F 'version="0.1.0_git${GITHUB_RUN_ID}"' .github/workflows/openwrt-packages.yml
# This is an intentional literal OpenWrt make variable reference.
# shellcheck disable=SC2016
grep -F '$(STAGING_DIR_HOSTPKG)/bin/po2lmo ./po/zh_Hans/rule_bot_client.po' openwrt/package/luci-app-rule-bot-client/Makefile
grep -F '/usr/lib/lua/luci/i18n/rule_bot_client.zh-cn.lmo' openwrt/package/luci-app-rule-bot-client/Makefile
# These are intentional literal workflow environment variable references.
# shellcheck disable=SC2016
grep -F './scripts/feeds update luci' .github/workflows/openwrt-packages.yml
grep -F 'make -C feeds/luci/modules/luci-base/src po2lmo CC=cc' .github/workflows/openwrt-packages.yml
grep -F 'install -D -m 0755 feeds/luci/modules/luci-base/src/po2lmo staging_dir/hostpkg/bin/po2lmo' .github/workflows/openwrt-packages.yml
# shellcheck disable=SC2016
grep -F 'test -x "$SDK_DIR/staging_dir/hostpkg/bin/po2lmo"' .github/workflows/openwrt-packages.yml

for path in \
  /etc/config/rule_bot_client \
  /etc/rule-bot-client/credentials/ \
  /etc/rule-bot-client/certs/ \
  /etc/rule-bot-client/exclude.list \
  /etc/rule-bot-client/data/ \
  /etc/rule-bot-client/recover.sh; do
  grep -Fx "$path" "$root/lib/upgrade/keep.d/rule-bot-client"
done

grep -F 'procd_add_reload_trigger rule_bot_client openclash nikki' "$root/etc/init.d/rule-bot-client"
grep -F '/var/run/rule-bot-client/config.json' "$root/etc/init.d/rule-bot-client"
grep -F "return { 'luci.rule_bot_client': methods };" "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
grep -F 'const allowed_actions = {' "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
grep -F 'const process = popen(' "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
if grep -F "bus.call('file', 'exec'" "$root/usr/share/rpcd/ucode/luci.rule_bot_client"; then
  echo 'rpcd ucode must not synchronously recurse through file.exec' >&2
  exit 1
fi
if grep -F "invoke('domains', request.args)" "$root/usr/share/rpcd/ucode/luci.rule_bot_client"; then
  echo 'rpcd ucode must not forward LuCI session metadata to the strict domains backend' >&2
  exit 1
fi

for file in "$root"/usr/share/rpcd/acl.d/*.json "$root"/usr/share/luci/menu.d/*.json; do
  jq -e . "$file" >/dev/null
done

for file in "$root"/www/luci-static/resources/rule_bot_client/*.js "$root"/www/luci-static/resources/view/rule_bot_client/*.js; do
  node --check "$file" >/dev/null
done

node - "$root" openwrt/package/luci-app-rule-bot-client/po/zh_Hans/rule_bot_client.po <<'NODE'
const fs = require('fs');
const path = require('path');
const root = process.argv[2];
const poPath = process.argv[3];
const menuPath = path.join(root, 'usr/share/luci/menu.d/luci-app-rule-bot-client.json');
const menu = JSON.parse(fs.readFileSync(menuPath, 'utf8'));
const expectedMenu = {
  'admin/services/rule_bot_client/overview': 'Overview',
  'admin/services/rule_bot_client/sources': 'Listening targets',
  'admin/services/rule_bot_client/collection': 'Collection and Rule-Bot',
  'admin/services/rule_bot_client/results': 'Local results',
  'admin/services/rule_bot_client/backup': 'Backup and restore',
  'admin/services/rule_bot_client/diagnostics': 'Logs and diagnostics'
};
for (const [route, title] of Object.entries(expectedMenu)) {
  if (menu[route]?.title !== title)
    throw new Error(`menu title ${route} must use the English translation key ${JSON.stringify(title)}`);
}

const jsFiles = [
  path.join(root, 'www/luci-static/resources/rule_bot_client/api.js'),
  ...fs.readdirSync(path.join(root, 'www/luci-static/resources/view/rule_bot_client'))
    .filter((name) => name.endsWith('.js'))
    .map((name) => path.join(root, 'www/luci-static/resources/view/rule_bot_client', name))
];
const keys = new Set(Object.values(expectedMenu));
for (const file of jsFiles) {
  const source = fs.readFileSync(file, 'utf8');
  if (source.includes('api.tr('))
    throw new Error(`${file} still uses the private translation map`);
  for (const match of source.matchAll(/_\(\s*'((?:\\.|[^'\\])*)'\s*\)/g))
    keys.add(match[1].replace(/\\'/g, "'").replace(/\\\\/g, '\\'));
}
if (keys.size < 90)
  throw new Error(`expected at least 90 native LuCI translation keys, found ${keys.size}`);

const po = fs.readFileSync(poPath, 'utf8');
const translations = new Map();
for (const match of po.matchAll(/^msgid "((?:\\.|[^"\\])*)"\r?\nmsgstr "((?:\\.|[^"\\])*)"/gm))
  translations.set(match[1].replace(/\\"/g, '"'), match[2].replace(/\\"/g, '"'));
for (const key of keys) {
  if (!translations.get(key))
    throw new Error(`missing Simplified Chinese translation for ${JSON.stringify(key)}`);
}
NODE

msgfmt --check --check-format -o /dev/null openwrt/package/luci-app-rule-bot-client/po/zh_Hans/rule_bot_client.po

node - "$root/www/luci-static/resources/rule_bot_client/api.js" <<'NODE'
const fs = require('fs');
const source = fs.readFileSync(process.argv[2], 'utf8');
const rpc = { declare: () => () => Promise.resolve({ ok: true }) };
const ui = { addNotification: () => {} };
const baseclass = {
  extend: (methods) => {
    function LuCIModule() {}
    LuCIModule.prototype = methods;
    return LuCIModule;
  }
};
const factory = new Function('rpc', 'ui', 'baseclass', 'L', 'E', source);
const Module = factory(rpc, ui, baseclass, { env: { lang: 'en' } }, () => ({}));
if (typeof Module !== 'function')
  throw new Error('rule_bot_client.api must yield a LuCI constructor');
const api = new Module();
for (const method of [ 'config', 'status', 'probe', 'domains', 'save', 'clear', 'restore', 'service' ]) {
  if (typeof api[method] !== 'function')
    throw new Error(`rule_bot_client.api is missing method ${method}`);
}
NODE

sh -n "$root/etc/init.d/rule-bot-client"
sh -n "$root/etc/rule-bot-client/recover.sh"

if command -v ucode >/dev/null 2>&1; then
  compiled=$(mktemp)
  trap 'rm -f "$compiled"' EXIT
  ucode -c -o "$compiled" "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
  test -s "$compiled"
fi

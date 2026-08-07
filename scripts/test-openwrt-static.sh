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
test -f openwrt/package/luci-app-rule-bot-client/po/templates/rule_bot_client.pot
test -f scripts/install-openwrt.sh
test -f scripts/prepare-openwrt-release.sh
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
grep -F "error_code: 'backend_start_failed'" "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
grep -F '"error_code": "backend_request_failed"' cmd/rule-bot-client-openwrt/main.go
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

node - "$root" <<'NODE'
const fs = require('fs');
const path = require('path');
const root = process.argv[2];
const menuPath = path.join(root, 'usr/share/luci/menu.d/luci-app-rule-bot-client.json');
const menu = JSON.parse(fs.readFileSync(menuPath, 'utf8'));
const expectedMenu = {
  'admin/services/rule_bot_client': 'Rule-Bot Client',
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
NODE

node scripts/openwrt-i18n.js check openwrt/package/luci-app-rule-bot-client \
  openwrt/package/luci-app-rule-bot-client/po/zh_Hans/rule_bot_client.po
generated_pot=$(mktemp)
node scripts/openwrt-i18n.js pot openwrt/package/luci-app-rule-bot-client > "$generated_pot"
cmp "$generated_pot" openwrt/package/luci-app-rule-bot-client/po/templates/rule_bot_client.pot
rm -f "$generated_pot"

msgfmt --check --check-format -o /dev/null openwrt/package/luci-app-rule-bot-client/po/zh_Hans/rule_bot_client.po

node - "$root/www/luci-static/resources/rule_bot_client/api.js" <<'NODE'
const fs = require('fs');
const source = fs.readFileSync(process.argv[2], 'utf8');
global._ = (message) => message;
const rpc = { declare: (spec) => () => Promise.resolve(spec.method === 'status'
  ? { ok: false, error_code: 'backend_request_failed', error: 'raw backend detail' }
  : { ok: true }) };
const ui = { addNotification: () => {} };
const baseclass = {
  extend: (methods) => {
    function LuCIModule() {}
    LuCIModule.prototype = methods;
    return LuCIModule;
  }
};
const factory = new Function('rpc', 'ui', 'baseclass', 'L', 'E', source);
const element = (tag, attributes, children) => ({ tag, attributes, children });
const Module = factory(rpc, ui, baseclass, { env: { lang: 'en' } }, element);
if (typeof Module !== 'function')
  throw new Error('rule_bot_client.api must yield a LuCI constructor');
const api = new Module();
for (const method of [ 'config', 'status', 'probe', 'domains', 'save', 'clear', 'restore', 'service' ]) {
  if (typeof api[method] !== 'function')
    throw new Error(`rule_bot_client.api is missing method ${method}`);
}
(async () => {
  try {
    await api.status();
    throw new Error('failed RPC result was not rejected');
  } catch (error) {
    if (error.message !== 'Operation failed' || error.detail !== 'raw backend detail' || error.code !== 'backend_request_failed')
      throw error;
    const nodes = api.errorNodes(error);
    if (nodes.length !== 2 || nodes[0].children !== 'Operation failed' || nodes[1].tag !== 'details')
      throw new Error('localized error summary and technical detail were not rendered separately');
  }
})().catch((error) => { console.error(error); process.exitCode = 1; });
NODE

sh -n "$root/etc/init.d/rule-bot-client"
sh -n "$root/etc/rule-bot-client/recover.sh"

if command -v ucode >/dev/null 2>&1; then
  compiled=$(mktemp)
  trap 'rm -f "$compiled"' EXIT
  ucode -c -o "$compiled" "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
  test -s "$compiled"
fi

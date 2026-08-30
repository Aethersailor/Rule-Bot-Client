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
grep -F 'DEPENDS:=+ca-bundle +rpcd +rpcd-mod-ucode +ucode +ucode-mod-fs +luci-base' openwrt/package/luci-app-rule-bot-client/Makefile
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
grep -F 'procd_set_param user nobody' "$root/etc/init.d/rule-bot-client"
grep -F 'procd_set_param group nogroup' "$root/etc/init.d/rule-bot-client"
grep -F "update_auto >/dev/null 2>&1" openwrt/package/luci-app-rule-bot-client/Makefile
# This is an intentional literal OpenWrt make variable reference.
# shellcheck disable=SC2016
grep -F 'initialize_output=$$(/usr/libexec/rule-bot-client-openwrt initialize 2>&1)' openwrt/package/luci-app-rule-bot-client/Makefile
grep -F 'configuration initialization failed; the package was installed with its service stopped' openwrt/package/luci-app-rule-bot-client/Makefile
test "$(grep -Fc '/etc/init.d/rpcd reload >/dev/null 2>&1 || true' openwrt/package/luci-app-rule-bot-client/Makefile)" -eq 2
grep -F 'define Package/luci-app-rule-bot-client/postrm' openwrt/package/luci-app-rule-bot-client/Makefile
if grep -F '/etc/init.d/rpcd restart' openwrt/package/luci-app-rule-bot-client/Makefile; then
  echo 'package lifecycle must not restart rpcd from inside an rpcd install request' >&2
  exit 1
fi
if grep -F 'initialize >/dev/null 2>&1 || exit 1' openwrt/package/luci-app-rule-bot-client/Makefile; then
  echo 'package lifecycle must preserve initialization diagnostics' >&2
  exit 1
fi
grep -F "return { 'luci.rule_bot_client': methods };" "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
grep -F 'const allowed_actions = {' "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
grep -F 'const process = popen(' "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
grep -F 'chmod(request, 0o600)' "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
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
grep -F 'api.updateCheck()' "$root/www/luci-static/resources/view/rule_bot_client/update.js"
grep -F 'api.updateConfig(automatic.checked)' "$root/www/luci-static/resources/view/rule_bot_client/update.js"
grep -F 'api.updateStart()' "$root/www/luci-static/resources/view/rule_bot_client/update.js"
grep -F '"--no-network"' internal/openwrt/update.go
if grep -F '"--network=false"' internal/openwrt/update.go; then
  echo 'APK 3 requires --no-network instead of --network=false' >&2
  exit 1
fi

for file in "$root"/usr/share/rpcd/acl.d/*.json "$root"/usr/share/luci/menu.d/*.json; do
  jq -e . "$file" >/dev/null
done

acl="$root/usr/share/rpcd/acl.d/luci-app-rule-bot-client.json"
jq -e '.["luci-app-rule-bot-client"].read.ubus["luci.rule_bot_client"] | index("config_edit") | not' "$acl" >/dev/null
jq -e '.["luci-app-rule-bot-client"].write.ubus["luci.rule_bot_client"] | index("config_edit") != null' "$acl" >/dev/null
jq -e '.["luci-app-rule-bot-client"].read.ubus["luci.rule_bot_client"] | index("update_status") != null' "$acl" >/dev/null
jq -e '.["luci-app-rule-bot-client"].write.ubus["luci.rule_bot_client"] | index("update_status") | not' "$acl" >/dev/null
for method in config_edit save clear backup restore service update_check update_config update_start; do
  jq -e --arg method "$method" '.["luci-app-rule-bot-client"].read.ubus["luci.rule_bot_client"] | index($method) | not' "$acl" >/dev/null
  jq -e --arg method "$method" '.["luci-app-rule-bot-client"].write.ubus["luci.rule_bot_client"] | index($method) != null' "$acl" >/dev/null
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
  'admin/services/rule_bot_client/update': 'Software update',
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
for (const method of [ 'config', 'configEdit', 'status', 'probe', 'domains', 'logs', 'backup', 'upgrade', 'updateStatus', 'updateCheck', 'updateConfig', 'updateStart', 'save', 'clear', 'restore', 'service' ]) {
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

node - "$root/www/luci-static/resources/view/rule_bot_client/sources.js" <<'NODE'
const fs = require('fs');
const source = fs.readFileSync(process.argv[2], 'utf8');
const view = { extend: (methods) => methods };
let modal;
let saved;
let reloaded = false;
const api = {
  save: (settings) => { saved = JSON.parse(JSON.stringify(settings)); return Promise.resolve({ ok: true }); },
  notifyError: (error) => { throw error; },
  detailNode: () => ({}),
  probe: () => Promise.resolve({ ok: true })
};
const ui = {
  addNotification: () => {},
  showModal: (title, nodes) => { modal = { title, nodes }; },
  hideModal: () => {},
  createHandlerFn: (scope, handler, ...args) => () => handler.apply(scope, args)
};
function element(tag, attributes, children) {
  if (children === undefined && (Array.isArray(attributes) || typeof attributes === 'string')) {
    children = attributes;
    attributes = {};
  }
  const listeners = {};
  const node = {
    tag, attributes: attributes || {}, children, listeners,
    value: attributes && attributes.value !== undefined ? attributes.value : '',
    checked: attributes && attributes.checked === true,
    addEventListener: (name, handler) => { listeners[name] = handler; }
  };
  return node;
}
function find(node, predicate) {
  if (Array.isArray(node)) {
    for (const child of node) {
      const match = find(child, predicate);
      if (match) return match;
    }
    return null;
  }
  if (!node || typeof node !== 'object') return null;
  if (predicate(node)) return node;
  return find(node.children, predicate);
}
function rowControl(root, label) {
  const row = find(root, (node) => node.attributes && node.attributes.class === 'cbi-value' &&
    find(node.children, (child) => child.tag === 'label' && child.children === label));
  return row && find(row.children, (node) => node.tag === 'input');
}
global.location = { reload: () => { reloaded = true; } };
const factory = new Function('view', 'ui', 'api', 'E', '_', source);
const module = factory(view, ui, api, element, (message) => message);
const settings = {
  sources: [
    { id: 'openclash', type: 'openclash', enabled: true, name: 'OpenClash', secret_set: true },
    { id: 'nikki', type: 'nikki', enabled: true, name: 'Nikki', secret_set: false }
  ]
};
const rendered = module.render([ settings, { adapters: { openclash: { available: true }, nikki: { available: true } } } ]);
if (!find(rendered, (node) => node.tag === 'button' && node.children === 'Controller secret'))
  throw new Error('automatic adapters do not expose a Controller secret override action');
module.showEditor({
  id: 'src_0123abcd', type: 'manual', enabled: true, name: 'Manual',
  url: 'http://127.0.0.1:9090', secret_set: true
}, null);
const manualPassword = find(modal.nodes, (node) => node.tag === 'input' && node.attributes.type === 'password');
const manualClear = rowControl(modal.nodes, 'Clear existing secret');
if (!manualPassword || manualPassword.value !== '' || manualPassword.attributes.placeholder !== 'Secret configured; leave empty to preserve')
  throw new Error('manual Controller secret input must be direct and must never prefill the stored secret');
manualPassword.value = 'replacement-manual-secret';
manualPassword.listeners.input();
manualClear.checked = true;
manualClear.listeners.change();
if (manualPassword.value !== '')
  throw new Error('manual Controller secret clear did not remove the newly entered value');
module.showAdapterSecret(settings.sources[0], 0);
const password = find(modal.nodes, (node) => node.tag === 'input' && node.attributes.type === 'password');
const clear = find(modal.nodes, (node) => node.tag === 'input' && node.attributes.type === 'checkbox');
const save = find(modal.nodes, (node) => node.tag === 'button' && node.children === 'Save');
if (!password || password.value !== '' || password.attributes.placeholder !== 'Override configured; leave empty to preserve')
  throw new Error('adapter override must never prefill the stored secret');
password.value = 'replacement-secret';
password.listeners.input();
clear.checked = true;
clear.listeners.change();
if (password.value !== '')
  throw new Error('selecting clear did not remove the newly entered secret');
password.value = 'replacement-secret';
password.listeners.input();
if (clear.checked)
  throw new Error('entering a secret did not clear the conflicting removal request');
Promise.resolve(save.attributes.click()).then(() => {
  if (!saved || saved.sources[0].secret !== 'replacement-secret' || saved.sources[0].clear_secret !== false)
    throw new Error('adapter override was not submitted through the write-authorized save RPC');
  if (!reloaded)
    throw new Error('adapter override save did not refresh the page');
}).catch((error) => { console.error(error); process.exitCode = 1; });
NODE

node - "$root/www/luci-static/resources/view/rule_bot_client/collection.js" <<'NODE'
const fs = require('fs');
const source = fs.readFileSync(process.argv[2], 'utf8');
const view = { extend: (methods) => methods };
let saved;
const api = {
  save: (settings) => { saved = JSON.parse(JSON.stringify(settings)); return Promise.resolve({ ok: true }); },
  notifyError: (error) => { throw error; }
};
const ui = { addNotification: () => {} };
function element(tag, attributes, children) {
  if (children === undefined && (Array.isArray(attributes) || typeof attributes === 'string')) {
    children = attributes;
    attributes = {};
  }
  const listeners = {};
  return {
    tag, attributes: attributes || {}, children, listeners,
    value: attributes && attributes.value !== undefined ? attributes.value : '',
    checked: attributes && attributes.checked === true,
    addEventListener: (name, handler) => { listeners[name] = handler; }
  };
}
function find(node, predicate) {
  if (Array.isArray(node)) {
    for (const child of node) {
      const match = find(child, predicate);
      if (match) return match;
    }
    return null;
  }
  if (!node || typeof node !== 'object') return null;
  if (predicate(node)) return node;
  return find(node.children, predicate);
}
function rowControl(root, label) {
  const row = find(root, (node) => node.attributes && node.attributes.class === 'cbi-value' &&
    find(node.children, (child) => child.tag === 'label' && child.children === label));
  return row && find(row.children, (node) => node.tag === 'input');
}
global.location = { reload: () => {} };
const factory = new Function('view', 'ui', 'api', 'E', '_', source);
const module = factory(view, ui, api, element, (message) => message);
const rendered = module.render({
  enabled: true, work_mode: 'rulebot', domain_mode: 'registrable_domain', flush_interval: '5s',
  storage: { mode: 'persistent' }, sources: [],
  rule_bot: { enabled: true, endpoint: 'https://rule-bot.example/api/hidden', token_set: true, send_existing: false }
});
const token = rowControl(rendered, 'Token');
const clear = rowControl(rendered, 'Clear existing token');
const save = find(rendered, (node) => node.tag === 'button' && node.children === 'Save and apply');
if (!token || token.value !== '' || token.attributes.placeholder !== 'Token configured; leave empty to preserve')
  throw new Error('Rule-Bot token input must never prefill the stored token');
token.value = 'replacement-token';
token.listeners.input();
clear.checked = true;
clear.listeners.change();
if (token.value !== '')
  throw new Error('selecting token clear did not remove the newly entered token');
token.value = 'replacement-token';
token.listeners.input();
Promise.resolve(save.attributes.click()).then(() => {
  if (!saved || saved.rule_bot.token !== 'replacement-token' || saved.rule_bot.clear_token !== false)
    throw new Error('Rule-Bot token was not submitted through save');
}).catch((error) => { console.error(error); process.exitCode = 1; });
NODE

node - "$root/www/luci-static/resources/view/rule_bot_client/overview.js" <<'NODE'
const fs = require('fs');
const source = fs.readFileSync(process.argv[2], 'utf8');
const view = { extend: (methods) => methods };
const poll = { add: () => {} };
const ui = { addNotification: () => {} };
let saved;
let reloaded = false;
const serviceActions = [];
const api = {
  save: (settings) => { saved = settings; return Promise.resolve({ ok: true }); },
  service: (action) => { serviceActions.push(action); return Promise.resolve({ ok: true }); },
  notifyError: (error) => { throw error; },
  detailNode: () => ({})
};
function element(tag, attributes, children) {
  if (children === undefined && (Array.isArray(attributes) || typeof attributes === 'string')) {
    children = attributes;
    attributes = {};
  }
  const listeners = {};
  return {
    tag, attributes: attributes || {}, children, listeners,
    addEventListener: (name, handler) => { listeners[name] = handler; },
    replaceChildren: () => {}
  };
}
global.location = { reload: () => { reloaded = true; } };
const factory = new Function('view', 'poll', 'ui', 'api', 'E', '_', source);
const module = factory(view, poll, ui, api, element, (message) => message);
const status = {
  service: 'running', config: { enabled: true, sources: [], storage: { mode: 'persistent' } },
  runtime: {}, output: {}, storage: {}
};
const rendered = module.render(status);
function find(node, predicate) {
  if (!node || typeof node !== 'object')
    return null;
  if (predicate(node))
    return node;
  const children = Array.isArray(node.children) ? node.children : [ node.children ];
  for (const child of children) {
    const match = find(child, predicate);
    if (match)
      return match;
  }
  return null;
}
const toggle = find(rendered, (node) => node.tag === 'input' && node.attributes.class === 'cbi-input-checkbox');
if (!toggle || typeof toggle.listeners.change !== 'function' || toggle.checked !== true)
  throw new Error('overview service master switch was not rendered as enabled');
const reload = find(rendered, (node) => node.tag === 'button' && node.children === 'Reload');
const restart = find(rendered, (node) => node.tag === 'button' && node.children === 'Restart');
if (!reload || !restart || reload.disabled !== false || restart.disabled !== false)
  throw new Error('overview service buttons were not enabled with the service master switch');
if ('disabled' in reload.attributes || 'disabled' in restart.attributes)
  throw new Error('overview service buttons must not render a false disabled attribute');
Promise.all([ reload.attributes.click(), restart.attributes.click() ]).then(() => {
  if (serviceActions.join(',') !== 'reload,restart')
    throw new Error(`overview service actions = ${serviceActions.join(',')}`);
}).catch((error) => { console.error(error); process.exitCode = 1; });
const disabledStatus = Object.assign({}, status, { config: Object.assign({}, status.config, { enabled: false }) });
const disabledRendered = module.render(disabledStatus);
const disabledReload = find(disabledRendered, (node) => node.tag === 'button' && node.children === 'Reload');
const disabledRestart = find(disabledRendered, (node) => node.tag === 'button' && node.children === 'Restart');
if (!disabledReload || !disabledRestart || disabledReload.disabled !== true || disabledRestart.disabled !== true)
  throw new Error('overview service buttons were not disabled with the service master switch');
toggle.checked = false;
Promise.resolve(toggle.listeners.change()).then(() => {
  if (!saved || saved.enabled !== false)
    throw new Error('overview service master switch did not persist disabled state');
  if (status.config.enabled !== true)
    throw new Error('overview service master switch mutated the polled status object');
  if (!reloaded)
    throw new Error('overview did not refresh after changing the service master switch');
}).catch((error) => { console.error(error); process.exitCode = 1; });
NODE

sh -n "$root/etc/init.d/rule-bot-client"
sh -n "$root/etc/rule-bot-client/recover.sh"

if command -v ucode >/dev/null 2>&1; then
  compiled=$(mktemp)
  trap 'rm -f "$compiled"' EXIT
  ucode -c -o "$compiled" "$root/usr/share/rpcd/ucode/luci.rule_bot_client"
  test -s "$compiled"
fi

'use strict';
'require view';
'require ui';
'require rule_bot_client.api as api';

function row(label, control, description) {
	return E('div', { class: 'cbi-value' }, [
		E('label', { class: 'cbi-value-title' }, label),
		E('div', { class: 'cbi-value-field' }, [ control, description ? E('div', { class: 'cbi-value-description' }, description) : '' ])
	]);
}

function option(value, text) { return E('option', { value: value }, text); }

return view.extend({
	load: function() { return api.config(); },
	render: function(settings) {
		const enabled = E('input', { type: 'checkbox' });
		enabled.checked = settings.enabled !== false;
		const workMode = E('select', { class: 'cbi-input-select' }, [ option('local', _('Local collection only')), option('rulebot', _('Local collection and Rule-Bot')) ]);
		workMode.value = settings.work_mode;
		const domainMode = E('select', { class: 'cbi-input-select' }, [ option('registrable_domain', _('Registrable domain')), option('hostname', _('Full hostname')) ]);
		domainMode.value = settings.domain_mode;
		const storageMode = E('select', { class: 'cbi-input-select' }, [ option('persistent', _('Persistent /etc/rule-bot-client/data')), option('temporary', _('Temporary /tmp/rule-bot-client/data')), option('external', _('External mount')) ]);
		storageMode.value = settings.storage.mode;
		const external = E('input', { class: 'cbi-input-text', value: settings.storage.external_path || '', placeholder: '/mnt/usb/rule-bot-client' });
		const endpoint = E('input', { class: 'cbi-input-text', value: settings.rule_bot.endpoint || '', placeholder: 'https://rule-bot.example/api/hidden/path' });
		const token = E('input', { class: 'cbi-input-password', type: 'password', value: '', placeholder: settings.rule_bot.token_set ? _('Token configured; leave empty to preserve') : _('Paste token') });
		const clearToken = E('input', { type: 'checkbox' });
		const sendExisting = E('input', { type: 'checkbox' });
		sendExisting.checked = !!settings.rule_bot.send_existing;
		const proxy = E('input', { class: 'cbi-input-text', value: settings.rule_bot.proxy_url || '', placeholder: _('Optional') + ': http://127.0.0.1:7890' });
		const flush = E('input', { class: 'cbi-input-text', value: settings.flush_interval || '5s' });
		const save = function() {
			settings.enabled = enabled.checked;
			settings.work_mode = workMode.value;
			settings.domain_mode = domainMode.value;
			settings.flush_interval = flush.value;
			settings.storage.mode = storageMode.value;
			settings.storage.external_path = external.value;
			settings.rule_bot.enabled = workMode.value === 'rulebot';
			settings.rule_bot.endpoint = endpoint.value;
			settings.rule_bot.token = token.value;
			settings.rule_bot.clear_token = clearToken.checked;
			settings.rule_bot.send_existing = sendExisting.checked;
			settings.rule_bot.proxy_url = proxy.value;
			return api.save(settings).then(function() {
				ui.addNotification(null, E('p', {}, _('Operation completed')), 'info');
				location.reload();
			}).catch(api.notifyError);
		};
		return E('div', {}, [
			E('h2', {}, _('Rule-Bot Client') + ' - ' + _('Collection and Rule-Bot')),
			row(_('Enabled'), enabled),
			row(_('Work mode'), workMode, _('Rule-Bot is a single shared delivery state for all controller instances.')),
			row(_('Domain mode'), domainMode),
			row(_('Flush interval'), flush),
			row(_('Storage mode'), storageMode, _('External storage is never replaced by overlay storage.')),
			row(_('External data directory'), external),
			E('h3', {}, 'Rule-Bot'),
			row(_('Complete endpoint'), endpoint, _('Paste the complete endpoint including its non-root path.')),
			row(_('Token'), token),
			row(_('Clear existing token'), clearToken),
			row(_('Send existing domains'), sendExisting, _('Default is off to prevent historical bulk delivery.')),
			row(_('Outbound proxy URL'), proxy),
			E('div', { class: 'cbi-page-actions' }, E('button', { class: 'btn cbi-button-positive important', click: save }, _('Save and apply')))
		]);
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

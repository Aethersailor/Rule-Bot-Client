'use strict';
'require view';
'require ui';
'require rule_bot_client.api as api';

function inputRow(label, input) {
	return E('div', { class: 'cbi-value' }, [ E('label', { class: 'cbi-value-title' }, label), E('div', { class: 'cbi-value-field' }, input) ]);
}

function clone(value) {
	return JSON.parse(JSON.stringify(value));
}

return view.extend({
	load: function() { return Promise.all([ api.config(), api.status() ]); },

	saveAll: function() {
		return api.save(this.settings).then(function() {
			ui.addNotification(null, E('p', {}, _('Operation completed')), 'info');
			location.reload();
		}).catch(api.notifyError);
	},

	probe: function(id) {
		ui.showModal(_('Test connection'), [ E('p', { class: 'spinning' }, _('Test connection') + '...') ]);
		return api.probe(id).then(function(result) {
			ui.showModal(_('Test connection'), [
				E('p', {}, result.ok ? _('Connected') : _('Disconnected')),
				E('pre', {}, JSON.stringify(result, null, 2)),
				E('div', { class: 'right' }, E('button', { class: 'btn', click: ui.hideModal }, _('Close')))
			]);
		}).catch(function(error) {
			ui.showModal(_('Test connection'), [ E('p', {}, error.message), E('div', { class: 'right' }, E('button', { class: 'btn', click: ui.hideModal }, _('Close'))) ]);
		});
	},

	showEditor: function(source, index) {
		const current = clone(source || {
			id: '', type: 'manual', enabled: true, name: '', url: 'http://192.168.1.2:9090',
			secret: '', ca_pem: '', tls_server_name: '', insecure_skip_verify: false
		});
		const name = E('input', { class: 'cbi-input-text', value: current.name || '' });
		const url = E('input', { class: 'cbi-input-text', value: current.url || '' });
		const secret = E('input', { class: 'cbi-input-password', type: 'password', value: '', placeholder: current.secret_set ? _('Secret configured') : _('No secret configured') });
		const clearSecret = E('input', { type: 'checkbox' });
		const ca = E('textarea', { class: 'cbi-input-textarea', rows: 6, placeholder: current.ca_set ? _('CA already configured; leave empty to preserve') : '-----BEGIN CERTIFICATE-----' });
		const clearCA = E('input', { type: 'checkbox' });
		const serverName = E('input', { class: 'cbi-input-text', value: current.tls_server_name || '' });
		const skip = E('input', { type: 'checkbox' });
		skip.checked = !!current.insecure_skip_verify;
		const enabled = E('input', { type: 'checkbox' });
		enabled.checked = current.enabled !== false;
		ui.showModal((source ? _('Edit') : _('Add target')), [
			inputRow(_('Enabled'), enabled),
			inputRow(_('Display name'), name),
			inputRow(_('Controller URL'), url),
			inputRow(_('Controller secret'), secret),
			inputRow(_('Clear existing secret'), clearSecret),
			inputRow(_('Custom CA PEM'), ca),
			inputRow(_('Clear existing CA'), clearCA),
			inputRow(_('TLS server name'), serverName),
			inputRow(_('Skip certificate verification'), skip),
			E('p', {}, _('An offline controller may be saved; Rule-Bot Client will reconnect independently.')),
			E('div', { class: 'right' }, [
				E('button', { class: 'btn', click: ui.hideModal }, _('Cancel')), ' ',
				E('button', { class: 'btn cbi-button-positive important', click: ui.createHandlerFn(this, function() {
					current.enabled = enabled.checked;
					current.name = name.value;
					current.url = url.value;
					current.secret = secret.value;
					current.clear_secret = clearSecret.checked;
					current.ca_pem = ca.value;
					current.clear_ca = clearCA.checked;
					current.tls_server_name = serverName.value;
					current.insecure_skip_verify = skip.checked;
					if (index == null)
						this.settings.sources.push(current);
					else
						this.settings.sources[index] = current;
					return api.save(this.settings).then(function() { ui.hideModal(); location.reload(); }).catch(api.notifyError);
				}) }, _('Save'))
			])
		]);
	},

	showNikkiTLS: function(source, index) {
		const current = clone(source);
		const ca = E('textarea', { class: 'cbi-input-textarea', rows: 6, placeholder: current.ca_set ? _('CA already configured; leave empty to preserve') : '-----BEGIN CERTIFICATE-----' });
		const clearCA = E('input', { type: 'checkbox' });
		const serverName = E('input', { class: 'cbi-input-text', value: current.tls_server_name || '' });
		const skip = E('input', { type: 'checkbox' });
		skip.checked = !!current.insecure_skip_verify;
		ui.showModal(_('Nikki TLS settings'), [
			inputRow(_('Custom CA PEM'), ca),
			inputRow(_('Clear existing CA'), clearCA),
			inputRow(_('TLS server name'), serverName),
			inputRow(_('Skip certificate verification'), skip),
			E('div', { class: 'right' }, [
				E('button', { class: 'btn', click: ui.hideModal }, _('Cancel')), ' ',
				E('button', { class: 'btn cbi-button-positive important', click: ui.createHandlerFn(this, function() {
					current.ca_pem = ca.value;
					current.clear_ca = clearCA.checked;
					current.tls_server_name = serverName.value;
					current.insecure_skip_verify = skip.checked;
					this.settings.sources[index] = current;
					return api.save(this.settings).then(function() { ui.hideModal(); location.reload(); }).catch(api.notifyError);
				}) }, _('Save'))
			])
		]);
	},

	render: function(data) {
		this.settings = data[0];
		const status = data[1];
		const automatic = [];
		const manual = [];
		this.settings.sources.forEach(function(source, index) {
			const adapter = (status.adapters || {})[source.id] || {};
			if (source.type === 'manual') {
				manual.push(E('tr', { class: 'tr' }, [
					E('td', { class: 'td' }, source.enabled ? '✓' : '—'),
					E('td', { class: 'td' }, source.name),
					E('td', { class: 'td' }, source.url),
					E('td', { class: 'td' }, source.secret_set ? _('Secret configured') : _('No secret configured')),
					E('td', { class: 'td' }, [
						E('button', { class: 'btn cbi-button-action', click: ui.createHandlerFn(this, this.probe, source.id) }, _('Test connection')), ' ',
						E('button', { class: 'btn', click: ui.createHandlerFn(this, this.showEditor, source, index) }, _('Edit')), ' ',
						E('button', { class: 'btn cbi-button-negative', click: ui.createHandlerFn(this, function() {
							if (!confirm(_('Delete this target? Existing domains and Rule-Bot state will be kept.'))) return;
							this.settings.sources.splice(index, 1);
							return this.saveAll();
						}) }, _('Delete'))
					])
				]));
			} else {
				const checkbox = E('input', { type: 'checkbox' });
				checkbox.checked = !!source.enabled;
				checkbox.addEventListener('change', function() { source.enabled = checkbox.checked; });
				const preferTLS = E('input', { type: 'checkbox' });
				preferTLS.checked = !!source.prefer_tls;
				preferTLS.addEventListener('change', function() { source.prefer_tls = preferTLS.checked; });
				const actions = [ E('button', { class: 'btn cbi-button-action', disabled: !source.enabled, click: ui.createHandlerFn(this, this.probe, source.id) }, _('Test connection')) ];
				if (source.type === 'nikki') {
					actions.push(' ');
					actions.push(E('button', { class: 'btn', click: ui.createHandlerFn(this, this.showNikkiTLS, source, index) }, _('Nikki TLS settings')));
				}
				automatic.push(E('tr', { class: 'tr' }, [
					E('td', { class: 'td' }, checkbox),
					E('td', { class: 'td' }, source.name),
					E('td', { class: 'td' }, adapter.available ? (adapter.url || '-') : (adapter.error || _('Waiting'))),
					E('td', { class: 'td' }, source.type === 'nikki' ? [ preferTLS, ' ', _('Use Nikki TLS controller') ] : '-'),
					E('td', { class: 'td' }, actions)
				]));
			}
		}, this);
		return E('div', {}, [
			E('h2', {}, 'Rule-Bot Client - ' + _('Listening targets')),
			E('p', {}, _('OpenClash, Nikki, and multiple manual controllers may be enabled in any combination.')),
			E('h3', {}, _('Automatic adapters')),
			E('div', { class: 'table' }, [ E('tr', { class: 'tr table-titles' }, [ E('th', { class: 'th' }, _('Enabled')), E('th', { class: 'th' }, _('Adapter')), E('th', { class: 'th' }, _('Discovery')), E('th', { class: 'th' }, 'TLS'), E('th', { class: 'th' }, _('Action')) ]) ].concat(automatic)),
			E('h3', {}, _('Manual Mihomo controllers')),
			E('div', { class: 'table' }, [ E('tr', { class: 'tr table-titles' }, [ E('th', { class: 'th' }, _('Enabled')), E('th', { class: 'th' }, _('Display name')), E('th', { class: 'th' }, _('Controller URL')), E('th', { class: 'th' }, 'Secret'), E('th', { class: 'th' }, _('Action')) ]) ].concat(manual.length ? manual : [ E('tr', { class: 'tr' }, E('td', { class: 'td', colspan: 5 }, _('No manual targets'))) ])),
			E('div', { class: 'cbi-page-actions' }, [
				E('button', { class: 'btn cbi-button-add', click: ui.createHandlerFn(this, this.showEditor, null, null) }, _('Add target')), ' ',
				E('button', { class: 'btn cbi-button-positive important', click: ui.createHandlerFn(this, this.saveAll) }, _('Save and apply'))
			])
		]);
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

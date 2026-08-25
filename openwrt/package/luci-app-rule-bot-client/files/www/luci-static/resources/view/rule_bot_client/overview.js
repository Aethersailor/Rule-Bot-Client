'use strict';
'require view';
'require poll';
'require ui';
'require rule_bot_client.api as api';

function statusTable(status) {
	const runtime = status.runtime || {};
	const instances = runtime.instances || {};
	const serviceEnabled = !status.config || status.config.enabled !== false;
	const displayNames = {};
	((status.config && status.config.sources) || []).forEach(function(source) { displayNames[source.id] = source.name; });
	const rows = Object.keys(instances).sort().map(function(id) {
		const item = instances[id];
		return E('tr', { class: 'tr' }, [
			E('td', { class: 'td' }, displayNames[id] || item.name || id),
			E('td', { class: 'td' }, item.connected ? _('Connected') : _('Disconnected')),
			E('td', { class: 'td' }, String(item.captured_events || 0)),
			E('td', { class: 'td' }, item.last_event_at || '-'),
			E('td', { class: 'td' }, item.recent_error ? api.detailNode(_('Error recorded'), item.recent_error) : '-')
		]);
	});
	return E('div', {}, [
		E('h3', {}, _('Service status')),
		E('div', { class: 'cbi-section-node' }, [
			E('p', {}, [ E('strong', {}, _('Service master switch') + ': '), serviceEnabled ? _('Enabled') : _('Disabled') ]),
			E('p', {}, [ E('strong', {}, _('Service status') + ': '), status.service === 'running' ? _('Running') : _('Stopped') ]),
			E('p', {}, [ E('strong', {}, _('Storage') + ': '), storageValue(status) ]),
			E('p', {}, [ E('strong', {}, _('Output') + ': '), status.output && status.output.exists ? String(status.output.bytes || 0) + ' ' + _('bytes') : '-' ])
		]),
		E('div', { class: 'table' }, [
			E('tr', { class: 'tr table-titles' }, [ E('th', { class: 'th' }, _('Instance')), E('th', { class: 'th' }, _('State')), E('th', { class: 'th' }, _('Events')), E('th', { class: 'th' }, _('Last event')), E('th', { class: 'th' }, _('Recent error')) ])
		].concat(rows.length ? rows : [ E('tr', { class: 'tr' }, E('td', { class: 'td', colspan: 5 }, _('No runtime status yet'))) ]))
	]);
}

function serviceControls(status) {
	const serviceEnabled = !status.config || status.config.enabled !== false;
	const toggle = E('input', { type: 'checkbox', class: 'cbi-input-checkbox' });
	const reload = E('button', { class: 'btn cbi-button-action', click: function() { return api.service('reload').then(function() { location.reload(); }).catch(api.notifyError); } }, _('Reload'));
	const restart = E('button', { class: 'btn cbi-button-positive', click: function() { return api.service('restart').then(function() { location.reload(); }).catch(api.notifyError); } }, _('Restart'));
	toggle.checked = serviceEnabled;
	reload.disabled = !serviceEnabled;
	restart.disabled = !serviceEnabled;
	toggle.addEventListener('change', function() {
		const nextEnabled = toggle.checked;
		if (!status.config) {
			toggle.checked = serviceEnabled;
			return;
		}
		const settings = Object.assign({}, status.config, { enabled: nextEnabled });
		toggle.disabled = true;
		return api.save(settings).then(function() {
			ui.addNotification(null, E('p', {}, nextEnabled ? _('Service enabled') : _('Service disabled')), 'info');
			location.reload();
		}).catch(function(error) {
			toggle.checked = serviceEnabled;
			toggle.disabled = false;
			api.notifyError(error);
		});
	});
	return E('div', {}, [
		E('div', { class: 'cbi-section' }, [
			E('div', { class: 'cbi-value' }, [
				E('label', { class: 'cbi-value-title' }, _('Service master switch')),
				E('div', { class: 'cbi-value-field' }, [
					toggle,
					E('div', { class: 'cbi-value-description' }, _('Turning this off saves the setting and stops the service. Turning it on validates the configuration and starts the service; the choice persists across reboot.'))
				])
			])
		]),
		E('div', { class: 'cbi-page-actions' }, [
			reload,
			' ',
			restart
		])
	]);
}

function pageContent(status) {
	return [
		E('h2', {}, _('Rule-Bot Client') + ' - ' + _('Overview')),
		statusTable(status),
		serviceControls(status)
	];
}

function storageValue(status) {
	if (status.storage && status.storage.path)
		return status.storage.path;
	const mode = status.config && status.config.storage && status.config.storage.mode;
	const labels = {
		persistent: _('Persistent storage'),
		temporary: _('Temporary storage'),
		external: _('External storage')
	};
	return labels[mode] || '-';
}

return view.extend({
	load: function() { return api.status(); },
	render: function(status) {
		const container = E('div', {}, pageContent(status));
		poll.add(function() {
			return api.status().then(function(fresh) {
				container.replaceChildren.apply(container, pageContent(fresh));
			}).catch(api.notifyError);
		}, 5);
		return container;
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

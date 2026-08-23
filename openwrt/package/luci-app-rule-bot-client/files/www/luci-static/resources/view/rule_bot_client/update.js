'use strict';
'require view';
'require ui';
'require rule_bot_client.api as api';

function value(value, fallback) {
	return value === undefined || value === null || value === '' ? fallback : String(value);
}

function stateLabel(state) {
	const labels = {
		idle: _('Not checked'), checking: _('Checking'), available: _('Update available'),
		up_to_date: _('Up to date'), downloading: _('Downloading'), installing: _('Installing'),
		rolling_back: _('Rolling back'), rolled_back: _('Rolled back'), completed: _('Completed'),
		failed: _('Failed')
	};
	return labels[state] || value(state, _('Unknown'));
}

return view.extend({
	load: function() { return Promise.all([ api.config(), api.updateStatus() ]); },
	render: function(data) {
		const settings = data[0];
		const status = data[1] || { state: 'idle' };
		const info = status.info || {};
		const automatic = E('input', { type: 'checkbox', class: 'cbi-input-checkbox' });
		automatic.checked = settings.auto_update === true;
		automatic.addEventListener('change', function() {
			automatic.disabled = true;
			api.updateConfig(automatic.checked).then(function() {
				location.reload();
			}).catch(function(error) {
				automatic.disabled = false;
				api.notifyError(error);
			});
		});

		const check = E('button', { class: 'btn cbi-button-action' }, _('Check for updates'));
		check.addEventListener('click', function() {
			check.disabled = true;
			api.updateCheck().then(function() { location.reload(); }).catch(function(error) {
				check.disabled = false;
				api.notifyError(error);
			});
		});

		const install = E('button', { class: 'btn cbi-button-positive', disabled: info.available !== true }, _('Install update'));
		install.addEventListener('click', function() {
			ui.showModal(_('Install update'), [
				E('p', {}, _('The service may restart during installation. Configuration and collected data are preserved.')),
				E('div', { class: 'right' }, [
					E('button', { class: 'btn', click: ui.hideModal }, _('Cancel')), ' ',
					E('button', { class: 'btn cbi-button-positive', click: function() {
						api.updateStart().then(function() { ui.hideModal(); location.reload(); }).catch(function(error) {
							ui.hideModal(); api.notifyError(error);
						});
					} }, _('Install update'))
				])
			]);
		});

		const rows = [
			[ _('Update status'), stateLabel(status.state) ],
			[ _('Current version'), value(info.current_version, _('Unavailable')) ],
			[ _('Latest version'), value(info.latest_version, _('Not checked')) ],
			[ _('Package manager'), value(info.package_manager, _('Unavailable')) ],
			[ _('Architecture'), value(info.architecture, _('Unavailable')) ],
			[ _('Package size'), info.size ? String(info.size) + ' B' : _('Unavailable') ],
			[ _('Last checked'), status.updated_at ? new Date(status.updated_at).toLocaleString() : _('Not checked') ]
		];
		const table = E('table', { class: 'table' }, rows.map(function(row) {
			return E('tr', { class: 'tr' }, [ E('td', { class: 'td left', width: '35%' }, row[0]), E('td', { class: 'td left' }, row[1]) ]);
		}));
		const notices = [];
		if (info.compatibility_warning)
			notices.push(E('div', { class: 'alert-message warning' }, info.compatibility_warning));
		if (status.error)
			notices.push(api.detailNode(_('Technical details'), status.error));

		return E('div', {}, [
			E('h2', {}, _('Rule-Bot Client') + ' - ' + _('Software update')),
			E('p', {}, _('Updates are checked against the latest stable GitHub Release. No OpenWrt package feed is used.')),
			E('label', { class: 'cbi-value' }, [
				E('span', { class: 'cbi-value-title' }, _('Automatic updates')), E('div', { class: 'cbi-value-field' }, automatic)
			]),
			table,
			...notices,
			E('div', { class: 'cbi-page-actions' }, [ check, ' ', install, ' ', E('button', { class: 'btn', click: function() { location.reload(); } }, _('Refresh')) ])
		]);
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

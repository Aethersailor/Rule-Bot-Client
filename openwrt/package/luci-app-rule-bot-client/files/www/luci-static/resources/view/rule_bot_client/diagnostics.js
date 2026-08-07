'use strict';
'require view';
'require rule_bot_client.api as api';

return view.extend({
	load: function() { return Promise.all([ api.logs(), api.status(), api.upgrade() ]); },
	render: function(data) {
		const logs = data[0];
		const status = data[1];
		const upgrade = data[2];
		return E('div', {}, [
			E('h2', {}, _('Rule-Bot Client') + ' - ' + _('Logs and diagnostics')),
			E('h3', {}, _('Sanitized status')),
			E('pre', { style: 'max-height: 35vh; overflow: auto' }, JSON.stringify(status, null, 2)),
			E('h3', {}, _('Recent service logs')),
			E('pre', { style: 'max-height: 35vh; overflow: auto' }, logs.lines || _('No logs')),
			E('h3', {}, _('Upgrade identity')),
			E('pre', {}, JSON.stringify({ package_manager: upgrade.package_manager, architecture: upgrade.architecture, keep_complete: upgrade.complete }, null, 2)),
			E('div', { class: 'cbi-page-actions' }, E('button', { class: 'btn cbi-button-action', click: function() { location.reload(); } }, _('Refresh')))
		]);
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

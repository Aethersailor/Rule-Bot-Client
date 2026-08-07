'use strict';
'require view';
'require ui';
'require rule_bot_client.api as api';

return view.extend({
	load: function() { return api.domains('', 0, 200); },
	renderRows: function(result) {
		return E('pre', { style: 'max-height: 55vh; overflow: auto' }, (result.items || []).join('\n') || _('No collected domains'));
	},
	render: function(result) {
		const query = E('input', { class: 'cbi-input-text', placeholder: _('Search') });
		const output = E('div', {}, this.renderRows(result));
		const search = ui.createHandlerFn(this, function() {
			return api.domains(query.value, 0, 200).then(function(fresh) { output.replaceChildren(this.renderRows(fresh)); }.bind(this)).catch(api.notifyError);
		});
		const download = function() {
			return api.exportDomains().then(function(data) { api.download(data.filename, data.content, 'text/plain;charset=utf-8'); }).catch(api.notifyError);
		};
		const clear = function() {
			if (!confirm(_('Clear domains and Rule-Bot state together? This cannot be undone.'))) return;
			return api.clear().then(function() { location.reload(); }).catch(api.notifyError);
		};
		return E('div', {}, [
			E('h2', {}, _('Rule-Bot Client') + ' - ' + _('Local results')),
			E('p', {}, _('Search is bounded and reads only the fixed active output path.')),
			E('div', {}, [ query, ' ', E('button', { class: 'btn cbi-button-action', click: search }, _('Search')) ]),
			output,
			E('div', { class: 'cbi-page-actions' }, [
				E('button', { class: 'btn cbi-button-action', click: download }, _('Download')), ' ',
				E('button', { class: 'btn cbi-button-negative', click: clear }, _('Clear safely'))
			])
		]);
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

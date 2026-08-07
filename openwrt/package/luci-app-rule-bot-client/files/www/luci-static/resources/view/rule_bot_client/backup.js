'use strict';
'require view';
'require ui';
'require rule_bot_client.api as api';

function decodeBase64(value) {
	const raw = atob(value);
	const bytes = new Uint8Array(raw.length);
	for (let index = 0; index < raw.length; index++) bytes[index] = raw.charCodeAt(index);
	return bytes;
}

return view.extend({
	load: function() { return api.upgrade(); },
	render: function(upgrade) {
		const file = E('input', { type: 'file', accept: '.gz,.tgz,.tar.gz' });
		const backup = function() {
			ui.showModal(_('Create backup'), [ E('p', { class: 'spinning' }, _('Create backup') + '...') ]);
			return api.backup().then(function(data) {
				api.download(data.filename, decodeBase64(data.archive), 'application/gzip');
				ui.hideModal();
			}).catch(function(error) { ui.hideModal(); api.notifyError(error); });
		};
		const restore = function() {
			if (!file.files || !file.files[0]) { api.notifyError(new Error(_('Choose a backup archive first.'))); return; }
			if (!confirm(_('Restore this Rule-Bot Client backup and reload the service?'))) return;
			const reader = new FileReader();
			reader.onload = function() {
				const base64 = String(reader.result).split(',', 2)[1] || '';
				api.restore(base64).then(function() { location.reload(); }).catch(api.notifyError);
			};
			reader.readAsDataURL(file.files[0]);
		};
		return E('div', {}, [
			E('h2', {}, _('Rule-Bot Client') + ' - ' + _('Backup and restore')),
			E('p', {}, _('Backups include UCI, credentials, certificates, exclusion list, persistent data, Rule-Bot state, and recovery script. Runtime-generated config, adapter secrets, and status are excluded.')),
			E('p', {}, [ E('strong', {}, _('sysupgrade keep.d') + ': '), upgrade.complete ? _('complete') : _('incomplete') ]),
			E('pre', {}, upgrade.keep_list || ''),
			E('div', { class: 'cbi-page-actions' }, [
				E('button', { class: 'btn cbi-button-action', click: backup }, _('Create backup')), ' ',
				file, ' ', E('button', { class: 'btn cbi-button-positive', click: restore }, _('Restore backup'))
			]),
			E('p', {}, _('After sysupgrade, reinstall the single matching package or run /etc/rule-bot-client/recover.sh. The script detects apk/opkg and architecture; it never runs automatically.'))
		]);
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

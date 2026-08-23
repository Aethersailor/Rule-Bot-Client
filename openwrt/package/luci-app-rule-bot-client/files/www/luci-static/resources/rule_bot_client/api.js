'use strict';
'require rpc';
'require ui';
'require baseclass';

const calls = {
	config: rpc.declare({ object: 'luci.rule_bot_client', method: 'config' }),
	status: rpc.declare({ object: 'luci.rule_bot_client', method: 'status' }),
	probe: rpc.declare({ object: 'luci.rule_bot_client', method: 'probe', params: [ 'id' ] }),
	domains: rpc.declare({ object: 'luci.rule_bot_client', method: 'domains', params: [ 'query', 'offset', 'limit' ] }),
	export: rpc.declare({ object: 'luci.rule_bot_client', method: 'export' }),
	logs: rpc.declare({ object: 'luci.rule_bot_client', method: 'logs' }),
	backup: rpc.declare({ object: 'luci.rule_bot_client', method: 'backup' }),
	upgrade: rpc.declare({ object: 'luci.rule_bot_client', method: 'upgrade' }),
	updateStatus: rpc.declare({ object: 'luci.rule_bot_client', method: 'update_status' }),
	updateCheck: rpc.declare({ object: 'luci.rule_bot_client', method: 'update_check' }),
	updateConfig: rpc.declare({ object: 'luci.rule_bot_client', method: 'update_config', params: [ 'enabled' ] }),
	updateStart: rpc.declare({ object: 'luci.rule_bot_client', method: 'update_start' }),
	save: rpc.declare({ object: 'luci.rule_bot_client', method: 'save', params: [ 'payload' ] }),
	clear: rpc.declare({ object: 'luci.rule_bot_client', method: 'clear', params: [ 'confirm' ] }),
	restore: rpc.declare({ object: 'luci.rule_bot_client', method: 'restore', params: [ 'archive' ] }),
	service: rpc.declare({ object: 'luci.rule_bot_client', method: 'service', params: [ 'action' ] })
};

function checked(promise) {
	return Promise.resolve(promise).then(function(result) {
		if (result && result.ok === false) {
			const error = new Error(_('Operation failed'));
			error.detail = result.error || _('Rule-Bot Client backend error');
			error.code = result.error_code || 'backend_error';
			throw error;
		}
		return result;
	});
}

function errorNodes(error) {
	const summary = (error && error.message) || _('Operation failed');
	const detail = error && error.detail;
	const nodes = [ E('p', {}, summary) ];
	if (detail && detail !== summary)
		nodes.push(detailNode(_('Technical details'), detail));
	return nodes;
}

function detailNode(summary, detail) {
	return E('details', {}, [
		E('summary', {}, summary),
		E('pre', { style: 'white-space: pre-wrap' }, String(detail))
	]);
}

function notifyError(error) {
	ui.addNotification(null, E('div', {}, errorNodes(error)), 'error');
}

function download(filename, content, mime) {
	const blob = content instanceof Uint8Array ? new Blob([ content ], { type: mime }) : new Blob([ content ], { type: mime });
	const url = URL.createObjectURL(blob);
	const anchor = E('a', { href: url, download: filename });
	document.body.appendChild(anchor);
	anchor.click();
	anchor.remove();
	setTimeout(function() { URL.revokeObjectURL(url); }, 1000);
}

calls.configEdit = rpc.declare({ object: 'luci.rule_bot_client', method: 'config_edit' });

return baseclass.extend({
	notifyError: notifyError,
	errorNodes: errorNodes,
	detailNode: detailNode,
	download: download,
	config: function() { return checked(calls.config()); },
	configEdit: function() { return checked(calls.configEdit()); },
	status: function() { return checked(calls.status()); },
	probe: function(id) { return checked(calls.probe(id)); },
	domains: function(query, offset, limit) { return checked(calls.domains(query || '', offset || 0, limit || 100)); },
	exportDomains: function() { return checked(calls.export()); },
	logs: function() { return checked(calls.logs()); },
	backup: function() { return checked(calls.backup()); },
	upgrade: function() { return checked(calls.upgrade()); },
	updateStatus: function() { return checked(calls.updateStatus()); },
	updateCheck: function() { return checked(calls.updateCheck()); },
	updateConfig: function(enabled) { return checked(calls.updateConfig(enabled)); },
	updateStart: function() { return checked(calls.updateStart()); },
	save: function(payload) { return checked(calls.save(payload)); },
	clear: function() { return checked(calls.clear('CLEAR')); },
	restore: function(archive) { return checked(calls.restore(archive)); },
	service: function(action) { return checked(calls.service(action)); }
});

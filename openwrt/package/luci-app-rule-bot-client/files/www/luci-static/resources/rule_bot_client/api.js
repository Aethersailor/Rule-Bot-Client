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
	save: rpc.declare({ object: 'luci.rule_bot_client', method: 'save', params: [ 'payload' ] }),
	clear: rpc.declare({ object: 'luci.rule_bot_client', method: 'clear', params: [ 'confirm' ] }),
	restore: rpc.declare({ object: 'luci.rule_bot_client', method: 'restore', params: [ 'archive' ] }),
	service: rpc.declare({ object: 'luci.rule_bot_client', method: 'service', params: [ 'action' ] })
};

function checked(promise) {
	return Promise.resolve(promise).then(function(result) {
		if (result && result.ok === false)
			throw new Error(result.error || 'Rule-Bot Client backend error');
		return result;
	});
}

function notifyError(error) {
	ui.addNotification(null, E('p', {}, error.message || String(error)), 'error');
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

return baseclass.extend({
	notifyError: notifyError,
	download: download,
	config: function() { return checked(calls.config()); },
	status: function() { return checked(calls.status()); },
	probe: function(id) { return checked(calls.probe(id)); },
	domains: function(query, offset, limit) { return checked(calls.domains(query || '', offset || 0, limit || 100)); },
	exportDomains: function() { return checked(calls.export()); },
	logs: function() { return checked(calls.logs()); },
	backup: function() { return checked(calls.backup()); },
	upgrade: function() { return checked(calls.upgrade()); },
	save: function(payload) { return checked(calls.save(payload)); },
	clear: function() { return checked(calls.clear('CLEAR')); },
	restore: function(archive) { return checked(calls.restore(archive)); },
	service: function(action) { return checked(calls.service(action)); }
});

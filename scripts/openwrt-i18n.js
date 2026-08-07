#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');

function decodeLiteral(value, quote) {
	return JSON.parse(quote === '"' ? `"${value}"` : `"${value.replace(/"/g, '\\"').replace(/\\'/g, "'")}"`);
}

function lineNumber(source, offset) {
	return source.slice(0, offset).split('\n').length;
}

function collect(root) {
	const references = new Map();
	const add = (key, reference) => {
		if (!key)
			return;
		if (!references.has(key))
			references.set(key, new Set());
		references.get(key).add(reference);
	};
	const resources = path.join(root, 'files/www/luci-static/resources');
	const jsFiles = [];
	const walk = (directory) => {
		for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
			const full = path.join(directory, entry.name);
			if (entry.isDirectory())
				walk(full);
			else if (entry.name.endsWith('.js'))
				jsFiles.push(full);
		}
	};
	walk(resources);
	for (const file of jsFiles.sort()) {
		const source = fs.readFileSync(file, 'utf8');
		for (const expression of [ /_\(\s*'((?:\\.|[^'\\])*)'\s*\)/g, /_\(\s*"((?:\\.|[^"\\])*)"\s*\)/g ]) {
			for (const match of source.matchAll(expression)) {
				const quote = match[0].includes("_('") ? "'" : '"';
				add(decodeLiteral(match[1], quote), `${path.relative(root, file).replace(/\\/g, '/')}:${lineNumber(source, match.index)}`);
			}
		}
		for (const literal of [ 'Rule-Bot Client - ', 'sysupgrade keep.d: ' ]) {
			if (source.includes(`'${literal}'`) || source.includes(`"${literal}"`))
				throw new Error(`${path.relative(root, file)} contains untranslated visible literal ${JSON.stringify(literal)}`);
		}
	}
	const menuFile = path.join(root, 'files/usr/share/luci/menu.d/luci-app-rule-bot-client.json');
	const menu = JSON.parse(fs.readFileSync(menuFile, 'utf8'));
	for (const node of Object.values(menu)) {
		if (node.title)
			add(node.title, `${path.relative(root, menuFile).replace(/\\/g, '/')}:1`);
	}
	return references;
}

function parsePO(poPath) {
	const translations = new Map();
	let entry = null;
	let field = null;
	const flush = () => {
		if (entry && entry.msgid !== null)
			translations.set(entry.msgid, entry.fuzzy ? '' : entry.msgstr);
		entry = null;
		field = null;
	};
	for (const line of fs.readFileSync(poPath, 'utf8').split(/\r?\n/)) {
		if (line.startsWith('#,') && line.split(',').some((flag) => flag.trim() === 'fuzzy')) {
			if (entry && entry.msgid !== null)
				flush();
			if (!entry)
				entry = { msgid: null, msgstr: '', fuzzy: false };
			entry.fuzzy = true;
			continue;
		}
		let match = line.match(/^(msgid|msgstr) ("(?:\\.|[^"\\])*")$/);
		if (match) {
			if (match[1] === 'msgid' && entry && entry.msgid !== null)
				flush();
			if (!entry)
				entry = { msgid: null, msgstr: '', fuzzy: false };
			field = match[1];
			entry[field] = JSON.parse(match[2]);
			continue;
		}
		match = line.match(/^("(?:\\.|[^"\\])*")$/);
		if (match && entry && field)
			entry[field] += JSON.parse(match[1]);
	}
	flush();
	return translations;
}

function quote(value) {
	return JSON.stringify(value);
}

function renderPOT(references) {
	const lines = [
		'msgid ""',
		'msgstr ""',
		'"Project-Id-Version: Rule-Bot Client\\n"',
		'"MIME-Version: 1.0\\n"',
		'"Content-Type: text/plain; charset=UTF-8\\n"',
		'"Content-Transfer-Encoding: 8bit\\n"',
		''
	];
	for (const key of [...references.keys()].sort((a, b) => a.localeCompare(b, 'en'))) {
		lines.push(`#: ${[...references.get(key)].sort().join(' ')}`);
		lines.push(`msgid ${quote(key)}`);
		lines.push('msgstr ""');
		lines.push('');
	}
	return lines.join('\n');
}

function main() {
	const [ mode, root, poPath ] = process.argv.slice(2);
	if (!mode || !root || (mode === 'check' && !poPath))
		throw new Error('usage: openwrt-i18n.js <check|pot> <package-root> [po-file]');
	const references = collect(root);
	if (mode === 'pot') {
		process.stdout.write(renderPOT(references));
		return;
	}
	if (mode !== 'check')
		throw new Error(`unsupported mode ${JSON.stringify(mode)}`);
	const translations = parsePO(poPath);
	const missing = [...references.keys()].filter((key) => !translations.get(key));
	const stale = [...translations.keys()].filter((key) => key && !references.has(key));
	if (missing.length)
		throw new Error(`missing Simplified Chinese translations: ${missing.map(JSON.stringify).join(', ')}`);
	if (stale.length)
		throw new Error(`stale Simplified Chinese translations: ${stale.map(JSON.stringify).join(', ')}`);
	if (references.size < 100)
		throw new Error(`expected at least 100 native LuCI translation keys, found ${references.size}`);
	process.stdout.write(`verified ${references.size} Simplified Chinese LuCI translation keys\n`);
}

main();

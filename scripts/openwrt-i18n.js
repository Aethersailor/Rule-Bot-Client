#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');

function decodeLiteral(value) {
	// Consume existing escape sequences before escaping bare JSON quotes.
	const json = value.replace(/\\.|"/g, (token) => {
		if (token === "\\'")
			return "'";
		if (token === '"')
			return '\\"';
		return token;
	});
	return JSON.parse(`"${json}"`);
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
				add(decodeLiteral(match[1]), `${path.relative(root, file).replace(/\\/g, '/')}:${lineNumber(source, match.index)}`);
			}
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
	const [ mode, root ] = process.argv.slice(2);
	if (mode !== 'pot' || !root)
		throw new Error('usage: openwrt-i18n.js pot <package-root>');
	process.stdout.write(renderPOT(collect(root)));
}

main();

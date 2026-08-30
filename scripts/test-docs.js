#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');

const repository = path.resolve(__dirname, '..');
const failures = [];
const forbiddenCredentialKeys = new Set([
	'secret_file',
	'secret_env',
	'token_file',
	'token_env'
]);

function fail(message) {
	failures.push(message);
}

function read(relativePath) {
	return fs.readFileSync(path.join(repository, relativePath), 'utf8');
}

function parseJSON(contents, label) {
	try {
		return JSON.parse(contents);
	} catch (error) {
		fail(`${label}: invalid JSON: ${error.message}`);
		return null;
	}
}

function findForbiddenCredentialKeys(value, location = '$', found = []) {
	if (Array.isArray(value)) {
		value.forEach((item, index) => findForbiddenCredentialKeys(item, `${location}[${index}]`, found));
		return found;
	}
	if (!value || typeof value !== 'object')
		return found;
	for (const [key, child] of Object.entries(value)) {
		if (forbiddenCredentialKeys.has(key))
			found.push(`${location}.${key}`);
		findForbiddenCredentialKeys(child, `${location}.${key}`, found);
	}
	return found;
}

function nonemptyString(value) {
	return typeof value === 'string' && value.trim() !== '';
}

function assertBundledConfig(relativePath) {
	const config = parseJSON(read(relativePath), relativePath);
	if (!config)
		return;

	const forbidden = findForbiddenCredentialKeys(config);
	if (forbidden.length)
		fail(`${relativePath}: bundled examples must not use file or environment credentials (${forbidden.join(', ')})`);

	if (!Array.isArray(config.instances) || config.instances.length === 0) {
		fail(`${relativePath}: instances must contain at least one controller`);
	} else {
		config.instances.forEach((instance, index) => {
			if (!instance || typeof instance !== 'object' || !nonemptyString(instance.secret))
				fail(`${relativePath}: instances[${index}].secret must be a non-empty inline value`);
		});
	}

	if (!config.rule_bot || typeof config.rule_bot !== 'object') {
		fail(`${relativePath}: rule_bot must be present in the ordinary-user example`);
	} else {
		if (!nonemptyString(config.rule_bot.token))
			fail(`${relativePath}: rule_bot.token must be a non-empty inline value`);
		if (config.rule_bot.send_existing !== false)
			fail(`${relativePath}: rule_bot.send_existing must remain false by default`);
	}
	if (relativePath === 'deploy/windows/config.json' && config.auto_update !== true)
		fail(`${relativePath}: Windows portable automatic updates must be enabled in its example`);
	if (relativePath !== 'deploy/windows/config.json' && config.auto_update !== false)
		fail(`${relativePath}: non-Windows examples must not enable in-process automatic updates`);
}

function markdownAnalysis(relativePath) {
	const source = read(relativePath);
	const lines = source.split(/\r?\n/);
	const shellBlocks = [];
	const proseLines = [];
	let fence = null;

	for (let index = 0; index < lines.length; index++) {
		const line = lines[index];
		if (fence) {
			if (new RegExp(`^ {0,3}${fence.marker[0]}{${fence.marker.length},}\\s*$`).test(line)) {
				if ([ 'sh', 'bash', 'shell' ].includes(fence.language)) {
					shellBlocks.push({
						body: fence.body.join('\n'),
						line: fence.line
					});
				}
				fence = null;
			} else {
				fence.body.push(line);
			}
			continue;
		}

		const opening = line.match(/^ {0,3}(`{3,}|~{3,})\s*([^\s`]*)?.*$/);
		if (opening) {
			fence = {
				marker: opening[1],
				language: (opening[2] || '').toLowerCase(),
				body: [],
				line: index + 1
			};
			continue;
		}

		proseLines.push(line);
	}

	if (fence)
		fail(`${relativePath}:${fence.line}: unclosed fenced code block`);

	return { source, shellBlocks, prose: proseLines.join('\n') };
}

function markdownTargets(prose) {
	const targets = [];
	const inline = /!?\[[^\]]*\]\(([^)]+)\)/g;
	const references = /^\s*\[[^\]]+\]:\s*(\S+)/gm;
	for (const match of prose.matchAll(inline))
		targets.push(match[1]);
	for (const match of prose.matchAll(references))
		targets.push(match[1]);
	return targets;
}

function localLinkPath(rawTarget) {
	let target = rawTarget.trim();
	if (target.startsWith('<')) {
		const closing = target.indexOf('>');
		if (closing !== -1)
			target = target.slice(1, closing);
	} else {
		target = target.split(/\s+["']/u, 1)[0];
	}
	if (!target || target.startsWith('#') || target.startsWith('//') || /^[a-z][a-z0-9+.-]*:/i.test(target))
		return null;
	target = target.split('#', 1)[0].split('?', 1)[0];
	if (!target)
		return null;
	try {
		return decodeURIComponent(target);
	} catch (error) {
		fail(`invalid percent encoding in Markdown link ${JSON.stringify(rawTarget)}`);
		return null;
	}
}

function assertLocalLinks(relativePath, analysis) {
	for (const rawTarget of markdownTargets(analysis.prose)) {
		const target = localLinkPath(rawTarget);
		if (!target)
			continue;
		const resolved = target.startsWith('/')
			? path.join(repository, target.slice(1))
			: path.resolve(repository, path.dirname(relativePath), target);
		if (!fs.existsSync(resolved))
			fail(`${relativePath}: local Markdown link does not exist: ${target}`);
	}
}

function requireMatch(contents, pattern, message) {
	if (!pattern.test(contents))
		fail(message);
}

function assertBackupExamples(relativePath, analysis) {
	for (const block of analysis.shellBlocks) {
		const commands = block.body.replace(/\\\r?\n[ \t]*/g, ' ');
		if (!/\btar\b/i.test(commands) || !/backup/i.test(commands))
			continue;

		const label = `${relativePath}:${block.line}`;
		if (!/(?:install\s+-d\b[^\n;]*-m\s+0700|chmod\s+0700\b)/i.test(commands))
			fail(`${label}: backup examples must create or restrict their destination directory to mode 0700`);
		if (!/(?:install\b[^\n;]*-m\s+0600|chmod\s+0600\b)/i.test(commands))
			fail(`${label}: backup examples must restrict the completed archive to mode 0600`);
		if (/\b[A-Za-z_][A-Za-z0-9_]*backup[A-Za-z0-9_]*\s*=\s*["']?\/(?:etc\/rule-bot-client|var\/lib\/rule-bot-client|opt\/rule-bot-client)(?:\/|["']|\s|$)/i.test(commands))
			fail(`${label}: backup destination variables must not point inside a source directory`);

		for (const command of commands.split(/\r?\n/)) {
			if (!/\btar\b/i.test(command) || !/(?:^|\s)-[A-Za-z]*c[A-Za-z]*f[A-Za-z]*(?:\s|$)/.test(command))
				continue;
			const archive = command.match(/(?:^|\s)-[A-Za-z]*f[A-Za-z]*\s+(?:"([^"]+)"|'([^']+)'|(\S+))/);
			if (!archive) {
				fail(`${label}: unable to verify the backup archive destination`);
				continue;
			}
			const target = archive[1] || archive[2] || archive[3];
			if (/^\/(?:etc\/rule-bot-client|var\/lib\/rule-bot-client|opt\/rule-bot-client)(?:\/|$)/.test(target) || (!target.startsWith('/') && !target.startsWith('$')))
				fail(`${label}: backup archive must be outside every source directory to avoid self-inclusion`);
		}
	}
}

function assertDesignCredentialContract() {
	const designPath = 'docs/design-contract.md';
	const design = read(designPath);
	requireMatch(design,
		/Inline `secret` and `token` values are the default for ordinary deployments/i,
		`${designPath}: inline secret and token must be the ordinary-user default`);
	requireMatch(design,
		/`secret_file`, `token_file`, `secret_env`, and `token_env` remain advanced[\s\S]*Each credential accepts exactly one source;[\s\S]*conflicts are rejected instead of resolved by precedence/i,
		`${designPath}: file and environment credentials must remain advanced, mutually exclusive sources`);
	if (/Inline secrets are supported for compatibility[\s\S]{0,120}(?:files?|environment variables?) are preferred/i.test(design))
		fail(`${designPath}: stale file/environment-preferred credential guidance remains`);
}

function assertReleaseExampleContract() {
	const buildRelease = read('scripts/build-release.sh');
	const archiveFunction = buildRelease.match(/build_archive\(\)\s*\{([\s\S]*?)^\}/m);
	if (!archiveFunction) {
		fail('scripts/build-release.sh: build_archive function is missing');
	} else {
		requireMatch(archiveFunction[1], /\bcp\s+deploy\/linux\/config\.json\s+"\$root\/config\.example\.json"/,
			'scripts/build-release.sh: every Linux archive must include config.example.json');
		requireMatch(archiveFunction[1], /chmod\s+0600\s+"\$root\/config\.example\.json"/,
			'scripts/build-release.sh: archived config.example.json must use mode 0600');
		requireMatch(archiveFunction[1], /\bcp\b[^\n]*\bPRIVACY\.md\b[^\n]*\bSECURITY\.md\b[^\n]*"\$root\/"/,
			'scripts/build-release.sh: Linux archives must include linked privacy and security documents');
	}
	const releaseWorkflow = read('.github/workflows/release.yml');
	requireMatch(buildRelease, /build_windows_archive\(\)[\s\S]*deploy\/windows\/config\.json[\s\S]*windows-\$label/,
		'scripts/build-release.sh: Windows portable archives must use the Windows configuration and target identity');
	requireMatch(buildRelease, /client-update-manifest\.json/,
		'scripts/build-release.sh: release builds must publish the client update manifest');
	requireMatch(releaseWorkflow,
		/tar -tzf "\$archive"[\s\\]*"\$package\/config\.example\.json"/,
		'.github/workflows/release.yml: release validation must inspect config.example.json in every Linux archive');
	requireMatch(releaseWorkflow,
		/tar -tzf "\$archive"[\s\S]*"\$package\/PRIVACY\.md"[\s\\]*"\$package\/SECURITY\.md"/,
		'.github/workflows/release.yml: release validation must inspect linked privacy and security documents');
	requireMatch(releaseWorkflow,
		/tar -tvzf "\$archive" "\$package\/config\.example\.json"[\s\S]*-rw-------/,
		'.github/workflows/release.yml: release validation must verify config.example.json mode 0600');
}

function assertVisitorDeploymentGuidance() {
	const readme = read('README.md');
	requireMatch(readme,
		/`MATCH`[^\n]{0,80}兜底规则|兜底规则[^\n]{0,80}`MATCH`/,
		'README.md: MATCH must be explained as the Mihomo fallback rule');
	requireMatch(readme,
		/Mihomo 控制接口[\s\S]{0,160}HTTP 管理接口[\s\S]{0,160}`external-controller`[\s\S]{0,220}不是需要另行安装的程序[\s\S]{0,120}Controller/,
		'README.md: Mihomo control interface must be explained in plain language and mapped to Controller');
	requireMatch(readme,
		/Rule-Bot[^\n]{0,40}可选[\s\S]{0,160}本地收集不需要 Rule-Bot/,
		'README.md: Rule-Bot must be presented as optional for local collection');
	requireMatch(readme,
		/同一套网络通常只需部署一个 Rule-Bot Client[\s\S]{0,200}不需要在 OpenWrt 和 Linux 上各安装一份/,
		'README.md: visitors must be told to choose one client deployment instead of installing on both OpenWrt and Linux');
	requireMatch(readme,
		/一个客户端[\s\S]{0,160}多个正在运行的 Mihomo 内核/,
		'README.md: visitors must be told that one client can collect from multiple controllers');
	requireMatch(readme,
		/只有 OpenWrt 是唯一合适的常驻设备时[\s\S]{0,80}安装到 OpenWrt/,
		'README.md: OpenWrt must remain the fallback when no suitable always-on Linux or Docker host exists');
	requireMatch(readme,
		/OpenWrt 包[\s\S]{0,120}不属于 OpenWrt 官方软件源[\s\S]{0,300}软件包丢失后仍需重新安装/,
		'README.md: visitors must see the OpenWrt package retention warning before choosing that deployment');
	requireMatch(readme,
		/不要在 LuCI「软件包」页面直接上传 APK[\s\S]{0,180}`UNTRUSTED signature`[\s\S]{0,300}SHA-256/,
		'README.md: visitors must use the hash-verifying installer instead of direct unsigned APK upload');
	requireMatch(readme,
		/ImmortalWrt SNAPSHOT[\s\S]{0,220}OpenWrt 25\.12 APK 兼容构建[\s\S]{0,120}`apk` 校验/,
		'README.md: ImmortalWrt SNAPSHOT compatibility and dependency validation must be explicit');
}

function assertDeploymentContracts() {
	const packageScript = read('scripts/package-deb.sh');
	const systemdUnit = read('deploy/systemd/rule-bot-client.service');
	const updateService = read('deploy/systemd/rule-bot-client-update.service');
	const updateTimer = read('deploy/systemd/rule-bot-client-update.timer');
	const dockerfile = read('Dockerfile');
	const composePath = 'deploy/docker/compose.yaml';
	const compose = read(composePath);

	requireMatch(packageScript, /^\/etc\/rule-bot-client\/config\.json$/m,
		'scripts/package-deb.sh: config.json must remain a dpkg conffile');
	requireMatch(packageScript, /chown\s+root:rule-bot-client\s+\/etc\/rule-bot-client\/config\.json/,
		'scripts/package-deb.sh: config.json must be owned by root:rule-bot-client');
	requireMatch(packageScript, /chmod\s+0640\s+\/etc\/rule-bot-client\/config\.json/,
		'scripts/package-deb.sh: config.json must use mode 0640');
	requireMatch(packageScript, /systemctl\s+enable\s+--now\s+rule-bot-client-update\.timer/,
		'scripts/package-deb.sh: Debian package must enable its automatic update timer');
	requireMatch(updateService, /--update-apply/,
		'deploy/systemd/rule-bot-client-update.service: update service must invoke the built-in updater');
	requireMatch(updateTimer, /^OnCalendar=daily$/m,
		'deploy/systemd/rule-bot-client-update.timer: automatic updates must run daily');
	requireMatch(systemdUnit, /^User=rule-bot-client$/m,
		'deploy/systemd/rule-bot-client.service: service must run as rule-bot-client');
	requireMatch(systemdUnit, /^Group=rule-bot-client$/m,
		'deploy/systemd/rule-bot-client.service: service group must be rule-bot-client');

	requireMatch(dockerfile, /^USER\s+10001:10001$/m,
		'Dockerfile: runtime image must use UID/GID 10001:10001');
	requireMatch(compose, /^\s*user:\s*["']?10001:10001["']?\s*$/m,
		`${composePath}: service must run as UID/GID 10001:10001`);
	requireMatch(compose, /^\s*-\s+\/opt\/rule-bot-client:\/data\s*$/m,
		`${composePath}: expected the documented /opt/rule-bot-client:/data bind mount`);
	requireMatch(compose, /^\s*read_only:\s*true\s*$/m,
		`${composePath}: container root filesystem must remain read-only`);
	requireMatch(compose, /^\s*network_mode:\s*host\s*$/m,
		`${composePath}: host networking is required for a loopback Mihomo controller`);

}

const bundledConfigs = [
	'deploy/docker/config.json',
	'deploy/linux/config.json',
	'deploy/systemd/config.json',
	'deploy/windows/config.json'
];
bundledConfigs.forEach(assertBundledConfig);

const markdownFiles = [
	'README.md',
	'PRIVACY.md',
	'SECURITY.md',
	'docs/design-contract.md',
	'docs/telegram-announcement.md',
	'deploy/README.md'
];
const analyses = new Map(markdownFiles.map((relativePath) => [ relativePath, markdownAnalysis(relativePath) ]));
for (const relativePath of markdownFiles) {
	const analysis = analyses.get(relativePath);
	assertLocalLinks(relativePath, analysis);
	assertBackupExamples(relativePath, analysis);
}

assertDeploymentContracts();
assertDesignCredentialContract();
assertReleaseExampleContract();
assertVisitorDeploymentGuidance();

if (failures.length) {
	for (const failure of failures)
		console.error(`test-docs: ${failure}`);
	process.exit(1);
}

console.log(`test-docs: validated ${bundledConfigs.length} configs, ${markdownFiles.length} Markdown files, beginner-facing concepts, deployment permissions, design semantics, and release examples`);

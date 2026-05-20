/**
 * mm project — local agent project index.
 *
 * Hits meta-me-local-agent's REST surface (default http://localhost:3142,
 * override with $MM_LOCAL_AGENT_URL). No auth — the local agent is
 * tailnet/localhost-trust by design.
 *
 * Pairs with the agent's `project_index_query` tool: that's the same
 * machinery the agent reaches for; this gives you the same view from a
 * terminal.
 */
import { homedir } from 'node:os';
import { statSync } from 'node:fs';
import path from 'node:path';

type ProjectRow = {
	id: string;
	root_path: string;
	label: string;
	last_opened_at: number;
	created_at: number;
	exists?: boolean;
};

type OverviewEntry =
	| {
			kind: 'folder';
			path: string;
			summary: string;
			files_count: number;
			modified: number;
	  }
	| {
			kind: 'file';
			path: string;
			summary: string;
			exports: string | null;
			language: string | null;
			size: number;
			mtime: number;
	  };

type OverviewResponse = {
	scope_path: string;
	entries: OverviewEntry[];
	folders_refreshed: number;
	files_refreshed: number;
};

type DetailEntry = {
	path: string;
	summary: string;
	exports: string | null;
	kind: string;
	size: number;
	mtime: number;
	language: string | null;
	stale: boolean;
};

type DetailResponse = {
	entries: DetailEntry[];
	refreshed: number;
	skipped: number;
};

const BASE = (process.env.MM_LOCAL_AGENT_URL ?? 'http://localhost:3142').replace(/\/+$/, '');

function expandPath(p: string): string {
	if (p === '~' || p.startsWith('~/')) return path.join(homedir(), p.slice(2));
	return path.resolve(p);
}

function looksLikePath(s: string): boolean {
	return s === '~' || s.startsWith('/') || s.startsWith('./') || s.startsWith('../') || s.startsWith('~/');
}

async function api<T>(pathname: string, init?: RequestInit): Promise<T> {
	let res: Response;
	try {
		res = await fetch(BASE + pathname, init);
	} catch (e) {
		process.stderr.write(
			`Error: local agent not reachable at ${BASE}\n` +
				`Is it running? Set MM_LOCAL_AGENT_URL if it's on a different port.\n` +
				`Underlying: ${e instanceof Error ? e.message : String(e)}\n`,
		);
		process.exit(1);
	}
	if (!res.ok) {
		const body = await res.text().catch(() => '');
		process.stderr.write(`Error: HTTP ${res.status} on ${pathname}\n${body}\n`);
		process.exit(1);
	}
	return (await res.json()) as T;
}

async function fetchProjects(): Promise<ProjectRow[]> {
	const r = await api<{ projects: ProjectRow[] }>('/api/projects');
	return r.projects;
}

async function resolveProject(needle: string): Promise<ProjectRow | null> {
	const all = await fetchProjects();
	if (looksLikePath(needle)) {
		const root = expandPath(needle);
		return all.find((p) => p.root_path === root) ?? null;
	}
	const byLabel = all.find((p) => p.label === needle);
	if (byLabel) return byLabel;
	const abs = expandPath(needle);
	return all.find((p) => p.root_path === abs) ?? null;
}

function fail(msg: string): never {
	process.stderr.write(msg.endsWith('\n') ? msg : msg + '\n');
	process.exit(1);
}

function getFlag(args: string[], name: string): string | undefined {
	const i = args.indexOf(name);
	return i >= 0 && i + 1 < args.length ? args[i + 1] : undefined;
}

function hasFlag(args: string[], name: string): boolean {
	return args.includes(name);
}

export function printProjectHelp() {
	console.log(`mm project — local agent project index

Subcommands:
  list                            List registered projects.
  overview <name|path> [subpath]  Folder-level summaries (default first move).
  detail   <name|path> [subpath]  Per-file summaries under a folder.
                                  Flags: --search <q>  --limit <n>  --shallow
  add      <path> [label]         Register a folder as a project.
  rebuild  <name|path> [subpath]  Drop cached rows and re-summarise.
  help                            Show this help.

Talks to ${BASE} (override with MM_LOCAL_AGENT_URL).
No auth — the local agent is localhost/tailnet-trust by design.

Resolution:
  <name>   matched against project.label (exact)
  <path>   starts with /, ./, ../, ~, or ~/ — resolved absolute, matched against root_path

Examples:
  mm project list
  mm project overview joe-inc
  mm project overview ~/Documents/dev/joe-inc
  mm project detail joe-inc profile --search PhD --limit 5
  mm project add ~/Documents/dev/new-thing
  mm project rebuild knowledgebase-v1 src

Add --json to any command for parseable output.`);
}

async function cmdList(json: boolean): Promise<void> {
	const projects = await fetchProjects();
	if (json) {
		process.stdout.write(JSON.stringify(projects, null, 2) + '\n');
		return;
	}
	if (projects.length === 0) {
		console.log('(no projects) — use: mm project add <path>');
		return;
	}
	const labelWidth = Math.max(...projects.map((r) => r.label.length), 5);
	for (const r of projects) {
		const missing = r.exists === false ? '  (missing on disk)' : '';
		console.log(`${r.label.padEnd(labelWidth)}  ${r.root_path}${missing}`);
	}
}

async function cmdOverview(args: string[], json: boolean): Promise<void> {
	const needle = args[0];
	const subPath = args[1];
	if (!needle) fail('Usage: mm project overview <name|path> [subpath]');
	const proj = await resolveProject(needle);
	if (!proj) fail(`No project matches "${needle}". Try: mm project list`);
	const q = subPath ? `?path=${encodeURIComponent(subPath)}` : '';
	const t0 = Date.now();
	const res = await api<OverviewResponse>(`/api/projects/${proj.id}/overview${q}`);
	const dt = ((Date.now() - t0) / 1000).toFixed(1);
	if (json) {
		process.stdout.write(JSON.stringify({ project: proj, ...res }, null, 2) + '\n');
		return;
	}
	const folders = res.entries.filter((e) => e.kind === 'folder');
	const files = res.entries.filter((e) => e.kind === 'file');
	const drift = folders.reduce((acc, e) => acc + (e.kind === 'folder' ? e.modified : 0), 0);
	const headerBits = [
		proj.label + (subPath ? `/${subPath}` : ''),
		`${folders.length} folder${folders.length === 1 ? '' : 's'}`,
		`${files.length} file${files.length === 1 ? '' : 's'}`,
	];
	if (drift > 0) headerBits.push(`${drift} modified since last overview`);
	if (res.folders_refreshed > 0) headerBits.push(`${res.folders_refreshed} re-summarised`);
	console.log(`[${dt}s] ${headerBits.join(' · ')}\n`);
	for (const e of res.entries) {
		if (e.kind === 'folder') {
			const driftPart = e.modified > 0 ? `, ${e.modified} modified` : '';
			console.log(`${e.path}/ — ${e.summary}  [${e.files_count} file${e.files_count === 1 ? '' : 's'}${driftPart}]`);
		} else {
			const exp = e.exports ? `  [exports: ${e.exports}]` : '';
			console.log(`${e.path} — ${e.summary}${exp}`);
		}
	}
}

async function cmdDetail(args: string[], json: boolean): Promise<void> {
	const positional = [...args];
	const search = getFlag(positional, '--search');
	const limit = getFlag(positional, '--limit');
	const shallow = hasFlag(positional, '--shallow');
	// Strip used flags from positional.
	const cleaned = positional.filter((a, i, arr) => {
		if (a === '--search' || a === '--limit') return false;
		if ((arr[i - 1] === '--search' || arr[i - 1] === '--limit') && a === positional[i]) return false;
		if (a === '--shallow') return false;
		return true;
	});
	const needle = cleaned[0];
	const subPath = cleaned[1] ?? '.';
	if (!needle) fail('Usage: mm project detail <name|path> [subpath] [--search q] [--limit n] [--shallow]');
	const proj = await resolveProject(needle);
	if (!proj) fail(`No project matches "${needle}".`);

	const params = new URLSearchParams();
	params.set('path', subPath);
	if (!shallow) params.set('deep', '1');
	else params.set('deep', '0');
	if (search) params.set('search', search);
	if (limit) params.set('limit', limit);

	const t0 = Date.now();
	const res = await api<DetailResponse>(`/api/projects/${proj.id}/index?${params}`);
	const dt = ((Date.now() - t0) / 1000).toFixed(1);
	if (json) {
		process.stdout.write(JSON.stringify({ project: proj, ...res }, null, 2) + '\n');
		return;
	}
	const headerBits = [
		`${proj.label} / ${subPath}`,
		`${res.entries.length} file${res.entries.length === 1 ? '' : 's'}`,
	];
	if (res.refreshed > 0) headerBits.push(`${res.refreshed} re-summarised`);
	if (search) headerBits.push(`search="${search}"`);
	console.log(`[${dt}s] ${headerBits.join(' · ')}\n`);
	for (const e of res.entries) {
		const exp = e.exports ? `  [exports: ${e.exports}]` : '';
		console.log(`${e.path} — ${e.summary}${exp}`);
	}
}

async function cmdAdd(args: string[], json: boolean): Promise<void> {
	const rawPath = args[0];
	if (!rawPath) fail('Usage: mm project add <path> [label]');
	const root = expandPath(rawPath);
	try {
		if (!statSync(root).isDirectory()) fail(`${root}: not a directory`);
	} catch {
		fail(`${root}: does not exist`);
	}
	const label = args[1];
	const res = await api<{ project?: ProjectRow; ok?: boolean; error?: string }>('/api/projects', {
		method: 'POST',
		headers: { 'content-type': 'application/json' },
		body: JSON.stringify({ root, label }),
	});
	if (!res.project) fail(`Failed: ${res.error ?? 'unknown error'}`);
	if (json) {
		process.stdout.write(JSON.stringify(res.project, null, 2) + '\n');
		return;
	}
	console.log(`Registered "${res.project.label}" at ${res.project.root_path}.`);
	console.log(`Try: mm project overview ${res.project.label}`);
}

async function cmdRebuild(args: string[], json: boolean): Promise<void> {
	const needle = args[0];
	const subPath = args[1];
	if (!needle) fail('Usage: mm project rebuild <name|path> [subpath]');
	const proj = await resolveProject(needle);
	if (!proj) fail(`No project matches "${needle}".`);
	const t0 = Date.now();
	const res = await api<{ refreshed: number; skipped: number }>(
		`/api/projects/${proj.id}/index/refresh`,
		{
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify(subPath ? { path: subPath } : {}),
		},
	);
	const dt = ((Date.now() - t0) / 1000).toFixed(1);
	if (json) {
		process.stdout.write(JSON.stringify({ project: proj, ...res }, null, 2) + '\n');
		return;
	}
	console.log(
		`[${dt}s] rebuilt ${proj.label}${subPath ? ` / ${subPath}` : ''}: refreshed=${res.refreshed} skipped=${res.skipped}`,
	);
}

export async function projectDispatch(command: string, args: string[], flags: { json?: boolean }) {
	const json = flags?.json || false;
	switch (command) {
		case '':
		case 'list':
			await cmdList(json);
			break;
		case 'overview':
			await cmdOverview(args, json);
			break;
		case 'detail':
			await cmdDetail(args, json);
			break;
		case 'add':
			await cmdAdd(args, json);
			break;
		case 'rebuild':
			await cmdRebuild(args, json);
			break;
		case 'help':
		case '--help':
		case '-h':
			printProjectHelp();
			break;
		default:
			process.stderr.write(`Unknown project subcommand: ${command}\n`);
			printProjectHelp();
			process.exit(1);
	}
}

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
			last_touched?: number | null;
			unsummarised?: number;
			dominant_language?: string | null;
			subfolders: Array<{ name: string; files_count: number }>;
			subfolders_total: number;
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

function relTime(ms: number): string {
	const diff = Date.now() - ms;
	const s = Math.floor(diff / 1000);
	if (s < 60) return 'just now';
	const m = Math.floor(s / 60);
	if (m < 60) return `${m}m ago`;
	const h = Math.floor(m / 60);
	if (h < 24) return `${h}h ago`;
	const d = Math.floor(h / 24);
	if (d < 30) return `${d}d ago`;
	const mo = Math.floor(d / 30);
	if (mo < 12) return `${mo}mo ago`;
	return `${Math.floor(mo / 12)}y ago`;
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
  find     <query>                Search across ALL projects (path, exports, summary).
                                  Flags: --project <name|path>  --limit <n>
  discover [--root <path>] [--apply]
                                  Scan a root dir for project-shaped subdirs.
                                  Default --root ~/Documents/dev. Dry-run unless --apply.
                                  Shallow-indexes README / CLAUDE / AGENTS / NOW /
                                  package.json for each newly-registered project.
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
	if (drift > 0) headerBits.push(`${drift} modified in last 7d`);
	if (res.folders_refreshed > 0) headerBits.push(`${res.folders_refreshed} re-summarised`);
	console.log(`[${dt}s] ${headerBits.join(' · ')}\n`);
	for (const e of res.entries) {
		if (e.kind === 'folder') {
			const activityPart =
				e.modified > 0
					? `, ${e.modified} modified in last 7d`
					: e.last_touched
						? `, touched ${relTime(e.last_touched)}`
						: '';
			const unsumPart = e.unsummarised && e.unsummarised > 0 ? `, ${e.unsummarised} not yet read` : '';
			const subBits =
				e.subfolders && e.subfolders.length > 0
					? '\n  · subfolders: ' +
						e.subfolders.map((s) => `${s.name} (${s.files_count})`).join(', ') +
						(e.subfolders_total > e.subfolders.length
							? `, +${e.subfolders_total - e.subfolders.length} more`
							: '')
					: '';
			if (!e.summary && e.dominant_language) {
				console.log(`${e.path}/  [${e.files_count} ${e.dominant_language} file${e.files_count === 1 ? '' : 's'}${activityPart}${unsumPart}]${subBits}`);
			} else {
				console.log(`${e.path}/ — ${e.summary}  [${e.files_count} file${e.files_count === 1 ? '' : 's'}${activityPart}${unsumPart}]${subBits}`);
			}
		} else {
			const exp = e.exports ? `  [exports: ${e.exports}]` : '';
			console.log(e.summary ? `${e.path} — ${e.summary}${exp}` : `${e.path}${exp}`);
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
		console.log(e.summary ? `${e.path} — ${e.summary}${exp}` : `${e.path}${exp}`);
	}
}

async function cmdFind(args: string[], json: boolean): Promise<void> {
	const positional = [...args];
	const projectScope = getFlag(positional, '--project');
	const limit = getFlag(positional, '--limit');
	const cleaned = positional.filter((a, i, arr) => {
		if (a === '--project' || a === '--limit') return false;
		if ((arr[i - 1] === '--project' || arr[i - 1] === '--limit') && a === positional[i]) return false;
		return true;
	});
	const query = cleaned.join(' ').trim();
	if (!query) fail('Usage: mm project find <query> [--project <name|path>] [--limit <n>]');

	const params = new URLSearchParams();
	params.set('q', query);
	if (limit) params.set('limit', limit);
	if (projectScope) {
		const proj = await resolveProject(projectScope);
		if (!proj) fail(`No project matches "${projectScope}".`);
		params.set('project', proj.id);
	}

	type Hit = {
		project_id: string;
		project_label: string;
		project_root: string;
		path: string;
		summary: string;
		exports: string | null;
		kind: string;
		language: string | null;
		mtime: number;
		rank: number;
	};
	const res = await api<{ q: string; hits: Hit[]; limit: number }>(`/api/projects/find?${params}`);

	if (json) {
		process.stdout.write(JSON.stringify(res, null, 2) + '\n');
		return;
	}
	if (res.hits.length === 0) {
		console.log(`(no matches for "${query}")`);
		return;
	}
	console.log(`${res.hits.length} hit${res.hits.length === 1 ? '' : 's'} for "${query}":\n`);
	// Group by project for readability.
	const byProject = new Map<string, Hit[]>();
	for (const h of res.hits) {
		const list = byProject.get(h.project_label) ?? [];
		list.push(h);
		byProject.set(h.project_label, list);
	}
	for (const [label, hits] of byProject) {
		console.log(`${label}:`);
		for (const h of hits) {
			const exp = h.exports ? `  [exports: ${h.exports}]` : '';
			const sumPart = h.summary ? ` — ${h.summary}` : '';
			console.log(`  ${h.path}${sumPart}${exp}`);
		}
		console.log('');
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

async function cmdDiscover(args: string[], json: boolean): Promise<void> {
	const positional = [...args];
	const rootArg = getFlag(positional, '--root');
	const apply = hasFlag(positional, '--apply');
	const root = rootArg ? expandPath(rootArg) : `${homedir()}/Documents/dev`;

	type Candidate = {
		root_path: string;
		label: string;
		already_registered: boolean;
		registered_id: string | null;
	};
	type Registered = {
		id: string;
		label: string;
		root_path: string;
		files_indexed: number;
	};
	type Result = { root: string; candidates: Candidate[]; registered: Registered[] };

	const t0 = Date.now();
	const res = await api<Result>('/api/projects/discover', {
		method: 'POST',
		headers: { 'content-type': 'application/json' },
		body: JSON.stringify({ root, apply }),
	});
	const dt = ((Date.now() - t0) / 1000).toFixed(1);

	if (json) {
		process.stdout.write(JSON.stringify(res, null, 2) + '\n');
		return;
	}

	const fresh = res.candidates.filter((c) => !c.already_registered);
	console.log(`Scanning ${res.root}…`);
	console.log(
		`Found ${res.candidates.length} project${res.candidates.length === 1 ? '' : 's'} (${fresh.length} new, ${res.candidates.length - fresh.length} already registered)\n`,
	);

	if (apply) {
		if (res.registered.length === 0) {
			console.log('(nothing new to register)');
			return;
		}
		console.log(`Registered + shallow-indexed in ${dt}s:`);
		for (const r of res.registered) {
			console.log(`  ${r.label.padEnd(28)} ${r.files_indexed} file${r.files_indexed === 1 ? '' : 's'} indexed`);
		}
		console.log(`\nThese projects now appear in 'mm project list' and the SPA project list.\nUse 'mm project rebuild <name>' for full indexing of any of them.`);
	} else {
		if (fresh.length === 0) {
			console.log('(no new candidates — everything already registered)');
			return;
		}
		console.log('Would register:');
		for (const c of fresh) {
			console.log(`  ${c.label.padEnd(28)} ${c.root_path}`);
		}
		console.log('\nRun with --apply to register + shallow-index canonical entry-point files.');
	}
}

async function cmdRebuild(args: string[], json: boolean): Promise<void> {
	const needle = args[0];
	const subPath = args[1];
	if (!needle) fail('Usage: mm project rebuild <name|path> [subpath]');
	const proj = await resolveProject(needle);
	if (!proj) fail(`No project matches "${needle}".`);

	// POST kicks off (or rejoins) the async rebuild; the daemon walks the
	// tree in the background and we poll the status endpoint for progress.
	// Returns 202 with `{accepted, alreadyRunning, startedAt, indexed}`.
	const start = await api<{
		accepted: boolean;
		alreadyRunning: boolean;
		startedAt: number;
		indexed: number;
	}>(`/api/projects/${proj.id}/index/refresh`, {
		method: 'POST',
		headers: { 'content-type': 'application/json' },
		body: JSON.stringify(subPath ? { path: subPath } : {}),
	});

	if (!json) {
		const prefix = start.alreadyRunning ? 'rebuild already running' : 'rebuild started';
		process.stdout.write(`${prefix} for ${proj.label}${subPath ? ` / ${subPath}` : ''} — polling…\n`);
	}

	type Status = {
		running: boolean;
		indexed: number;
		startedAt: number | null;
		finishedAt: number | null;
		refreshed: number | null;
		error: string | null;
	};

	const t0 = Date.now();
	let last = 0;
	while (true) {
		const status = await api<Status>(`/api/projects/${proj.id}/index/refresh/status`);
		if (!json && status.running && status.indexed !== last) {
			const elapsed = ((Date.now() - t0) / 1000).toFixed(0);
			process.stdout.write(`  [${elapsed}s] indexed=${status.indexed}\n`);
			last = status.indexed;
		}
		if (!status.running) {
			const dt = ((Date.now() - t0) / 1000).toFixed(1);
			if (json) {
				process.stdout.write(
					JSON.stringify({ project: proj, ...status, elapsed_seconds: Number(dt) }, null, 2) + '\n',
				);
				return;
			}
			if (status.error) {
				fail(`[${dt}s] rebuild failed: ${status.error}`);
			}
			console.log(
				`[${dt}s] rebuilt ${proj.label}${subPath ? ` / ${subPath}` : ''}: refreshed=${status.refreshed ?? 0} indexed=${status.indexed}`,
			);
			return;
		}
		await new Promise((r) => setTimeout(r, 2000));
	}
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
		case 'find':
			await cmdFind(args, json);
			break;
		case 'discover':
			await cmdDiscover(args, json);
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

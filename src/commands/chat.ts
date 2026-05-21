/**
 * mm chat — local agent threads.
 *
 * Reads `~/.mm/meta-me-local-agent.db` via bun:sqlite. The agent owns
 * writes; this is a read-only window into the conversation history so
 * you can list / show / search from any terminal.
 */

import { Database } from 'bun:sqlite';
import { existsSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { hubApi } from '../hub';
import { getTailscaleSuffix } from '../tailscale';

const DB_PATH = join(homedir(), '.mm', 'meta-me-local-agent.db');
const AGENT_BASE = process.env.MM_LOCAL_AGENT_URL ?? 'http://localhost:3142';

function open(): Database {
	if (!existsSync(DB_PATH)) {
		process.stderr.write(`Error: agent DB not found at ${DB_PATH}\n`);
		process.stderr.write('Is the local agent installed? See https://meta-me.uk/cli\n');
		process.exit(1);
	}
	return new Database(DB_PATH, { readonly: true });
}

function fmtTime(ms: number): string {
	const d = new Date(ms);
	return d.toISOString().slice(0, 16).replace('T', ' ');
}

function relTime(ms: number): string {
	const diff = Date.now() - ms;
	const s = Math.floor(diff / 1000);
	if (s < 60) return `${s}s ago`;
	const m = Math.floor(s / 60);
	if (m < 60) return `${m}m ago`;
	const h = Math.floor(m / 60);
	if (h < 24) return `${h}h ago`;
	const d = Math.floor(h / 24);
	return `${d}d ago`;
}

function truncate(s: string, max: number): string {
	const flat = s.replace(/\s+/g, ' ').trim();
	return flat.length <= max ? flat : flat.slice(0, max - 1) + '…';
}

export function printChatHelp() {
	console.log(`mm chat — local agent threads

Subcommands:
  list [--node <name>] [--limit N] [--project <id>]
                       Recent threads, newest first (default limit 20)
  show <id> [--node <name>] [--limit N]
                       Print messages in a thread (default limit 50). With
                       --node, requires a full UUID (no prefix resolution).
  search <query> [--node <name>] [--limit N]
                       Substring match across message bodies (case-insensitive)
  projects [--node <name>]
                       List known projects + thread counts
  send "<message>" [--new] [--title <t>] [--model <id>] [--thread <id>] [--project <id>]
                       Drive a turn on the local agent. Streams assistant
                       output to stdout. Without --new or --thread, continues
                       the most recently updated thread.
  nodes                List registered agent nodes from the hub (instance.list)
  models [--node <name>]
                       List models the agent has provider keys for
  help                 Show this help

Reads ${DB_PATH} (read-only) for list/show/search/projects.
\`send\` talks to the agent at ${AGENT_BASE} (override with MM_LOCAL_AGENT_URL).
Add --json for parseable output.

Tips:
  • Thread IDs are UUIDs; a 6-char prefix is enough for \`show\` or \`send --thread\`.
  • \`mm chat list\` is the fastest way to find a thread to resume in
    chat.meta-me.uk — the title and timestamp identify it; click it
    there to pick up where it left off.`);
}

function resolveThreadId(db: Database, prefix: string): string | null {
	const exact = db.query<{ id: string }, [string]>('SELECT id FROM thread WHERE id = ?').get(prefix);
	if (exact) return exact.id;
	const matches = db
		.query<{ id: string }, [string]>('SELECT id FROM thread WHERE id LIKE ? LIMIT 2')
		.all(`${prefix}%`);
	if (matches.length === 1) return matches[0].id;
	if (matches.length > 1) {
		process.stderr.write(`Error: thread prefix "${prefix}" is ambiguous\n`);
		process.exit(1);
	}
	return null;
}

const UUID_RE_CLI = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Resolve --project for the local SQLite path. Mirrors the agent's
 * resolveProjectInput so `mm chat list --project joe-inc` works
 * identically with and without --node.
 */
function resolveLocalProject(db: Database, input: string): string | null {
	if (UUID_RE_CLI.test(input)) {
		const row = db.query<{ id: string }, [string]>('SELECT id FROM project WHERE id = ?').get(input);
		if (row) return row.id;
	}
	const matches = db
		.query<{ id: string }, [string]>('SELECT id FROM project WHERE LOWER(label) = LOWER(?)')
		.all(input);
	if (matches.length === 1) return matches[0].id;
	if (matches.length > 1) {
		process.stderr.write(`Error: project label '${input}' is ambiguous (${matches.length} matches)\n`);
		process.exit(1);
	}
	return null;
}

function getFlag(args: string[], name: string): string | undefined {
	const i = args.indexOf(name);
	return i >= 0 && i + 1 < args.length ? args[i + 1] : undefined;
}

async function listThreads(args: string[], json: boolean) {
	args = await consumeMentionsFromArgs(args);
	const limit = parseInt(getFlag(args, '--limit') || '20', 10);
	const projectId = getFlag(args, '--project');
	const node = getFlag(args, '--node');

	let rows: any[];
	if (node) {
		const qs = new URLSearchParams({ limit: String(limit) });
		if (projectId) qs.set('project_id', projectId);
		const resp = await nodeFetch(node, `/api/threads?${qs}`);
		if (!resp.ok) {
			process.stderr.write(`Error: GET /api/threads ${resp.status}\n`);
			process.exit(1);
		}
		rows = ((await resp.json()) as { threads: any[] }).threads ?? [];
	} else {
		const db = open();
		let resolvedProjectId: string | null = null;
		if (projectId) {
			resolvedProjectId = resolveLocalProject(db, projectId);
			if (!resolvedProjectId) {
				db.close();
				process.stderr.write(
					`Error: project '${projectId}' not found locally. Try \`mm chat projects\` for the list.\n`,
				);
				process.exit(1);
			}
		}
		rows = resolvedProjectId
			? db
					.query<any, [string, number]>(
						`SELECT id, title, project_id, model_id, updated_at,
						        (SELECT COUNT(*) FROM message WHERE thread_id = thread.id) AS msg_count
						   FROM thread WHERE project_id = ? ORDER BY updated_at DESC LIMIT ?`,
					)
					.all(resolvedProjectId, limit)
			: db
					.query<any, [number]>(
						`SELECT id, title, project_id, model_id, updated_at,
						        (SELECT COUNT(*) FROM message WHERE thread_id = thread.id) AS msg_count
						   FROM thread ORDER BY updated_at DESC LIMIT ?`,
					)
					.all(limit);
		db.close();
	}

	if (json) {
		process.stdout.write(JSON.stringify(rows, null, 2) + '\n');
		return;
	}
	if (rows.length === 0) {
		console.log('(no threads)');
		return;
	}
	for (const r of rows) {
		const id6 = r.id.slice(0, 6);
		const proj = r.project_id ? ` [${r.project_id.slice(0, 6)}]` : '';
		const model = r.model_id ? ` ${r.model_id.split('/').pop()}` : '';
		console.log(`${id6}  ${relTime(r.updated_at).padEnd(8)} ${(r.msg_count ?? 0).toString().padStart(3)}msg  ${truncate(r.title, 60)}${proj}${model}`);
	}
}

async function showThread(args: string[], json: boolean) {
	args = await consumeMentionsFromArgs(args);
	const idArg = args[0];
	if (!idArg) {
		process.stderr.write('Usage: mm chat show <id> [--node <name>]\n');
		process.exit(1);
	}
	const node = getFlag(args, '--node');
	const limit = parseInt(getFlag(args, '--limit') || '50', 10);

	let thread: any;
	let messages: any[];

	if (node) {
		// Without local SQLite there's no prefix resolution — require full id.
		if (idArg.length < 36) {
			process.stderr.write(
				`Error: --node requires a full thread id (36 chars), got '${idArg}'. Use \`mm chat list --node ${node}\` to find one.\n`,
			);
			process.exit(1);
		}
		const threadsResp = await nodeFetch(node, `/api/threads?limit=1000`);
		if (!threadsResp.ok) {
			process.stderr.write(`Error: GET /api/threads ${threadsResp.status}\n`);
			process.exit(1);
		}
		const threadsData = (await threadsResp.json()) as { threads: any[] };
		thread = (threadsData.threads ?? []).find((t) => t.id === idArg);
		if (!thread) {
			process.stderr.write(`Error: thread not found on node '${node}': ${idArg}\n`);
			process.exit(1);
		}
		const msgsResp = await nodeFetch(node, `/api/threads/${idArg}/messages`);
		if (!msgsResp.ok) {
			process.stderr.write(`Error: GET /api/threads/:id/messages ${msgsResp.status}\n`);
			process.exit(1);
		}
		const msgsData = (await msgsResp.json()) as { messages: any[] };
		messages = (msgsData.messages ?? []).slice(0, limit);
	} else {
		const db = open();
		const id = resolveThreadId(db, idArg);
		if (!id) {
			db.close();
			process.stderr.write(`Error: thread not found: ${idArg}\n`);
			process.exit(1);
		}
		thread = db.query<any, [string]>('SELECT * FROM thread WHERE id = ?').get(id);
		messages = db
			.query<any, [string, number]>(
				'SELECT id, role, content, created_at FROM message WHERE thread_id = ? ORDER BY created_at ASC LIMIT ?',
			)
			.all(id, limit);
		db.close();
	}

	if (json) {
		process.stdout.write(JSON.stringify({ thread, messages }, null, 2) + '\n');
		return;
	}
	console.log(`# ${thread.title}`);
	console.log(`id: ${thread.id}`);
	console.log(`updated: ${fmtTime(thread.updated_at)}`);
	if (thread.project_id) console.log(`project: ${thread.project_id}`);
	if (thread.model_id) console.log(`model: ${thread.model_provider}/${thread.model_id}`);
	console.log('');
	for (const m of messages) {
		console.log(`── ${m.role} · ${fmtTime(m.created_at)} ──`);
		console.log(m.content);
		console.log('');
	}
	if (messages.length === limit) {
		console.log(`(showing first ${limit} messages; use --limit to see more)`);
	}
}

async function searchMessages(args: string[], json: boolean) {
	args = await consumeMentionsFromArgs(args);
	const query = args[0];
	if (!query || query.startsWith('--')) {
		process.stderr.write('Usage: mm chat search <query> [--node <name>] [--limit N]\n');
		process.exit(1);
	}
	const limit = parseInt(getFlag(args, '--limit') || '20', 10);
	const node = getFlag(args, '--node');

	let rows: any[];
	if (node) {
		const qs = new URLSearchParams({ q: query, limit: String(limit) });
		const resp = await nodeFetch(node, `/api/messages/search?${qs}`);
		if (resp.status === 404) {
			process.stderr.write(
				`Error: node '${node}' doesn't have /api/messages/search yet — it needs to be redeployed with the current meta-me-local-agent build.\n`,
			);
			process.exit(1);
		}
		if (!resp.ok) {
			process.stderr.write(`Error: GET /api/messages/search ${resp.status}\n`);
			process.exit(1);
		}
		rows = ((await resp.json()) as { matches: any[] }).matches ?? [];
	} else {
		const db = open();
		rows = db
			.query<any, [string, number]>(
				`SELECT m.id, m.thread_id, m.role, m.content, m.created_at, t.title
				   FROM message m JOIN thread t ON t.id = m.thread_id
				  WHERE LOWER(m.content) LIKE LOWER(?)
				  ORDER BY m.created_at DESC LIMIT ?`,
			)
			.all(`%${query}%`, limit);
		db.close();
	}

	if (json) {
		process.stdout.write(JSON.stringify(rows, null, 2) + '\n');
		return;
	}
	if (rows.length === 0) {
		console.log('(no matches)');
		return;
	}
	for (const r of rows) {
		const tid6 = r.thread_id.slice(0, 6);
		console.log(`${tid6}  ${relTime(r.created_at).padEnd(8)} ${r.role.padEnd(9)} ${truncate(r.title, 30)}`);
		console.log(`        ${truncate(r.content, 100)}`);
	}
}

async function listProjects(args: string[], json: boolean) {
	args = await consumeMentionsFromArgs(args);
	const node = getFlag(args, '--node');
	let rows: any[];
	if (node) {
		const resp = await nodeFetch(node, `/api/projects`);
		if (!resp.ok) {
			process.stderr.write(`Error: GET /api/projects ${resp.status}\n`);
			process.exit(1);
		}
		rows = ((await resp.json()) as { projects: any[] }).projects ?? [];
	} else {
		const db = open();
		rows = db
			.query<any, []>(
				`SELECT p.id, p.label, p.root_path, p.last_opened_at,
				        (SELECT COUNT(*) FROM thread WHERE project_id = p.id) AS thread_count
				   FROM project p ORDER BY p.last_opened_at DESC`,
			)
			.all();
		db.close();
	}
	if (json) {
		process.stdout.write(JSON.stringify(rows, null, 2) + '\n');
		return;
	}
	if (rows.length === 0) {
		console.log('(no projects)');
		return;
	}
	for (const r of rows) {
		const id6 = r.id.slice(0, 6);
		const count = (r.thread_count ?? 0).toString().padStart(3);
		console.log(`${id6}  ${count}thr  ${r.label.padEnd(24)}  ${r.root_path}`);
	}
}

function hasFlag(args: string[], name: string): boolean {
	return args.includes(name);
}

// --- @<entity> mentions (phase 1: @node + @project) ---
// Spec: specs/at-mentions.md in meta-me-local-agent (§ 3.1 updated).
//
// Anywhere-in-message resolution: any @<token> that *unambiguously* resolves
// to a registered node or project sets routing/binding metadata, wherever it
// appears. Unresolved @<token> values (handles, prose, email addresses) pass
// through as plain text — false-routing is avoided by the unambiguous-only
// rule rather than by position-locking.
//
// Stripping: mentions in the leading block (whitespace + resolved mentions
// only before them) are removed from the outbound prompt. Mid-sentence
// mentions stay inline so the prose still reads naturally and the model can
// mirror the notation in its reply.
//
// Escape: `@@<token>` disables resolution and is unescaped to `@<token>` in
// the outbound body. For literal-mention cases (writing about the syntax,
// quoting another conversation).
//
// `@node:<name>` / `@project:<name>` force a single-axis lookup for the
// (rare) bare-name collision.

const MENTION_RE = /(?<![@\w])@([a-zA-Z0-9][\w.:-]*)/g;
const ESCAPE_RE = /@@([a-zA-Z0-9][\w.:-]*)/g;

async function lookupNodeName(name: string): Promise<boolean> {
	try {
		const nodes = await loadNodes();
		return nodes.some((n) => n.name.toLowerCase() === name.toLowerCase());
	} catch {
		return false;
	}
}

async function lookupProjectName(name: string, targetNode: string | undefined): Promise<boolean> {
	if (targetNode) {
		try {
			const resp = await nodeFetch(targetNode, '/api/projects');
			if (!resp.ok) return false;
			const data = (await resp.json()) as { projects: { label: string }[] };
			return (data.projects ?? []).some((p) => p.label.toLowerCase() === name.toLowerCase());
		} catch {
			return false;
		}
	}
	if (!existsSync(DB_PATH)) return false;
	const db = new Database(DB_PATH, { readonly: true });
	try {
		const row = db
			.query<{ c: number }, [string]>(
				'SELECT COUNT(*) AS c FROM project WHERE LOWER(label) = LOWER(?)',
			)
			.get(name);
		return (row?.c ?? 0) > 0;
	} finally {
		db.close();
	}
}

type ResolvedMention = {
	start: number; // index in original message of '@'
	end: number; // one-past-end of the matched mention
	type: 'node' | 'project';
	name: string;
};

/**
 * Classify a single token (sans leading `@`). Returns the resolved type +
 * name, or `null` if the token doesn't resolve to anything we know. Throws
 * on cross-axis ambiguity (same bare name matches both node and project).
 */
async function classifyToken(
	token: string,
	contextNode: string | undefined,
): Promise<{ type: 'node' | 'project'; name: string } | null> {
	if (token.startsWith('node:')) {
		const name = token.slice('node:'.length);
		return name.length > 0 ? { type: 'node', name } : null;
	}
	if (token.startsWith('project:')) {
		const name = token.slice('project:'.length);
		return name.length > 0 ? { type: 'project', name } : null;
	}
	const [isNode, isProject] = await Promise.all([
		lookupNodeName(token),
		lookupProjectName(token, contextNode),
	]);
	if (isNode && isProject) {
		throw new Error(
			`'@${token}' is ambiguous (matches: node '${token}', project '${token}'). ` +
				`Use @node:${token} or @project:${token}.`,
		);
	}
	if (isNode) return { type: 'node', name: token };
	if (isProject) return { type: 'project', name: token };
	return null;
}

/**
 * Scan the message for `@<token>` mentions. Resolve each via the lookups.
 * Apply first-wins-per-axis to set node/project. Build the outbound body by
 * stripping leading-block resolved mentions and unescaping `@@<token>`.
 */
async function scanMessageMentions(
	message: string,
	existing: { node?: string; project?: string },
): Promise<{ body: string; node?: string; project?: string; warnings: string[] }> {
	const warnings: string[] = [];

	// Collect all regex matches first so positions stay stable.
	const matches: { start: number; end: number; token: string }[] = [];
	let m: RegExpExecArray | null;
	MENTION_RE.lastIndex = 0;
	while ((m = MENTION_RE.exec(message)) !== null) {
		matches.push({ start: m.index, end: m.index + m[0].length, token: m[1] });
	}

	// Resolve in order. An earlier @<node> mention narrows the project
	// lookup target for subsequent untyped tokens (so `@joe-inc` on a
	// message routed to fedora resolves against fedora's projects).
	let contextNode = existing.node;
	const resolved: ResolvedMention[] = [];
	for (const match of matches) {
		const cls = await classifyToken(match.token, contextNode);
		if (!cls) continue;
		if (cls.type === 'node' && contextNode === undefined) {
			contextNode = cls.name;
		}
		resolved.push({ start: match.start, end: match.end, type: cls.type, name: cls.name });
	}

	// First-wins-per-axis; explicit flags override.
	let node = existing.node;
	let project = existing.project;
	let nodeClaimed = existing.node !== undefined;
	let projectClaimed = existing.project !== undefined;
	for (const r of resolved) {
		if (r.type === 'node') {
			if (existing.node !== undefined) {
				if (existing.node.toLowerCase() !== r.name.toLowerCase()) {
					warnings.push(`warning: --node '${existing.node}' overrides @${r.name}`);
				}
			} else if (!nodeClaimed) {
				node = r.name;
				nodeClaimed = true;
			}
		} else {
			if (existing.project !== undefined) {
				if (existing.project.toLowerCase() !== r.name.toLowerCase()) {
					warnings.push(`warning: --project '${existing.project}' overrides @${r.name}`);
				}
			} else if (!projectClaimed) {
				project = r.name;
				projectClaimed = true;
			}
		}
	}

	// Strip resolved mentions in the leading block (whitespace + already-
	// stripped mentions before them). Mid-sentence mentions stay inline.
	let cursor = 0;
	while (cursor < message.length && /\s/.test(message[cursor])) cursor++;
	let stripped = false;
	for (const r of resolved) {
		if (r.start !== cursor) break;
		cursor = r.end;
		while (cursor < message.length && /\s/.test(message[cursor])) cursor++;
		stripped = true;
	}
	let body = stripped ? message.slice(cursor) : message;
	body = body.replace(ESCAPE_RE, '@$1');

	return { body, node, project, warnings };
}

/**
 * For read commands: peel leading args of shape `@<token>`. Different from
 * `send` — argv has no natural "mid-sentence" position, so any @-prefixed
 * arg before a flag/positional is a mention. Stop at the first non-mention
 * arg. Resolved mentions are dropped; unresolved ones (or same-axis dupes)
 * are pushed back as @-prefixed args so the caller can still see them.
 */
async function consumeMentionsFromArgs(args: string[]): Promise<string[]> {
	const tokens: string[] = [];
	let i = 0;
	while (i < args.length && args[i].startsWith('@') && args[i].length > 1) {
		tokens.push(args[i].slice(1));
		i++;
	}
	if (tokens.length === 0) return args;

	const existing = { node: getFlag(args, '--node'), project: getFlag(args, '--project') };
	let contextNode = existing.node;
	const dropped = new Set<number>();
	const warnings: string[] = [];
	let node = existing.node;
	let project = existing.project;
	let nodeClaimed = existing.node !== undefined;
	let projectClaimed = existing.project !== undefined;

	for (let j = 0; j < tokens.length; j++) {
		const cls = await classifyToken(tokens[j], contextNode);
		if (!cls) continue;
		if (cls.type === 'node' && contextNode === undefined) contextNode = cls.name;
		if (cls.type === 'node') {
			if (existing.node !== undefined) {
				if (existing.node.toLowerCase() !== cls.name.toLowerCase()) {
					warnings.push(`warning: --node '${existing.node}' overrides @${cls.name}`);
				}
				dropped.add(j);
			} else if (!nodeClaimed) {
				node = cls.name;
				nodeClaimed = true;
				dropped.add(j);
			}
		} else {
			if (existing.project !== undefined) {
				if (existing.project.toLowerCase() !== cls.name.toLowerCase()) {
					warnings.push(`warning: --project '${existing.project}' overrides @${cls.name}`);
				}
				dropped.add(j);
			} else if (!projectClaimed) {
				project = cls.name;
				projectClaimed = true;
				dropped.add(j);
			}
		}
	}

	for (const w of warnings) process.stderr.write(w + '\n');

	const keptTokens: string[] = [];
	for (let j = 0; j < tokens.length; j++) {
		if (!dropped.has(j)) keptTokens.push(`@${tokens[j]}`);
	}
	const tailArgs = args.slice(i);
	const newArgs = [...keptTokens, ...tailArgs];
	if (node && !existing.node) newArgs.push('--node', node);
	if (project && !existing.project) newArgs.push('--project', project);
	return newArgs;
}

type InstanceListItem = {
	id: string;
	appSlug: string;
	name: string;
	url: string | null;
	isOwner: boolean;
};

let nodesCache: InstanceListItem[] | null = null;

async function loadNodes(): Promise<InstanceListItem[]> {
	if (nodesCache) return nodesCache;
	const data = await hubApi<{ instances: InstanceListItem[] }>('instance', 'list', {
		slugs: ['chat', 'agent'],
	});
	nodesCache = data.instances ?? [];
	return nodesCache;
}

/**
 * Resolve `--node <name>` to a base URL fronted by the local tailscaled's
 * MagicDNS suffix, so cert + connection stay in sync across suffix rotations.
 * The stored `app_instance.url` provides the bare hostname + port; the suffix
 * comes from `tailscale status --json`.
 */
async function resolveNode(name: string): Promise<{ baseUrl: string; displayName: string }> {
	const nodes = await loadNodes();
	const lower = name.toLowerCase();
	const matches = nodes.filter((n) => n.name.toLowerCase() === lower);
	if (matches.length === 0) {
		const known = nodes.map((n) => n.name).join(', ') || '(none registered)';
		throw new Error(`No node named '${name}'. Known: ${known}. Try: mm chat nodes`);
	}
	if (matches.length > 1) {
		throw new Error(`Multiple nodes named '${name}'. Disambiguate via the hub.`);
	}
	const row = matches[0];
	if (!row.url) {
		throw new Error(`Node '${row.name}' has no URL registered.`);
	}
	const parsed = new URL(row.url);
	const bare = parsed.hostname.split('.')[0];
	const port = parsed.port || (parsed.protocol === 'https:' ? '443' : '80');
	const suffix = getTailscaleSuffix();
	return {
		baseUrl: `https://${bare}.${suffix}:${port}`,
		displayName: row.name,
	};
}

async function nodeFetch(node: string, path: string): Promise<Response> {
	const { baseUrl, displayName } = await resolveNode(node);
	const url = `${baseUrl}${path}`;
	try {
		return await fetch(url);
	} catch (err) {
		throw new Error(`fetch ${url} failed (node '${displayName}'): ${err}`);
	}
}

async function listNodes(json: boolean) {
	try {
		// Cover both the current `chat` slug (deployed reality — m4, fedora,
		// dees-imac all filed under 'chat') and the prospective `agent` slug
		// from the rename discussion. Whichever the hub has, we surface.
		const data = await hubApi<{ instances: InstanceListItem[] }>('instance', 'list', {
			slugs: ['chat', 'agent'],
		});
		const rows = data.instances ?? [];
		if (json) {
			process.stdout.write(JSON.stringify(rows, null, 2) + '\n');
			return;
		}
		if (rows.length === 0) {
			console.log('(no nodes registered)');
			console.log('Register one with: mm v2 instance.create --slug=chat --label=<name> --url=<url>');
			return;
		}
		for (const r of rows) {
			const owner = r.isOwner ? '' : ' (shared)';
			const url = r.url ?? '(no url)';
			console.log(`${r.name.padEnd(20)} ${r.appSlug.padEnd(8)} ${url}${owner}`);
		}
	} catch (err) {
		process.stderr.write(`Error: ${err instanceof Error ? err.message : String(err)}\n`);
		process.exit(1);
	}
}

type AgentModel = {
	provider: string;
	id: string;
	label: string;
	input: string[];
};

async function listModels(args: string[], json: boolean) {
	const nodeFlag = getFlag(args, '--node');
	try {
		const resp = nodeFlag
			? await nodeFetch(nodeFlag, '/api/models')
			: await fetch(`${AGENT_BASE}/api/models`);
		if (!resp.ok) {
			process.stderr.write(`Error: GET /api/models ${resp.status}\n`);
			process.exit(1);
		}
		const data = (await resp.json()) as { models: AgentModel[] };
		const models = data.models ?? [];
		if (json) {
			process.stdout.write(JSON.stringify(models, null, 2) + '\n');
			return;
		}
		if (models.length === 0) {
			console.log('(no models available — set a provider key via the SPA or ~/.pi/agent/auth.json)');
			return;
		}
		for (const m of models) {
			const fullId = `${m.provider}/${m.id}`;
			const inputs = m.input.join(',');
			console.log(`${m.label.padEnd(6)} ${fullId.padEnd(40)} [${inputs}]`);
		}
	} catch (err) {
		process.stderr.write(`Error: agent unreachable at ${AGENT_BASE} (${err})\n`);
		process.exit(1);
	}
}

async function sendMessage(args: string[], flags: { json?: boolean }) {
	const rawMessage = args[0];
	if (!rawMessage || rawMessage.startsWith('--')) {
		process.stderr.write(
			'Usage: mm chat send "<message>" [--node <name>] [--new] [--title <t>] [--model <id>] [--thread <id>] [--project <id>]\n',
		);
		process.exit(1);
	}

	const isNew = hasFlag(args, '--new');
	const titleFlag = getFlag(args, '--title');
	const modelFlag = getFlag(args, '--model');
	const threadFlag = getFlag(args, '--thread');
	const json = flags?.json || false;

	// Parse @<entity> mentions out of the message. Resolves any @<token>
	// that unambiguously matches a registered node/project (anywhere in the
	// message); leading-block mentions are stripped, mid-sentence ones stay
	// inline. `@@<token>` escapes to a literal `@<token>`. Explicit flags
	// win on conflict; the parser warns.
	const parsed = await scanMessageMentions(rawMessage, {
		node: getFlag(args, '--node'),
		project: getFlag(args, '--project'),
	});
	for (const w of parsed.warnings) process.stderr.write(w + '\n');
	const message = parsed.body.trim();
	if (!message) {
		process.stderr.write('Error: message is empty after stripping mentions.\n');
		process.exit(1);
	}
	const nodeFlag = parsed.node;
	const projectFlag = parsed.project;

	// Resolve target base URL (HTTP) + WS URL for the dial.
	let httpBase: string;
	let wsBase: string;
	if (nodeFlag) {
		const resolved = await resolveNode(nodeFlag);
		httpBase = resolved.baseUrl;
		wsBase = resolved.baseUrl.replace(/^https/, 'wss').replace(/^http/, 'ws');
	} else {
		httpBase = AGENT_BASE;
		wsBase = AGENT_BASE.replace(/^http/, 'ws');
	}

	if (isNew && threadFlag) {
		process.stderr.write('Error: --new and --thread are mutually exclusive\n');
		process.exit(1);
	}

	let provider: string | undefined;
	let modelId: string | undefined;
	if (modelFlag) {
		const slash = modelFlag.indexOf('/');
		if (slash <= 0) {
			process.stderr.write(
				`Error: --model must be provider-prefixed, e.g. google/gemini-3.5-flash\n`,
			);
			process.exit(1);
		}
		provider = modelFlag.slice(0, slash);
		modelId = modelFlag.slice(slash + 1);
	}

	let threadId: string;
	if (threadFlag) {
		if (nodeFlag) {
			// No SQLite prefix resolution on a remote node — require full UUID.
			if (threadFlag.length < 36) {
				process.stderr.write(
					`Error: --thread under --node requires a full UUID (36 chars), got '${threadFlag}'.\n`,
				);
				process.exit(1);
			}
			threadId = threadFlag;
		} else {
			if (!existsSync(DB_PATH)) {
				process.stderr.write(`Error: agent DB not found at ${DB_PATH}\n`);
				process.exit(1);
			}
			const db = new Database(DB_PATH, { readonly: true });
			const resolved = resolveThreadId(db, threadFlag);
			db.close();
			if (!resolved) {
				process.stderr.write(`Error: thread not found: ${threadFlag}\n`);
				process.exit(1);
			}
			threadId = resolved;
		}
	} else if (isNew) {
		const body: Record<string, string> = {};
		if (titleFlag) body.title = titleFlag;
		if (projectFlag) body.project_id = projectFlag;
		try {
			const resp = await fetch(`${httpBase}/api/threads`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify(body),
			});
			if (!resp.ok) {
				const text = await resp.text();
				process.stderr.write(`Error: POST /api/threads ${resp.status}: ${text}\n`);
				process.exit(1);
			}
			const data = (await resp.json()) as { id: string };
			threadId = data.id;
		} catch (err) {
			process.stderr.write(`Error: agent unreachable at ${httpBase} (${err})\n`);
			process.exit(1);
		}
	} else {
		// Continue most recently updated thread, scoped to project if given.
		if (nodeFlag) {
			const qs = new URLSearchParams({ limit: '1' });
			if (projectFlag) qs.set('project_id', projectFlag);
			const resp = await fetch(`${httpBase}/api/threads?${qs}`);
			if (!resp.ok) {
				process.stderr.write(`Error: GET /api/threads on '${nodeFlag}' ${resp.status}\n`);
				process.exit(1);
			}
			const data = (await resp.json()) as { threads: { id: string }[] };
			const row = data.threads?.[0];
			if (!row) {
				process.stderr.write(
					`Error: no thread to continue on node '${nodeFlag}'. Pass --new to create one.\n`,
				);
				process.exit(1);
			}
			threadId = row.id;
		} else {
			if (!existsSync(DB_PATH)) {
				process.stderr.write(`Error: agent DB not found at ${DB_PATH}\n`);
				process.exit(1);
			}
			const db = new Database(DB_PATH, { readonly: true });
			let resolvedProjectId: string | null = null;
			if (projectFlag) {
				resolvedProjectId = resolveLocalProject(db, projectFlag);
				if (!resolvedProjectId) {
					db.close();
					process.stderr.write(
						`Error: project '${projectFlag}' not found locally. Try \`mm chat projects\`.\n`,
					);
					process.exit(1);
				}
			}
			const row = resolvedProjectId
				? db
						.query<{ id: string }, [string]>(
							'SELECT id FROM thread WHERE project_id = ? ORDER BY updated_at DESC LIMIT 1',
						)
						.get(resolvedProjectId)
				: db
						.query<{ id: string }, []>(
							'SELECT id FROM thread ORDER BY updated_at DESC LIMIT 1',
						)
						.get();
			db.close();
			if (!row) {
				process.stderr.write(
					'Error: no existing thread to continue. Pass --new to create one.\n',
				);
				process.exit(1);
			}
			threadId = row.id;
		}
	}

	const wsUrl = `${wsBase}/ws`;
	const ws = new WebSocket(wsUrl);

	let exitCode = 0;
	let streamedAnything = false;
	let statusActive = false;
	let gotTerminal = false;
	const isTty = !!process.stdout.isTTY;

	const clearStatus = () => {
		if (statusActive && isTty) {
			process.stdout.write('\r' + ' '.repeat(60) + '\r');
		}
		statusActive = false;
	};

	await new Promise<void>((resolve) => {
		const finish = () => {
			try {
				ws.close();
			} catch {}
			resolve();
		};

		ws.addEventListener('open', () => {
			const payload: Record<string, unknown> = { type: 'send', threadId, content: message };
			if (provider && modelId) {
				payload.provider = provider;
				payload.modelId = modelId;
			}
			if (projectFlag && !isNew) {
				// On --new the project was already set at create time. For continuing
				// threads, agent's setThreadProject is no-op on already-bound threads.
				payload.projectId = projectFlag;
			}
			ws.send(JSON.stringify(payload));
		});

		ws.addEventListener('message', (evt: MessageEvent) => {
			let event: Record<string, unknown>;
			try {
				event = JSON.parse(String(evt.data));
			} catch {
				return;
			}

			if (json) {
				process.stdout.write(JSON.stringify(event) + '\n');
				if (event.type === 'done') {
					gotTerminal = true;
					finish();
				} else if (event.type === 'error') {
					gotTerminal = true;
					exitCode = 1;
					finish();
				}
				return;
			}

			switch (event.type) {
				case 'delta':
					clearStatus();
					if (typeof event.text === 'string' && event.text.length > 0) {
						process.stdout.write(event.text);
						streamedAnything = true;
					}
					break;
				case 'tool_start':
					if (isTty) {
						clearStatus();
						const name = typeof event.toolName === 'string' ? event.toolName : 'tool';
						process.stdout.write(`\r· running ${name}`);
						statusActive = true;
					}
					break;
				case 'tool_end':
					clearStatus();
					break;
				case 'thinking_delta':
				case 'status':
					// Suppressed in default mode.
					break;
				case 'done': {
					clearStatus();
					const ft = typeof event.fullText === 'string' ? event.fullText : '';
					if (!streamedAnything && ft.length > 0) {
						process.stdout.write(ft);
						streamedAnything = true;
					}
					if (streamedAnything) process.stdout.write('\n');
					gotTerminal = true;
					finish();
					break;
				}
				case 'error':
					clearStatus();
					process.stderr.write(`\nError: ${event.message ?? 'unknown'}\n`);
					exitCode = 1;
					gotTerminal = true;
					finish();
					break;
			}
		});

		ws.addEventListener('error', () => {
			clearStatus();
			process.stderr.write(`\nError: WebSocket failed to ${wsUrl}\n`);
			exitCode = 1;
			finish();
		});

		ws.addEventListener('close', () => {
			if (!gotTerminal && exitCode === 0) {
				process.stderr.write('\nError: connection closed before turn completed\n');
				exitCode = 1;
			}
			resolve();
		});
	});

	process.exit(exitCode);
}

export async function chatDispatch(command: string, args: string[], flags: { json?: boolean }) {
	const json = flags?.json || false;
	switch (command) {
		case '':
		case 'list':
			await listThreads(args, json);
			break;
		case 'show':
		case 'read':
			await showThread(args, json);
			break;
		case 'search':
		case 'find':
			await searchMessages(args, json);
			break;
		case 'projects':
			await listProjects(args, json);
			break;
		case 'send':
			await sendMessage(args, flags);
			break;
		case 'nodes':
			await listNodes(json);
			break;
		case 'models':
			await listModels(args, json);
			break;
		case 'help':
		case '--help':
		case '-h':
			printChatHelp();
			break;
		default:
			process.stderr.write(`Unknown chat subcommand: ${command}\n`);
			printChatHelp();
			process.exit(1);
	}
}

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
  list [--limit N] [--project <id>]
                       Recent threads, newest first (default limit 20)
  show <id> [--limit N]
                       Print messages in a thread (default limit 50)
  search <query> [--limit N]
                       Substring match across message bodies (case-insensitive)
  projects             List known projects + thread counts
  send "<message>" [--new] [--title <t>] [--model <id>] [--thread <id>] [--project <id>]
                       Drive a turn on the local agent. Streams assistant
                       output to stdout. Without --new or --thread, continues
                       the most recently updated thread.
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

function getFlag(args: string[], name: string): string | undefined {
	const i = args.indexOf(name);
	return i >= 0 && i + 1 < args.length ? args[i + 1] : undefined;
}

function listThreads(args: string[], json: boolean) {
	const db = open();
	const limit = parseInt(getFlag(args, '--limit') || '20', 10);
	const projectId = getFlag(args, '--project');
	const rows = projectId
		? db
				.query<any, [string, number]>(
					`SELECT id, title, project_id, model_id, updated_at,
					        (SELECT COUNT(*) FROM message WHERE thread_id = thread.id) AS msg_count
					   FROM thread
					  WHERE project_id = ?
					  ORDER BY updated_at DESC LIMIT ?`,
				)
				.all(projectId, limit)
		: db
				.query<any, [number]>(
					`SELECT id, title, project_id, model_id, updated_at,
					        (SELECT COUNT(*) FROM message WHERE thread_id = thread.id) AS msg_count
					   FROM thread
					  ORDER BY updated_at DESC LIMIT ?`,
				)
				.all(limit);
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
		console.log(`${id6}  ${relTime(r.updated_at).padEnd(8)} ${r.msg_count.toString().padStart(3)}msg  ${truncate(r.title, 60)}${proj}${model}`);
	}
}

function showThread(args: string[], json: boolean) {
	const idArg = args[0];
	if (!idArg) {
		process.stderr.write('Usage: mm chat show <id>\n');
		process.exit(1);
	}
	const db = open();
	const id = resolveThreadId(db, idArg);
	if (!id) {
		process.stderr.write(`Error: thread not found: ${idArg}\n`);
		process.exit(1);
	}
	const thread = db
		.query<any, [string]>('SELECT * FROM thread WHERE id = ?')
		.get(id);
	const limit = parseInt(getFlag(args, '--limit') || '50', 10);
	const messages = db
		.query<any, [string, number]>(
			'SELECT id, role, content, created_at FROM message WHERE thread_id = ? ORDER BY created_at ASC LIMIT ?',
		)
		.all(id, limit);

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

function searchMessages(args: string[], json: boolean) {
	const query = args[0];
	if (!query) {
		process.stderr.write('Usage: mm chat search <query>\n');
		process.exit(1);
	}
	const db = open();
	const limit = parseInt(getFlag(args, '--limit') || '20', 10);
	const rows = db
		.query<any, [string, number]>(
			`SELECT m.id, m.thread_id, m.role, m.content, m.created_at, t.title
			   FROM message m
			   JOIN thread t ON t.id = m.thread_id
			  WHERE LOWER(m.content) LIKE LOWER(?)
			  ORDER BY m.created_at DESC LIMIT ?`,
		)
		.all(`%${query}%`, limit);
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

function listProjects(json: boolean) {
	const db = open();
	const rows = db
		.query<any, []>(
			`SELECT p.id, p.label, p.root_path, p.last_opened_at,
			        (SELECT COUNT(*) FROM thread WHERE project_id = p.id) AS thread_count
			   FROM project p
			  ORDER BY p.last_opened_at DESC`,
		)
		.all();
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
		console.log(`${id6}  ${r.thread_count.toString().padStart(3)}thr  ${r.label.padEnd(24)}  ${r.root_path}`);
	}
}

function hasFlag(args: string[], name: string): boolean {
	return args.includes(name);
}

async function sendMessage(args: string[], flags: { json?: boolean }) {
	const message = args[0];
	if (!message || message.startsWith('--')) {
		process.stderr.write(
			'Usage: mm chat send "<message>" [--new] [--title <t>] [--model <id>] [--thread <id>] [--project <id>]\n',
		);
		process.exit(1);
	}

	const isNew = hasFlag(args, '--new');
	const titleFlag = getFlag(args, '--title');
	const modelFlag = getFlag(args, '--model');
	const threadFlag = getFlag(args, '--thread');
	const projectFlag = getFlag(args, '--project');
	const json = flags?.json || false;

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
	} else if (isNew) {
		const body: Record<string, string> = {};
		if (titleFlag) body.title = titleFlag;
		if (projectFlag) body.project_id = projectFlag;
		try {
			const resp = await fetch(`${AGENT_BASE}/api/threads`, {
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
			process.stderr.write(`Error: agent unreachable at ${AGENT_BASE} (${err})\n`);
			process.exit(1);
		}
	} else {
		// Continue most recently updated thread, scoped to project if given.
		if (!existsSync(DB_PATH)) {
			process.stderr.write(`Error: agent DB not found at ${DB_PATH}\n`);
			process.exit(1);
		}
		const db = new Database(DB_PATH, { readonly: true });
		const row = projectFlag
			? db
					.query<{ id: string }, [string]>(
						'SELECT id FROM thread WHERE project_id = ? ORDER BY updated_at DESC LIMIT 1',
					)
					.get(projectFlag)
			: db
					.query<{ id: string }, []>('SELECT id FROM thread ORDER BY updated_at DESC LIMIT 1')
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

	const wsUrl = AGENT_BASE.replace(/^http/, 'ws') + '/ws';
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
			listThreads(args, json);
			break;
		case 'show':
		case 'read':
			showThread(args, json);
			break;
		case 'search':
		case 'find':
			searchMessages(args, json);
			break;
		case 'projects':
			listProjects(json);
			break;
		case 'send':
			await sendMessage(args, flags);
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

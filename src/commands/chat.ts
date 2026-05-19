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
  help                 Show this help

Reads ${DB_PATH} (read-only). Add --json for parseable output.

Tips:
  • Thread IDs are UUIDs; a 6-char prefix is enough for \`show\`.
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

/**
 * mm hub admin commands — sql, apps, app, health, errors, error.
 *
 * Ported from the old `meta-me.uk/cli/mm.ts`. These all hit the hub
 * Postgres directly (via `MM_DATABASE_URL`); they're meaningful only
 * to admin users who have that URL.
 */

import { db } from '../db';

// ─── Output helpers (ported verbatim from old mm.ts) ───────────────────

export function out(s: string) {
	process.stdout.write(s + '\n');
}
export function outJson(data: unknown) {
	process.stdout.write(JSON.stringify(data, null, 2) + '\n');
}
export function errExit(msg: string): never {
	process.stderr.write(`Error: ${msg}\n`);
	process.exit(1);
}
function fmtCell(v: unknown): string {
	if (v === null || v === undefined) return '';
	if (v instanceof Date) return v.toISOString().slice(0, 16).replace('T', ' ');
	if (typeof v === 'object') return JSON.stringify(v);
	return String(v);
}
function truncate(s: string, max: number): string {
	if (s.length <= max) return s;
	return s.slice(0, max - 1) + '…';
}
function escapePipes(s: string): string {
	return s.replace(/\|/g, '\\|').replace(/\n/g, ' ');
}
type Col = { key: string; label: string; max?: number; fmt?: (v: unknown, row: Record<string, unknown>) => string };
function mdTable(rows: Record<string, unknown>[], cols: Col[]): string {
	if (rows.length === 0) return '_(no rows)_';
	const cells = rows.map((r) =>
		cols.map((c) => {
			const raw = c.fmt ? c.fmt(r[c.key], r) : fmtCell(r[c.key]);
			const trimmed = c.max ? truncate(raw, c.max) : raw;
			return escapePipes(trimmed);
		}),
	);
	const widths = cols.map((c, i) => Math.max(c.label.length, ...cells.map((row) => row[i].length)));
	const fmtRow = (vals: string[]) => '| ' + vals.map((v, i) => v.padEnd(widths[i])).join(' | ') + ' |';
	const sep = '|' + widths.map((w) => '-'.repeat(w + 2)).join('|') + '|';
	return [fmtRow(cols.map((c) => c.label)), sep, ...cells.map(fmtRow)].join('\n');
}
function parseSince(s: string | undefined): Date | null {
	if (!s) return null;
	const m = s.match(/^(\d+)([smhd])$/);
	if (!m) return null;
	const n = parseInt(m[1], 10);
	const unit = m[2];
	const ms = unit === 's' ? n * 1000 : unit === 'm' ? n * 60_000 : unit === 'h' ? n * 3_600_000 : n * 86_400_000;
	return new Date(Date.now() - ms);
}

// ─── sql ───────────────────────────────────────────────────────────────

export function printSqlHelp() {
	out(`mm sql "<query>" — run arbitrary SQL against the hub DB

Connection comes from MM_DATABASE_URL / DATABASE_URL (in env or
~/.mm/.env). Adds --json for parseable output; default renders a
markdown table.

Examples:
  mm sql "SELECT slug, name FROM app ORDER BY slug"
  mm sql "SELECT * FROM error WHERE app_slug='kb' LIMIT 5" --json`);
}

export async function sqlDispatch(args: string[], flags: { json?: boolean }, dbOverride?: ReturnType<typeof db>) {
	const first = args[0];
	if (!first || first === 'help' || first === '--help' || first === '-h') {
		printSqlHelp();
		if (!first) process.exit(1);
		return;
	}
	const sql = dbOverride ?? db();
	try {
		const result = await sql.unsafe(first);
		if (flags.json) return outJson(result);
		if (!Array.isArray(result) || result.length === 0) {
			out('_(no rows)_');
			return;
		}
		const cols = Object.keys(result[0] as object).map((k) => ({ key: k, label: k, max: 60 }));
		out(mdTable(result as any[], cols));
		out(`\n_(${result.length} row${result.length === 1 ? '' : 's'})_`);
	} catch (e) {
		errExit(e instanceof Error ? e.message : String(e));
	}
}

// ─── apps + app ────────────────────────────────────────────────────────

export function printAppsHelp() {
	out(`mm apps — list all apps registered with the hub

Flags:
  --json    Parseable output`);
}

export async function appsDispatch(args: string[], flags: { json?: boolean }) {
	const first = args[0];
	if (first === 'help' || first === '--help' || first === '-h') {
		printAppsHelp();
		return;
	}
	const sql = db();
	const rows = await sql`
		SELECT
			a.slug, a.name, a.enabled, a.listed, a.sort_order,
			COALESCE(
				(SELECT array_agg(al.label_slug) FROM app_label al WHERE al.app_slug = a.slug),
				ARRAY[]::text[]
			) AS labels,
			a.features
		FROM app a
		ORDER BY a.sort_order, a.slug`;
	if (flags.json) return outJson(rows);
	out(`# Apps (${rows.length})\n`);
	out(
		mdTable(rows as any[], [
			{ key: 'slug', label: 'slug' },
			{ key: 'name', label: 'name' },
			{ key: 'enabled', label: 'on' },
			{ key: 'listed', label: 'listed' },
			{ key: 'sort_order', label: 'order' },
			{ key: 'labels', label: 'labels', fmt: (v) => ((v as string[]) ?? []).join(',') },
			{ key: 'features', label: 'features', fmt: (v) => ((v as string[]) ?? []).join(',') },
		]),
	);
}

export function printAppHelp() {
	out(`mm app <slug> [enable|disable] — inspect or toggle an app

Examples:
  mm app kb              Show the kb app's row + entitled users
  mm app kb enable       Flip enabled=true
  mm app kb --json       Raw JSON

(The full \`mm app <slug> set --field=value …\` mutate path from the
old CLI isn't ported yet — open an issue if you need it.)`);
}

export async function appDispatch(args: string[], flags: { json?: boolean }) {
	const slug = args[0];
	if (!slug || slug === 'help' || slug === '--help' || slug === '-h') {
		printAppHelp();
		if (!slug) process.exit(1);
		return;
	}
	const verb = args[1];
	const sql = db();
	if (verb === 'enable' || verb === 'disable') {
		const next = verb === 'enable';
		const updated = await sql`UPDATE app SET enabled = ${next} WHERE slug = ${slug} RETURNING slug, enabled`;
		if (updated.length === 0) errExit(`App not found: ${slug}`);
		out(`✓ ${slug} → enabled=${next}`);
		return;
	}
	const rows = await sql`SELECT * FROM app WHERE slug = ${slug}`;
	if (rows.length === 0) errExit(`App not found: ${slug}`);
	const a = rows[0] as any;
	const labels = await sql`SELECT label_slug FROM app_label WHERE app_slug = ${slug}`;
	const userCount = (await sql`SELECT COUNT(*)::int AS n FROM user_app WHERE app_slug = ${slug}`)[0] as any;
	if (flags.json) return outJson({ ...a, labels: (labels as any[]).map((l) => l.label_slug), users: userCount.n });
	out(`# ${a.name} (${a.slug})\n`);
	out(`- **enabled:** ${a.enabled} · **listed:** ${a.listed} · **default_for_new_users:** ${a.default_for_new_users}`);
	out(`- **url:** ${a.url}${a.home_path ?? ''}`);
	out(`- **users entitled:** ${userCount.n}`);
	if (a.caption) out(`- **caption:** ${a.caption}`);
	if (a.features?.length) out(`- **features:** ${a.features.join(', ')}`);
	if ((labels as any[]).length) out(`- **labels:** ${(labels as any[]).map((l) => l.label_slug).join(', ')}`);
	if (a.description) out(`\n## Description\n\n${a.description}`);
	if (a.agent_description) out(`\n## Agent description\n\n${a.agent_description}`);
}

// ─── health ────────────────────────────────────────────────────────────

export function printHealthHelp() {
	out(`mm health — quick hub stats (users, apps, recent errors, feedback)

Flags:
  --json    Parseable output`);
}

export async function healthDispatch(args: string[], flags: { json?: boolean }) {
	const first = args[0];
	if (first === 'help' || first === '--help' || first === '-h') {
		printHealthHelp();
		return;
	}
	const sql = db();
	const [users, apps, errors24h, feedback, digest24h, latest] = await Promise.all([
		sql`SELECT COUNT(*)::int AS n FROM "user"`,
		sql`SELECT COUNT(*)::int AS n FROM app`,
		sql`SELECT COUNT(*)::int AS n FROM error WHERE last_seen > NOW() - INTERVAL '24 hours'`,
		sql`SELECT COUNT(*)::int AS n FROM feedback WHERE status = 'new'`,
		sql`SELECT COUNT(*)::int AS n FROM digest WHERE occurred_at > NOW() - INTERVAL '24 hours'`,
		sql`SELECT MAX(occurred_at) AS t FROM digest`,
	]);
	const rows = [
		{ metric: 'users', value: (users[0] as any).n },
		{ metric: 'apps', value: (apps[0] as any).n },
		{ metric: 'errors (24h)', value: (errors24h[0] as any).n },
		{ metric: 'feedback (new)', value: (feedback[0] as any).n },
		{ metric: 'digest events (24h)', value: (digest24h[0] as any).n },
		{ metric: 'last digest', value: (latest[0] as any).t ? fmtCell((latest[0] as any).t) : '(none)' },
	];
	if (flags.json) return outJson(rows);
	out('# Hub health\n');
	out(mdTable(rows, [
		{ key: 'metric', label: 'metric' },
		{ key: 'value', label: 'value' },
	]));
}

// ─── errors + error ───────────────────────────────────────────────────

export function printErrorsHelp() {
	out(`mm errors — list recent errors

Flags:
  --since <n><unit>   Window (e.g. 24h, 7d). Default: 7d
  --limit <n>         Max rows. Default: 50
  --status <s>        Filter: new | triaged | resolved | wontfix | ignored
  --app <slug>        Filter by app
  --level <level>     Filter (default: error)
  --priority high     Only high-priority
  --json              Parseable output

\`mm error <fingerprint>\` for full detail.`);
}

function parseFreeformFlags(args: string[]): { positional: string[]; flags: Record<string, string | boolean> } {
	const positional: string[] = [];
	const flags: Record<string, string | boolean> = {};
	for (let i = 0; i < args.length; i++) {
		const a = args[i];
		if (!a.startsWith('--')) {
			positional.push(a);
			continue;
		}
		const eq = a.indexOf('=');
		if (eq > 0) {
			flags[a.slice(2, eq)] = a.slice(eq + 1);
		} else {
			// boolean OR next-arg-as-value
			const next = args[i + 1];
			if (next && !next.startsWith('--')) {
				flags[a.slice(2)] = next;
				i++;
			} else {
				flags[a.slice(2)] = true;
			}
		}
	}
	return { positional, flags };
}

export async function errorsDispatch(rawArgs: string[], outerFlags: Record<string, string | boolean>) {
	const first = rawArgs[0];
	if (first === 'help' || first === '--help' || first === '-h') {
		printErrorsHelp();
		return;
	}
	const { flags: parsed } = parseFreeformFlags(rawArgs);
	const flags: Record<string, string | boolean> = { ...parsed, ...outerFlags };
	const sql = db();
	const json = !!flags.json;
	const since = parseSince(flags.since as string) ?? new Date(Date.now() - 7 * 86_400_000);
	const limit = parseInt((flags.limit as string) ?? '50', 10);
	const status = flags.status as string | undefined;
	const app = flags.app as string | undefined;
	const priority = flags.priority as string | undefined;
	const level = (flags.level as string | undefined) ?? 'error';
	const rows = await sql`
		SELECT fingerprint, app_slug, surface, level, status, priority, log_quality, count, last_seen, message, notes
		FROM error
		WHERE last_seen > ${since}
			AND level = ${level}
			${status ? sql`AND status = ${status}` : sql``}
			${app ? sql`AND app_slug = ${app}` : sql``}
			${priority ? sql`AND priority = ${priority}` : sql``}
		ORDER BY (priority = 'high') DESC, last_seen DESC
		LIMIT ${limit}`;
	if (json) return outJson(rows);
	const total = (
		await sql`SELECT COUNT(*)::int AS n FROM error WHERE last_seen > ${since}
			AND level = ${level}
			${status ? sql`AND status = ${status}` : sql``}
			${app ? sql`AND app_slug = ${app}` : sql``}
			${priority ? sql`AND priority = ${priority}` : sql``}`
	)[0] as any;
	out(`# Errors (showing ${rows.length} of ${total.n}, since ${since.toISOString().slice(0, 16).replace('T', ' ')})\n`);
	out(mdTable(rows as any[], [
		{ key: 'fingerprint', label: 'fingerprint', fmt: (v) => (v as string).slice(0, 12) },
		{ key: 'app_slug', label: 'app' },
		{ key: 'priority', label: 'pri', fmt: (v) => (v === 'high' ? '🔥' : '·') },
		{ key: 'status', label: 'status' },
		{ key: 'count', label: 'n' },
		{ key: 'log_quality', label: 'log' },
		{ key: 'last_seen', label: 'last_seen' },
		{ key: 'message', label: 'message', max: 60 },
	]));
	if (rows.length > 0) out(`\n→ \`mm error <fingerprint>\` for full detail`);
}

export function printErrorHelp() {
	out(`mm error <fingerprint> [<new-status>] — inspect / triage one error

Without a status: prints full error detail (stack, payload, notes).
With a status: updates the row.

Triage statuses: new | triaged | resolved | wontfix | ignored
Optional --note "..." (appended to notes), --priority high|normal,
  --log-quality ok|missing-stack|… , --json.`);
}

export async function errorDispatch(rawArgs: string[], outerFlags: Record<string, string | boolean>) {
	const { positional, flags: parsed } = parseFreeformFlags(rawArgs);
	const fp = positional[0];
	if (!fp || fp === 'help' || fp === '--help' || fp === '-h') {
		printErrorHelp();
		if (!fp) process.exit(1);
		return;
	}
	const flags: Record<string, string | boolean> = { ...parsed, ...outerFlags };
	const sql = db();
	const json = !!flags.json;
	const newStatus = positional[1];

	if (newStatus) {
		const valid = ['new', 'triaged', 'resolved', 'wontfix', 'ignored'];
		if (!valid.includes(newStatus)) errExit(`Invalid status. One of: ${valid.join(', ')}`);
		const noteFlag = flags.note as string | undefined;
		const priorityFlag = flags.priority as string | undefined;
		const logQualityFlag = flags['log-quality'] as string | undefined;
		if (priorityFlag && !['high', 'normal'].includes(priorityFlag)) {
			errExit(`Invalid --priority. Use 'high' or 'normal'.`);
		}
		const noteLine = noteFlag ? `[${new Date().toISOString().slice(0, 10)}] ${noteFlag}` : null;
		const updated = await sql`
			UPDATE error
			SET status = ${newStatus}
				${priorityFlag ? sql`, priority = ${priorityFlag}` : sql``}
				${logQualityFlag ? sql`, log_quality = ${logQualityFlag}` : sql``}
				${noteLine ? sql`, notes = COALESCE(notes || E'\n', '') || ${noteLine}` : sql``}
			WHERE fingerprint LIKE ${fp + '%'}
			RETURNING fingerprint, status, priority, message`;
		if (updated.length === 0) errExit(`No error matching fingerprint: ${fp}`);
		if (json) return outJson(updated);
		for (const r of updated as any[]) {
			const tag = r.priority === 'high' ? '🔥' : '';
			out(`✓ ${tag}${r.fingerprint} → ${r.status}  (${truncate(r.message, 60)})`);
		}
		return;
	}

	const rows = await sql`SELECT * FROM error WHERE fingerprint LIKE ${fp + '%'} ORDER BY last_seen DESC`;
	if (rows.length === 0) errExit(`No error matching fingerprint: ${fp}`);
	if (rows.length > 1) {
		out(`# ${rows.length} matches for "${fp}":`);
		for (const r of rows as any[]) out(`- ${r.fingerprint}  ${r.app_slug}  ${r.message.slice(0, 60)}`);
		return;
	}
	const e = rows[0] as any;
	if (json) return outJson(e);
	out(`# Error ${e.fingerprint}  (${e.app_slug} ${e.surface})\n`);
	out(`- **status:** ${e.status} · **priority:** ${e.priority ?? 'normal'} · **count:** ${e.count} · **level:** ${e.level} · **env:** ${e.env}`);
	if (e.log_quality) out(`- **log_quality:** ${e.log_quality}`);
	out(`- **first_seen:** ${fmtCell(e.first_seen)} · **last_seen:** ${fmtCell(e.last_seen)}`);
	if (e.route) out(`- **route:** ${e.route}`);
	if (e.user_id) out(`- **user_id:** ${e.user_id}`);
	if (e.release_sha) out(`- **release:** ${e.release_sha}`);
	out(`\n## Message\n\n\`\`\`\n${e.message}\n\`\`\``);
	if (e.stack) out(`\n## Stack\n\n\`\`\`\n${e.stack}\n\`\`\``);
	if (e.latest_payload) out(`\n## Latest payload\n\n\`\`\`json\n${JSON.stringify(e.latest_payload, null, 2)}\n\`\`\``);
	if (e.notes) out(`\n## Notes\n\n${e.notes}`);
}

/**
 * mm run — delegate a task to Hermes + Gemini 3.5 Flash in an isolated worktree.
 *
 * Fires hermes as a detached background process and returns immediately.
 * Hermes posts results to meta-me.uk/admin/audit and (if --thread is given)
 * injects a completion message back into the originating desk chat thread.
 *
 * Usage:
 *   mm run "refactor error handling in keel" --project keel --thread <id>
 *   mm run "write tests for auth routes" --project auth.meta-me.uk
 *   mm run "..." --wait          # foreground — stream Hermes output
 */

import { spawnSync } from 'node:child_process';
import { homedir } from 'node:os';
import path from 'node:path';
import { loadConfig } from '../config';
import { hub as hubApi } from '../http/client';

const DEFAULT_MODEL = 'google/gemini-3.5-flash';
const AGENT_BASE = loadConfig().localAgentUrl ?? 'http://localhost:3142';

// ─── relative-time formatting ───────────────────────────────────────────

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

function shortId(runId: string): string {
	return runId.length <= 12 ? runId : runId.slice(0, 12);
}

// ─── helpers ────────────────────────────────────────────────────────────

function hermesExists(): boolean {
	const which = spawnSync('which', ['hermes'], { stdio: 'pipe' });
	if (which.error || !which.stdout.length) return false;
	return true;
}

type ProjectRow = { id: string; root_path: string; label: string };

async function resolveProject(labelOrPath: string): Promise<ProjectRow | null> {
	try {
		const res = await fetch(`${AGENT_BASE}/api/projects`);
		if (!res.ok) return null;
		const { projects } = (await res.json()) as { projects: ProjectRow[] };
		const norm = labelOrPath.startsWith('~')
			? path.resolve(homedir(), labelOrPath.slice(2))
			: labelOrPath;
		return (
			projects.find(
				(p) =>
					p.label.toLowerCase() === labelOrPath.toLowerCase() ||
					p.root_path === norm,
			) ?? null
		);
	} catch {
		return null;
	}
}

export async function runDispatch(args: string[], _flags: { json?: boolean }) {
	// Subcommand routing
	const sub = args[0];
	if (sub === 'list') return runList(args.slice(1), _flags);
	if (sub === 'show') return runShow(args.slice(1), _flags);

	// Collect positional spec parts (everything that isn't a flag or its value)
	const flagsWithValues = new Set(['--project', '--thread', '--model', '--skills']);
	const spec: string[] = [];
	let project: string | undefined;
	let thread: string | undefined;
	let model = DEFAULT_MODEL;
	let extraSkills: string[] = [];
	let wait = false;
	let dryRun = false;

	for (let i = 0; i < args.length; i++) {
		const a = args[i];
		if (a === '--project' || a === '-p') { project = args[++i]; continue; }
		if (a === '--thread' || a === '-t') { thread = args[++i]; continue; }
		if (a === '--model' || a === '-m') { model = args[++i]; continue; }
		if (a === '--skills' || a === '-s') { extraSkills = (args[++i] ?? '').split(','); continue; }
		if (a === '--wait') { wait = true; continue; }
		if (a === '--dry-run') { dryRun = true; continue; }
		if (!a.startsWith('-')) spec.push(a);
	}

	if (spec.length === 0) {
		console.error('Usage: mm run "<spec>" [--project <name>] [--thread <id>] [--wait]');
		process.exit(1);
	}

	// Resolve cwd: registered project root > cwd
	let cwd = process.cwd();
	if (project) {
		const row = await resolveProject(project);
		if (!row) {
			console.error(`Project "${project}" not found. Run \`mm project list\` to see registered projects.`);
			process.exit(1);
		}
		cwd = row.root_path;
	}

	// Build the prompt — prepend MM_THREAD_ID marker if we have one
	const threadMarker = thread ? `MM_THREAD_ID=${thread} ` : '';
	const prompt = `${threadMarker}${spec.join(' ')}`;

	// Build skill list: meta-me is always included
	const skills = ['meta-me', ...extraSkills.filter((s) => s && s !== 'meta-me')].join(',');

	// Assemble Hermes invocation.
	// -m only applies to --oneshot and --tui; use the env var for chat -q.
	const hermesArgs = [
		'--worktree',
		'--yolo',
		'--accept-hooks',
		'--pass-session-id',
		'-s', skills,
		'chat',
		'-q', prompt,
	];
	const hermesEnv = {
		...process.env,
		HERMES_INFERENCE_MODEL: model,
	};

	if (dryRun) {
		console.log(`HERMES_INFERENCE_MODEL=${model} hermes ` + hermesArgs.map((a) => (a.includes(' ') ? `"${a}"` : a)).join(' '));
		console.log(`cwd: ${cwd}`);
		return;
	}

	if (wait) {
		// Foreground — stream output
		const result = spawnSync('hermes', hermesArgs, { cwd, stdio: 'inherit', env: hermesEnv });
		if (result.error) {
			console.error('Failed to spawn hermes:', result.error.message);
			process.exit(1);
		}
		process.exit(result.status ?? 0);
	}

	// Background — detached, inherits nothing
	if (!hermesExists()) {
		console.error('hermes not found on PATH. Make sure Hermes Agent is installed.');
		process.exit(1);
	}
	const { spawn } = await import('node:child_process');
	const child = spawn('hermes', hermesArgs, {
		cwd,
		detached: true,
		stdio: 'ignore',
		env: hermesEnv,
	});
	child.unref();

	const modelLabel = model.split('/').pop();
	const projectLabel = project ?? path.basename(cwd);
	console.log(`▶ Hermes running in background (${modelLabel}, worktree from ${projectLabel})`);
	if (thread) console.log(`  Will notify thread ${thread} on completion.`);
	console.log(`  Results → https://meta-me.uk/admin/audit`);
}

// ─── run list ───────────────────────────────────────────────────────────

interface AuditListItem {
	runId: string;
	createdAt: string;
	status: string;
	appSlugs: string[];
	totalGapsFound: number;
	totalGapsFixed: number;
	totalFilesChecked: number;
	lookback: string;
	mode: string;
	report: string | null;
}

async function runList(args: string[], flags: { json?: boolean }) {
	let limit = 20;
	for (let i = 0; i < args.length; i++) {
		if ((args[i] === '--limit' || args[i] === '-n') && i + 1 < args.length) {
			limit = parseInt(args[++i], 10) || 20;
		}
	}

	let data: { runs: AuditListItem[] };
	try {
		data = await hubApi<{ runs: AuditListItem[] }>('audit', 'list', { limit });
	} catch (err: any) {
		console.error(err.message);
		process.exit(1);
	}

	if (flags.json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}

	if (data.runs.length === 0) {
		console.log('No runs found.');
		return;
	}

	// Header
	console.log(`${'WHEN'.padEnd(9)} ${'RUN'.padEnd(14)} ${'STATUS'.padEnd(9)} ${'GAPS'.padEnd(7)} ${'APPS'.padEnd(18)} SUMMARY`);
	console.log(`${'─'.repeat(9)} ${'─'.repeat(14)} ${'─'.repeat(9)} ${'─'.repeat(7)} ${'─'.repeat(18)} ${'─'.repeat(50)}`);

	for (const run of data.runs) {
		const when = relTime(new Date(run.createdAt).getTime());
		const rid = shortId(run.runId);
		const status = run.status === 'reviewed' ? '✓' : '●';
		const gaps = `${run.totalGapsFound}/${run.totalGapsFixed}`;
		const apps = run.appSlugs.slice(0, 3).join(',');
		const appsLabel = apps.length <= 18 ? apps : apps.slice(0, 17) + '…';
		const summary = (run.report ?? '').slice(0, 50);

		console.log(`${when.padEnd(9)} ${rid.padEnd(14)} ${status.padEnd(9)} ${gaps.padEnd(7)} ${appsLabel.padEnd(18)} ${summary}`);
	}
}

// ─── run show ───────────────────────────────────────────────────────────

interface AuditShowRow {
	id: string;
	appSlug: string;
	createdAt: string;
	lookback: string;
	mode: string;
	filesChecked: number;
	gapsFound: number;
	gapsFixed: number;
	highCount: number;
	mediumCount: number;
	lowCount: number;
	report: string | null;
	status: string;
}

async function runShow(args: string[], flags: { json?: boolean }) {
	const runId = args[0]?.trim();
	if (!runId) {
		console.error('Usage: mm run show <run-id>');
		process.exit(1);
	}

	let data: { runId: string; createdAt: string; status: string; rows: AuditShowRow[] };
	try {
		data = await hubApi<{ runId: string; createdAt: string; status: string; rows: AuditShowRow[] }>(
			'audit', 'show', { runId }
		);
	} catch (err: any) {
		console.error(err.message);
		process.exit(1);
	}

	if (flags.json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}

	const dt = new Date(data.createdAt);
	const statusIcon = data.status === 'reviewed' ? '✓ reviewed' : '● new';

	console.log(`Run:      ${data.runId}`);
	console.log(`When:     ${dt.toLocaleString()} ${statusIcon}`);
	if (data.rows.length > 0) {
		console.log(`Lookback: ${data.rows[0].lookback}`);
		console.log(`Mode:     ${data.rows[0].mode}`);
	}
	console.log('');

	for (const row of data.rows) {
		console.log(`── ${row.appSlug} ───────────────────────────────────────`);
		console.log(`  Files checked: ${row.filesChecked}  Gaps found: ${row.gapsFound}  Fixed: ${row.gapsFixed}`);
		if (row.highCount > 0 || row.mediumCount > 0 || row.lowCount > 0) {
			console.log(`  Severity: high=${row.highCount} medium=${row.mediumCount} low=${row.lowCount}`);
		}
		if (row.report) {
			console.log(`  Report:`);
			for (const line of row.report.split('\n')) {
				console.log(`    ${line}`);
			}
		}
		console.log('');
	}
}

export function printRunHelp() {
	console.log(`mm run — delegate tasks to Hermes and review results

Subcommands:
  run \"<spec>\" [options]    Fire a Hermes run in an isolated worktree
  run list [--limit N] [--json]
                             Show recent runs, newest first (default limit 20)
  run show <run-id> [--json]
                             Show full detail for a single run (accepts prefix)

Run options:
  --project <name|path>   Project to work in (resolved from registered projects).
                          Sets the cwd for Hermes — worktree is branched from here.
  --thread <id>           Desk chat thread ID. Hermes injects a completion message
                          when done (\"results posted to admin/audit\").
  --model <id>            Model override (default: ${DEFAULT_MODEL})
  --skills <s1,s2>        Extra Hermes skills to preload (meta-me is always loaded)
  --wait                  Run in foreground and stream Hermes output (default: background)
  --dry-run               Print the Hermes command without running it

Examples:
  mm run \"refactor error handling in keel's API routes\" --project keel --thread abc123
  mm run list
  mm run list --limit 5 --json
  mm run show hermes-bb44

Hermes works in an isolated git worktree, posts a structured report to
https://meta-me.uk/admin/audit, and notifies the desk thread when done.
\`mm run list\` and \`mm run show\` read those reports from the CLI.
`);
}

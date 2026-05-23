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

const DEFAULT_MODEL = 'google/gemini-3.5-flash';
const AGENT_BASE = loadConfig().localAgentUrl ?? 'http://localhost:3142';

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

	// Assemble Hermes invocation
	const hermesArgs = [
		'--worktree',
		'--yolo',
		'--accept-hooks',
		'-m', model,
		'--pass-session-id',
		'-s', skills,
		'chat',
		'-q', prompt,
	];

	if (dryRun) {
		console.log('hermes ' + hermesArgs.map((a) => (a.includes(' ') ? `"${a}"` : a)).join(' '));
		console.log(`cwd: ${cwd}`);
		return;
	}

	if (wait) {
		// Foreground — stream output
		const result = spawnSync('hermes', hermesArgs, { cwd, stdio: 'inherit' });
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
	});
	child.unref();

	const modelLabel = model.split('/').pop();
	const projectLabel = project ?? path.basename(cwd);
	console.log(`▶ Hermes running in background (${modelLabel}, worktree from ${projectLabel})`);
	if (thread) console.log(`  Will notify thread ${thread} on completion.`);
	console.log(`  Results → https://meta-me.uk/admin/audit`);
}

export function printRunHelp() {
	console.log(`mm run — delegate a task to Hermes + Gemini 3.5 Flash in an isolated worktree

Usage:
  mm run "<spec>" [options]

Options:
  --project <name|path>   Project to work in (resolved from registered projects).
                          Sets the cwd for Hermes — worktree is branched from here.
  --thread <id>           Desk chat thread ID. Hermes injects a completion message
                          when done ("results posted to admin/audit").
  --model <id>            Model override (default: ${DEFAULT_MODEL})
  --skills <s1,s2>        Extra Hermes skills to preload (meta-me is always loaded)
  --wait                  Run in foreground and stream Hermes output (default: background)
  --dry-run               Print the Hermes command without running it

Examples:
  mm run "refactor error handling in keel's API routes" --project keel --thread abc123
  mm run "write unit tests for src/lib/auth.ts" --project auth.meta-me.uk --wait
  mm run "audit dependencies for outdated packages" --dry-run

Hermes works in an isolated git worktree, posts a structured report to
https://meta-me.uk/admin/audit, and notifies the desk thread when done.
`);
}

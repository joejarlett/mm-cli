/**
 * mm tasks — read + write Google Tasks across all the user's lists.
 *
 *   mm tasks                           due in next 7 days, all lists (= `list`)
 *   mm tasks list [--days N] [--all]
 *   mm tasks add "Buy milk" [--due "tomorrow"] [--list "Home"] [--notes "..."]
 *   mm tasks done <task-id> [--list-id <list-id>]
 *
 * `--due` accepts natural language ("tomorrow", "next friday") or
 * ISO `YYYY-MM-DD`. Google Tasks stores due as a date only.
 */
import { hubApi } from '../hub';
import { parseNlDate } from '../nl-date';

type Task = {
	id: string;
	title: string;
	notes?: string;
	due?: string;
	status: 'needsAction' | 'completed';
	webViewLink?: string;
};

type Group = { listId: string; listTitle: string; tasks: Task[] };
type ListResp = { groups: Group[]; accountSlug: string | null };
type AddResp = { id: string; listId: string; listTitle: string };

export async function tasksDispatch(
	command: string,
	args: string[],
	flags: { json?: boolean }
) {
	const json = flags?.json || false;
	if (command.startsWith('--')) {
		args = [command, ...args];
		command = 'list';
	}
	switch (command) {
		case '':
		case 'list':
		case 'ls':
			return tasksList(args, json);
		case 'add':
		case 'new':
			return tasksAdd(args, json);
		case 'done':
		case 'complete':
			return tasksDone(args, json);
		default:
			console.error(`Unknown command: mm tasks ${command}`);
			process.exit(1);
	}
}

function parseFlags(args: string[]): { flags: Record<string, string>; positional: string[] } {
	const flags: Record<string, string> = {};
	const positional: string[] = [];
	for (let i = 0; i < args.length; i++) {
		const a = args[i];
		const eq = a.match(/^--([a-z][a-z-]*)=(.+)$/s);
		if (eq) {
			flags[eq[1]] = eq[2];
			continue;
		}
		const longFlag = a.match(/^--([a-z][a-z-]*)$/);
		if (longFlag) {
			// Boolean if next arg is also a flag, else value.
			const next = args[i + 1];
			if (next === undefined || next.startsWith('--')) {
				flags[longFlag[1]] = 'true';
			} else {
				flags[longFlag[1]] = next;
				i++;
			}
			continue;
		}
		positional.push(a);
	}
	return { flags, positional };
}

function fmtDue(iso: string | undefined): string {
	if (!iso) return '';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return iso;
	return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
}

async function tasksList(args: string[], json: boolean) {
	const { flags } = parseFlags(args);
	const payload: Record<string, unknown> = {};
	if (flags.days) payload.days = Number(flags.days);
	if (flags.all) payload.all = true;
	if (flags.account) payload.accountSlug = flags.account;

	const data = await hubApi<ListResp>('tasks', 'list', payload);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}

	const total = data.groups.reduce((n, g) => n + g.tasks.length, 0);
	if (total === 0) {
		console.log('Nothing pending. ✨');
		return;
	}

	for (const g of data.groups) {
		if (g.tasks.length === 0) continue;
		console.log(`\n${g.listTitle}  (${g.tasks.length})`);
		for (const t of g.tasks) {
			const due = t.due ? `  due ${fmtDue(t.due)}` : '';
			console.log(`  · ${t.title}${due}`);
			console.log(`      id=${t.id}  list=${g.listId}`);
			if (t.notes) console.log(`      ${t.notes.split('\n').join('\n      ')}`);
		}
	}
}

async function tasksAdd(args: string[], json: boolean) {
	const { flags, positional } = parseFlags(args);
	const title = positional.join(' ').trim();
	if (!title) {
		console.error('Usage: mm tasks add "<title>" [--due YYYY-MM-DD] [--list "List name"] [--notes "..."]');
		process.exit(1);
	}
	const payload: Record<string, unknown> = { title };
	let dueDisplay: string | undefined;
	if (flags.due) {
		try {
			const parsed = parseNlDate(flags.due);
			payload.due = parsed.iso;
			dueDisplay = parsed.iso;
		} catch (err) {
			console.error(`✗ ${err instanceof Error ? err.message : err}`);
			process.exit(1);
		}
	}
	if (flags.list) payload.listTitle = flags.list;
	if (flags['list-id']) payload.listId = flags['list-id'];
	if (flags.notes) payload.notes = flags.notes;
	if (flags.account) payload.accountSlug = flags.account;

	const data = await hubApi<AddResp>('tasks', 'add', payload);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log(`✓ Added to ${data.listTitle}`);
	console.log(`  ${title}`);
	if (dueDisplay) console.log(`  due ${dueDisplay}`);
}

async function tasksDone(args: string[], json: boolean) {
	const { flags, positional } = parseFlags(args);
	const taskId = positional[0];
	if (!taskId) {
		console.error('Usage: mm tasks done <task-id> [--list-id <list-id>]');
		console.error('       (run `mm tasks` first to see ids)');
		process.exit(1);
	}
	const listId = flags['list-id'];
	if (!listId) {
		console.error('Missing --list-id. `mm tasks` shows ids for each task — the line below the title.');
		process.exit(1);
	}

	const data = await hubApi<{ ok: true }>('tasks', 'complete', {
		listId,
		taskId,
		...(flags.account ? { accountSlug: flags.account } : {})
	});
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log('✓ Done.');
}

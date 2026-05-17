/**
 * mm calendar — read + write the active Google account's calendar.
 *
 *   mm calendar                 next 7 days, agenda (alias for `list`)
 *   mm calendar list [--days N] [--q "search"]
 *   mm calendar new --title "X" --when "tomorrow 14:00" [--end "15:00"]
 *                   [--at "Brunel"] [--invite a@x.com,b@y.com] [--describe "..."]
 *
 * `--when` accepts natural language ("tomorrow 14:00", "next monday 10am",
 * "in 2 hours") or ISO ("2026-05-20 14:00"). Parsed locally in your TZ.
 */
import { hubApi } from '../hub';
import { parseNlDateTime } from '../nl-date';

type EventRow = {
	id: string;
	summary: string;
	start: string;
	end: string;
	location: string | null;
	htmlLink: string | null;
	attendees: number;
	allDay: boolean;
};

type ListResp = { events: EventRow[]; rangeDays: number; accountSlug: string | null };

type CreateResp = { id: string; htmlLink: string | null; summary: string; start: string; end: string };

export async function calendarDispatch(
	command: string,
	args: string[],
	flags: { json?: boolean }
) {
	const json = flags?.json || false;
	// If the user typed `mm calendar --days 7` (no subcommand), the first
	// arg landed in `command`. Hoist it back into args and default to list.
	if (command.startsWith('--')) {
		args = [command, ...args];
		command = 'list';
	}
	switch (command) {
		case '':
		case 'list':
		case 'ls':
			return calendarList(args, json);
		case 'new':
		case 'create':
			return calendarNew(args, json);
		default:
			console.error(`Unknown command: mm calendar ${command}`);
			process.exit(1);
	}
}

function parseFlags(args: string[]): Record<string, string> {
	const out: Record<string, string> = {};
	for (let i = 0; i < args.length; i++) {
		const a = args[i];
		const eq = a.match(/^--([a-z][a-z-]*)=(.+)$/s);
		if (eq) {
			out[eq[1]] = eq[2];
			continue;
		}
		const long = a.match(/^--([a-z][a-z-]*)$/);
		if (long && i + 1 < args.length) {
			out[long[1]] = args[++i];
		}
	}
	return out;
}

function fmtDay(iso: string): string {
	if (!iso) return '—';
	const d = new Date(iso);
	return d.toLocaleDateString('en-GB', { weekday: 'short', day: 'numeric', month: 'short' });
}

function fmtTime(iso: string, allDay: boolean): string {
	if (allDay) return 'all-day';
	if (!iso) return '—';
	const d = new Date(iso);
	return d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
}

async function calendarList(args: string[], json: boolean) {
	const flags = parseFlags(args);
	const payload: Record<string, unknown> = {};
	if (flags.days) payload.days = Number(flags.days);
	if (flags.q) payload.q = flags.q;

	const data = await hubApi<ListResp>('calendar', 'list', payload);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}

	if (data.events.length === 0) {
		console.log(`No events in the next ${data.rangeDays} days.`);
		return;
	}

	// Group by day
	const groups = new Map<string, EventRow[]>();
	for (const e of data.events) {
		const key = (e.start || '').slice(0, 10) || 'unscheduled';
		const arr = groups.get(key) ?? [];
		arr.push(e);
		groups.set(key, arr);
	}
	for (const [key, evs] of [...groups.entries()].sort()) {
		const head = key === 'unscheduled' ? 'Unscheduled' : fmtDay(key);
		console.log(`\n${head}`);
		for (const e of evs) {
			const t = fmtTime(e.start, e.allDay);
			const loc = e.location ? `  📍 ${e.location}` : '';
			const att = e.attendees ? `  👥 ${e.attendees}` : '';
			console.log(`  ${t.padEnd(10)} ${e.summary}${loc}${att}`);
		}
	}
}

async function calendarNew(args: string[], json: boolean) {
	const flags = parseFlags(args);
	const title = flags.title;
	const when = flags.when;
	if (!title || !when) {
		console.error('Usage: mm calendar new --title "X" --when "tomorrow 14:00" [--end "15:00"] [--at "..."] [--invite a@x.com,b@y.com] [--describe "..."]');
		process.exit(1);
	}

	let parsedWhen: string;
	let parsedEnd: string | undefined;
	try {
		const start = parseNlDateTime(when);
		parsedWhen = start.iso;
		if (flags.end) {
			// `--end` is permitted as a bare time ("15:00") relative to the
			// parsed start date, or as its own NL/ISO string.
			const justTime = flags.end.match(/^(\d{1,2}):(\d{2})$/);
			if (justTime) {
				const endDate = new Date(start.date);
				endDate.setHours(Number(justTime[1]), Number(justTime[2]), 0, 0);
				parsedEnd = parseNlDateTime(
					`${endDate.getFullYear()}-${String(endDate.getMonth() + 1).padStart(2, '0')}-${String(endDate.getDate()).padStart(2, '0')} ${flags.end}`
				).iso;
			} else {
				parsedEnd = parseNlDateTime(flags.end).iso;
			}
		}
	} catch (err) {
		console.error(`✗ ${err instanceof Error ? err.message : err}`);
		process.exit(1);
	}

	const attendees =
		flags.invite
			?.split(',')
			.map((s) => s.trim())
			.filter(Boolean) ?? [];

	const payload: Record<string, unknown> = {
		title,
		when: parsedWhen,
		...(parsedEnd ? { end: parsedEnd } : {}),
		...(flags.at ? { location: flags.at } : {}),
		...(flags.describe ? { description: flags.describe } : {}),
		...(attendees.length ? { attendees } : {}),
		...(flags.notify ? { sendUpdates: flags.notify } : {}),
		...(flags.account ? { accountSlug: flags.account } : {})
	};

	const data = await hubApi<CreateResp>('calendar', 'create', payload);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log(`✓ Created: ${data.summary}`);
	console.log(`  ${fmtDay(data.start)} ${fmtTime(data.start, false)}–${fmtTime(data.end, false)}`);
	if (data.htmlLink) console.log(`  ${data.htmlLink}`);
}

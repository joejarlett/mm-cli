/**
 * Natural-language date parsing for CLI `--when` / `--due` flags.
 *
 * Accepts:
 *   "2026-05-20 14:00"         → strict
 *   "tomorrow 14:00"           → chrono
 *   "next monday 10am"         → chrono
 *   "in 2 hours"               → chrono
 *   "fri"                      → chrono (defaults to start of business day)
 *
 * Returns a wall-clock ISO string `YYYY-MM-DDTHH:MM:SS` in the caller's
 * local timezone — Google's `dateTime` + `timeZone` pair handles offset.
 * Throws on unparseable input so the caller can surface a clear error.
 */
import * as chrono from 'chrono-node';

function pad(n: number): string {
	return String(n).padStart(2, '0');
}

function toLocalDateTime(d: Date): string {
	return (
		`${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
		`T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
	);
}

function toLocalDate(d: Date): string {
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function parseNlDateTime(raw: string): { iso: string; date: Date } {
	const trimmed = raw.trim();
	if (!trimmed) throw new Error('empty date');

	const direct = new Date(trimmed);
	if (!Number.isNaN(direct.getTime()) && /\d{4}-\d{2}-\d{2}/.test(trimmed)) {
		return { iso: toLocalDateTime(direct), date: direct };
	}

	const results = chrono.parse(trimmed, new Date(), { forwardDate: true });
	if (results.length === 0) {
		throw new Error(`couldn't parse "${raw}" as a date/time`);
	}
	const r = results[0];
	const d = r.start.date();
	return { iso: toLocalDateTime(d), date: d };
}

export function parseNlDate(raw: string): { iso: string; date: Date } {
	const trimmed = raw.trim();
	if (!trimmed) throw new Error('empty date');

	if (/^\d{4}-\d{2}-\d{2}$/.test(trimmed)) {
		const d = new Date(`${trimmed}T00:00:00`);
		return { iso: trimmed, date: d };
	}

	const results = chrono.parse(trimmed, new Date(), { forwardDate: true });
	if (results.length === 0) {
		throw new Error(`couldn't parse "${raw}" as a date`);
	}
	const d = results[0].start.date();
	return { iso: toLocalDate(d), date: d };
}

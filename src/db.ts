/**
 * Postgres client for the hub DB — used by the `mm sql / apps / errors`
 * etc. subcommands ported from the old `meta-me.uk/cli/mm.ts`.
 *
 * Connection string priority:
 *   1. MM_DATABASE_URL  (lets the CLI target a host-local DB while an
 *                       app's DATABASE_URL points at a docker-internal
 *                       hostname)
 *   2. DATABASE_URL     (fallback)
 *
 * Looked up from process.env. The local agent injects ~/.mm/.env into
 * its own process env at boot, and the bash tool inherits that, so the
 * agent's `mm sql` calls find the URL without any extra wiring.
 */

import postgres from 'postgres';
import { existsSync, readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

let booted = false;

// Best-effort env merge from ~/.mm/.env so the CLI works when invoked
// from a shell that hasn't sourced it. No override — explicit env wins.
function maybeLoadUserEnv() {
	if (booted) return;
	booted = true;
	const path = join(homedir(), '.mm', '.env');
	if (!existsSync(path)) return;
	for (const line of readFileSync(path, 'utf-8').split('\n')) {
		const trimmed = line.trim();
		if (!trimmed || trimmed.startsWith('#')) continue;
		const eq = trimmed.indexOf('=');
		if (eq < 0) continue;
		const key = trimmed.slice(0, eq).replace(/^export\s+/, '').trim();
		let value = trimmed.slice(eq + 1).trim();
		if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
			value = value.slice(1, -1);
		}
		if (!key || key in process.env) continue;
		process.env[key] = value;
	}
}

let _sql: ReturnType<typeof postgres> | null = null;

export function db() {
	if (_sql) return _sql;
	maybeLoadUserEnv();
	const url = process.env.MM_DATABASE_URL || process.env.DATABASE_URL;
	if (!url) {
		process.stderr.write('Error: MM_DATABASE_URL or DATABASE_URL not set (env or ~/.mm/.env)\n');
		process.exit(1);
	}
	_sql = postgres(url, {
		max: 2,
		idle_timeout: 10,
		connect_timeout: 5,
		ssl:
			url.includes('localhost') || url.includes('127.0.0.1')
				? false
				: { rejectUnauthorized: false },
	});
	return _sql;
}

export async function shutdown() {
	if (_sql) await _sql.end({ timeout: 2 });
}

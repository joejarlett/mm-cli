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
import { loadConfig } from './config';

let _sql: ReturnType<typeof postgres> | null = null;

export function db() {
	if (_sql) return _sql;
	const url = loadConfig().databaseUrl;
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

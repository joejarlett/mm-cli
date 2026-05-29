/**
 * Postgres clients for the hub DB and KB DB.
 *
 * Hub DB connection string priority:
 *   1. MM_DATABASE_URL  (lets the CLI target a host-local DB while an
 *                       app's DATABASE_URL points at a docker-internal
 *                       hostname)
 *   2. DATABASE_URL     (fallback)
 *
 * KB DB: MM_KB_DATABASE_URL
 *
 * Looked up from process.env. The local agent injects ~/.mm/.env into
 * its own process env at boot, and the bash tool inherits that, so the
 * agent's `mm sql` calls find the URL without any extra wiring.
 */

import postgres from 'postgres';
import { loadConfig } from './config';

function makeDb(url: string) {
	return postgres(url, {
		max: 2,
		idle_timeout: 10,
		connect_timeout: 5,
		ssl: url.includes('localhost') || url.includes('127.0.0.1') ? false : { rejectUnauthorized: false },
	});
}

let _sql: ReturnType<typeof postgres> | null = null;

export function db() {
	if (_sql) return _sql;
	const url = loadConfig().databaseUrl;
	if (!url) {
		process.stderr.write('Error: MM_DATABASE_URL or DATABASE_URL not set (env or ~/.mm/.env)\n');
		process.exit(1);
	}
	_sql = makeDb(url);
	return _sql;
}

let _kbSql: ReturnType<typeof postgres> | null = null;

export function kbDb() {
	if (_kbSql) return _kbSql;
	const url = loadConfig().kbDatabaseUrl;
	if (!url) {
		process.stderr.write('Error: MM_KB_DATABASE_URL not set (env or ~/.mm/.env)\n');
		process.exit(1);
	}
	_kbSql = makeDb(url);
	return _kbSql;
}

export async function shutdown() {
	await Promise.all([
		_sql?.end({ timeout: 2 }),
		_kbSql?.end({ timeout: 2 }),
	]);
}

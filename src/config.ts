/**
 * mm-cli configuration — env vars + defaults in one place.
 *
 * Replaces ad-hoc `process.env.X` reads scattered across hub.ts,
 * dispatcher.ts, http/client.ts, db.ts, commands/stt.ts, commands/tts.ts,
 * commands/chat.ts. Single struct, loaded once on import.
 *
 * Env var convention: `MM_<NAME>`. Legacy `DATABASE_URL` is honoured as
 * a fallback for admin DB connections (existing servers ship with it).
 *
 * The Go port will mirror this as one `Config` struct loaded at startup.
 */

import { existsSync, readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

export interface Config {
	/** Hub base URL (`meta-me.uk` in prod). Used by `/api/mm`, `/api/stt`, `/api/tts`. */
	hubUrl: string;
	/** Auth service base URL (`auth.meta-me.uk`). Used by device-flow login. */
	authUrl: string;
	/** Local agent base URL (`localhost:3142`). Used by `mm chat` + `mm project`. */
	localAgentUrl: string;
	/** Hub Postgres connection string. Optional — only admin commands need it. */
	databaseUrl: string | undefined;
}

/**
 * Best-effort merge of `~/.mm/.env` into `process.env`. The local agent
 * injects `~/.mm/.env` into its own process env at boot; the bash tool
 * inherits that, so the agent's `mm` calls already find the URL. This
 * is for human-driven invocations from a shell that hasn't sourced it.
 *
 * No override — explicit env wins.
 */
let envLoaded = false;
function maybeLoadUserEnv(): void {
	if (envLoaded) return;
	envLoaded = true;
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

let cached: Config | null = null;

export function loadConfig(): Config {
	if (cached) return cached;
	maybeLoadUserEnv();
	cached = {
		hubUrl: process.env.MM_HUB_URL ?? 'https://meta-me.uk',
		authUrl: process.env.MM_AUTH_URL ?? 'https://auth.meta-me.uk',
		localAgentUrl: process.env.MM_LOCAL_AGENT_URL ?? 'http://localhost:3142',
		databaseUrl: process.env.MM_DATABASE_URL ?? process.env.DATABASE_URL,
	};
	return cached;
}

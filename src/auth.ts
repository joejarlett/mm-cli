/**
 * Auth module — token storage and retrieval.
 *
 * Token format: mm_<40 hex chars>
 * Storage: ~/.config/mm/auth.json → { token, prefix, userId, userName, createdAt }
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

const MM_DIR = join(homedir(), '.config', 'mm');

export interface AuthState {
	token: string;
	prefix: string;      // first 8 chars of token
	userId: string;      // resolved from validate endpoint
	userName: string;
	userEmail: string;
	createdAt: string;
}

function ensureDir() {
	if (!existsSync(MM_DIR)) {
		mkdirSync(MM_DIR, { recursive: true });
	}
}

export function getAuthPath(): string {
	return join(MM_DIR, 'auth.json');
}

export function loadAuth(): AuthState | null {
	const path = getAuthPath();
	if (!existsSync(path)) return null;
	try {
		const raw = readFileSync(path, 'utf-8');
		return JSON.parse(raw) as AuthState;
	} catch {
		return null;
	}
}

export function saveAuth(state: AuthState): void {
	ensureDir();
	const path = getAuthPath();
	writeFileSync(path, JSON.stringify(state, null, 2), { mode: 0o600 });
}

export function clearAuth(): void {
	const path = getAuthPath();
	if (existsSync(path)) {
		writeFileSync(path, '', { mode: 0o600 });
	}
}

export function getConfigDir(): string {
	ensureDir();
	return MM_DIR;
}

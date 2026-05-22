/**
 * Agent Card fetcher + cache.
 *
 * Each app publishes a Card at `<app>/.well-known/agent.json` describing
 * its agent-facing surface: description, capabilities, curated tools
 * with MCP annotations. This is the human/agent-readable view of the
 * app — richer than the raw manifest for discovery, sparser for
 * dispatch.
 *
 * Cache: `~/.mm-cli/cards/<slug>.json`, 24h TTL, `--refresh` to bust.
 *
 * Contract: meta-me.uk/specs/cross-app-communication.md §4.3.
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { resolveApp } from './apps';

const CACHE_DIR = join(homedir(), '.mm-cli', 'cards');
const CACHE_TTL_MS = 24 * 60 * 60 * 1000;

export type Capability = 'ask' | 'chat' | 'search' | 'writes';

export interface AgentCardTool {
	name: string;
	description?: string;
	readOnlyHint?: boolean;
	destructiveHint?: boolean;
	idempotentHint?: boolean;
	openWorldHint?: boolean;
}

export interface AgentCardAlias {
	feature: string;
	action: string;
	description?: string;
}

export interface AgentCard {
	name: string;
	description?: string;
	version?: string;
	capabilities?: Capability[];
	chatUrl?: string;
	mcpUrl?: string | null;
	tools?: AgentCardTool[];
	aliases?: Record<string, AgentCardAlias>;
	auth?: string[];
}

function ensureDir() {
	if (!existsSync(CACHE_DIR)) mkdirSync(CACHE_DIR, { recursive: true });
}

function cachePath(slug: string): string {
	return join(CACHE_DIR, `${slug}.json`);
}

function isFresh(path: string): boolean {
	try {
		return Date.now() - statSync(path).mtimeMs < CACHE_TTL_MS;
	} catch {
		return false;
	}
}

export async function fetchAgentCard(slug: string): Promise<AgentCard> {
	const app = resolveApp(slug);
	const url = `${app.url}/.well-known/agent.json`;
	const res = await fetch(url, {
		headers: { accept: 'application/json' },
		redirect: 'follow'
	});
	if (!res.ok) {
		throw new Error(`Agent Card fetch ${url} failed: HTTP ${res.status}`);
	}
	const card = (await res.json()) as AgentCard;
	if (!card.name) {
		throw new Error(`Agent Card from ${url} is malformed: missing 'name'`);
	}
	return card;
}

export async function loadAgentCard(
	slug: string,
	opts: { refresh?: boolean } = {}
): Promise<AgentCard> {
	ensureDir();
	const path = cachePath(slug);

	if (!opts.refresh && existsSync(path) && isFresh(path)) {
		try {
			return JSON.parse(readFileSync(path, 'utf-8')) as AgentCard;
		} catch {
			// corrupt — refetch
		}
	}

	const card = await fetchAgentCard(slug);
	writeFileSync(path, JSON.stringify(card, null, 2));
	return card;
}

export function hasCapability(card: AgentCard, cap: Capability): boolean {
	return Array.isArray(card.capabilities) && card.capabilities.includes(cap);
}

export function findTool(card: AgentCard, name: string): AgentCardTool | undefined {
	if (!Array.isArray(card.tools)) return undefined;
	return card.tools.find((t) => t.name === name);
}

export function findAlias(card: AgentCard, verb: string): AgentCardAlias | undefined {
	return card.aliases?.[verb];
}

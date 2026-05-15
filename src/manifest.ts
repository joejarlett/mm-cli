/**
 * Manifest fetcher + cache.
 *
 * Each app publishes a manifest at `<app>/api/v2/manifest` describing
 * the full feature.action surface (auth modes + Zod-derived JSON schemas
 * for input/output). The CLI caches manifests under
 * `~/.mm-cli/manifests/<slug>.json` so subsequent commands can
 * pre-validate input + render --help text without a network round trip.
 *
 * Cache invalidation: `mm manifest <app> --refresh` or 24h TTL.
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { resolveApp } from './apps';

const CACHE_DIR = join(homedir(), '.mm-cli', 'manifests');
const CACHE_TTL_MS = 24 * 60 * 60 * 1000; // 24h

export type AuthMode = 'session' | 'hub' | 'either' | 'public' | 'install';

export interface ManifestAction {
	auth: AuthMode;
	description?: string;
	input: Record<string, unknown> | 'Response';
	output: Record<string, unknown> | 'Response';
}

export interface AppManifest {
	appSlug: string;
	version: string;
	features: Record<string, Record<string, ManifestAction>>;
}

function ensureDir() {
	if (!existsSync(CACHE_DIR)) {
		mkdirSync(CACHE_DIR, { recursive: true });
	}
}

function cachePath(slug: string): string {
	return join(CACHE_DIR, `${slug}.json`);
}

function isCacheFresh(path: string): boolean {
	try {
		const age = Date.now() - statSync(path).mtimeMs;
		return age < CACHE_TTL_MS;
	} catch {
		return false;
	}
}

/**
 * Fetch a manifest from the app's `/api/v2/manifest` endpoint.
 * No auth — manifests are public surface descriptions.
 */
export async function fetchManifest(slug: string): Promise<AppManifest> {
	const app = resolveApp(slug);
	const url = `${app.url}/api/v2/manifest`;
	const res = await fetch(url, { headers: { accept: 'application/json' } });
	if (!res.ok) {
		throw new Error(`Manifest fetch ${url} failed: HTTP ${res.status}`);
	}
	const manifest = (await res.json()) as AppManifest;
	if (!manifest.appSlug || !manifest.features) {
		throw new Error(`Manifest from ${url} is malformed: missing appSlug/features`);
	}
	return manifest;
}

/**
 * Load a manifest from cache (if fresh) or fetch + cache.
 * Pass `{refresh: true}` to bypass cache.
 */
export async function loadManifest(
	slug: string,
	opts: { refresh?: boolean } = {}
): Promise<AppManifest> {
	ensureDir();
	const path = cachePath(slug);

	if (!opts.refresh && existsSync(path) && isCacheFresh(path)) {
		try {
			return JSON.parse(readFileSync(path, 'utf-8')) as AppManifest;
		} catch {
			// Corrupt cache — fall through to refetch.
		}
	}

	const manifest = await fetchManifest(slug);
	writeFileSync(path, JSON.stringify(manifest, null, 2));
	return manifest;
}

export function clearManifestCache(slug?: string) {
	ensureDir();
	if (slug) {
		const p = cachePath(slug);
		if (existsSync(p)) {
			require('node:fs').unlinkSync(p);
		}
	} else {
		// Clear all
		for (const f of require('node:fs').readdirSync(CACHE_DIR)) {
			require('node:fs').unlinkSync(join(CACHE_DIR, f));
		}
	}
}

/**
 * Total action count across all features in a manifest. Useful for
 * `mm manifest` summary output.
 */
export function actionCount(m: AppManifest): number {
	return Object.values(m.features).reduce((n, actions) => n + Object.keys(actions).length, 0);
}

/**
 * Resolve a `feature.action` string against a manifest. Throws with a
 * helpful message if either part is missing.
 */
export function resolveAction(
	manifest: AppManifest,
	featureAction: string
): { feature: string; action: string; def: ManifestAction } {
	const dot = featureAction.indexOf('.');
	if (dot < 0) {
		throw new Error(
			`feature.action must be 'feature.action' format, got: '${featureAction}'`
		);
	}
	const feature = featureAction.slice(0, dot);
	const action = featureAction.slice(dot + 1);
	const featureMap = manifest.features[feature];
	if (!featureMap) {
		const known = Object.keys(manifest.features).slice(0, 10).join(', ');
		throw new Error(
			`Unknown feature '${feature}' in ${manifest.appSlug}. ` +
				`Known: ${known}${Object.keys(manifest.features).length > 10 ? '…' : ''}`
		);
	}
	const def = featureMap[action];
	if (!def) {
		const known = Object.keys(featureMap).slice(0, 10).join(', ');
		throw new Error(
			`Unknown action '${action}' on ${manifest.appSlug}.${feature}. ` +
				`Known: ${known}${Object.keys(featureMap).length > 10 ? '…' : ''}`
		);
	}
	return { feature, action, def };
}

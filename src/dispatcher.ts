/**
 * Generic dispatcher — POSTs `{feature, action, payload}` to any app's
 * `/api/v2` endpoint with the user's bearer token (mm_…).
 *
 * Replaces per-app HTTP clients (kb.ts/crm.ts/etc) with one path. The
 * per-app commands continue to exist as ergonomic aliases that build
 * the right `feature.action` + payload before delegating here.
 */

import { resolveApp } from './apps';
import { loadAuth } from './auth';
import { loadManifest, resolveAction, type AppManifest } from './manifest';

export interface DispatchResult {
	ok: boolean;
	status: number;
	body: unknown;
}

/**
 * Call `<app>/api/v2` with `{feature, action, payload}`. If
 * `validate: true`, pre-flights against the cached manifest to fail
 * fast on unknown feature.action (saves a network round trip + gives a
 * better error message).
 */
export async function dispatch(
	appSlug: string,
	featureAction: string,
	payload: Record<string, unknown> = {},
	opts: { validate?: boolean; instanceId?: string } = {}
): Promise<DispatchResult> {
	const app = resolveApp(appSlug);

	let manifest: AppManifest | null = null;
	if (opts.validate ?? true) {
		try {
			manifest = await loadManifest(appSlug);
			resolveAction(manifest, featureAction); // throws on unknown
		} catch (err) {
			// If manifest fetch fails (network, app down), fall through to
			// the live POST so the actual server error surfaces, not ours.
			if (err instanceof Error && /Unknown (feature|action)/.test(err.message)) {
				throw err;
			}
			// otherwise: proceed without pre-validation
		}
	}

	const dot = featureAction.indexOf('.');
	const feature = featureAction.slice(0, dot);
	const action = featureAction.slice(dot + 1);

	const headers: Record<string, string> = {
		'content-type': 'application/json'
	};
	const auth = loadAuth();
	if (auth) {
		headers['authorization'] = `Bearer ${auth.token}`;
	}
	if (opts.instanceId) {
		headers['x-hub-instance-id'] = opts.instanceId;
	}

	const url = `${app.url}/api/v2`;
	const res = await fetch(url, {
		method: 'POST',
		headers,
		body: JSON.stringify({ feature, action, payload })
	});

	let body: unknown;
	const contentType = res.headers.get('content-type') ?? '';
	if (contentType.includes('application/json')) {
		body = await res.json();
	} else {
		body = await res.text();
	}

	return { ok: res.ok, status: res.status, body };
}

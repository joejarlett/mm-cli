/**
 * Unified HTTP client — one place for every wire surface mm-cli speaks.
 *
 * Three transport methods, one auth path:
 *
 *   hub(feature, action, payload)
 *     POST https://meta-me.uk/api/mm
 *     Body: { feature, action, payload }
 *     Envelope: { data: T } | { errors: [...] } — unwrapped on success,
 *               thrown on error.
 *     Used by: calendar, tasks, drive, email (incl. inbox), instance.list.
 *
 *   v2(appSlug, "feature.action", payload, opts)
 *     POST <app>/api/v2
 *     Body: { feature, action, payload }
 *     Returns the raw envelope (status + body) without throwing — the
 *     v2 contract varies by app so callers parse their own shape.
 *     Used by: universal verbs (mm <app> ask/find/do/<feature> <action>).
 *
 *   rpc(appSlug, feature, action, payload)
 *     POST <app>/api/rpc
 *     Body: { feature, action, payload }
 *     Returns parsed JSON. Throws on HTTP error.
 *     Used by: kb, crm (legacy per-app surface that pre-dates /api/v2).
 *     Bearer + X-Hub-User-Id headers are set; the apps' bearer-auth
 *     handler validates against meta-me-auth/api/cli/validate and uses
 *     the validation response (NOT the request header) for identity.
 *
 * For local-agent REST/WS, see `agentBase()` / `agentFetch()` below.
 *
 * All three carry `Authorization: Bearer <mm_token>` + `X-Hub-User-Id`
 * when the user is logged in. Unauthenticated calls (manifest, agent
 * card) use the same fetch but no auth headers — those callers don't
 * go through this client today; they have their own fetch in
 * src/manifest.ts and src/agent-card.ts.
 */

import { loadAuth } from '../auth';
import { resolveApp } from '../apps';
import { loadManifest, resolveAction, type AppManifest } from '../manifest';
import { getTailscaleSuffix } from '../tailscale';
import { loadConfig } from '../config';

const { hubUrl: HUB_URL, localAgentUrl: AGENT_BASE, crmInstanceId: CRM_INSTANCE } = loadConfig();

// ─── Hub mm-RPC ────────────────────────────────────────────────────────

/**
 * POST `meta-me.uk/api/mm` with `{feature, action, payload}`. Unwraps
 * the `data` field on success, throws an Error with the structured
 * `errors[0].detail || title` on failure.
 */
export async function hub<T = unknown>(
	feature: string,
	action: string,
	payload?: Record<string, unknown>,
): Promise<T> {
	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const res = await fetch(`${HUB_URL}/api/mm`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${auth.token}`,
			'X-Hub-User-Id': auth.userId,
			'Content-Type': 'application/json',
		},
		body: JSON.stringify({ feature, action, payload: payload ?? {} }),
	});

	const text = await res.text();
	let parsed: { data?: unknown; errors?: Array<{ code: string; title?: string; detail?: string }> };
	try {
		parsed = JSON.parse(text);
	} catch {
		throw new Error(`Hub API non-JSON response (${res.status}): ${text.slice(0, 200)}`);
	}

	if (!res.ok || 'errors' in parsed) {
		const first = parsed.errors?.[0];
		const msg = first?.detail || first?.title || `${feature}.${action} failed (${res.status})`;
		throw new Error(msg);
	}
	return (parsed as { data: T }).data;
}

// ─── App /api/v2 ───────────────────────────────────────────────────────

export interface V2Result {
	ok: boolean;
	status: number;
	body: unknown;
}

/**
 * POST `<app>/api/v2` with `{feature, action, payload}`. With
 * `validate: true` (default), pre-flights against the cached manifest
 * to fail fast on unknown feature.action with a friendlier error.
 *
 * Does NOT throw on HTTP error — returns `{ok, status, body}` so
 * callers can render the failure envelope (which varies per app).
 */
export async function v2(
	appSlug: string,
	featureAction: string,
	payload: Record<string, unknown> = {},
	opts: { validate?: boolean; instanceId?: string } = {},
): Promise<V2Result> {
	const app = resolveApp(appSlug);

	let manifest: AppManifest | null = null;
	if (opts.validate ?? true) {
		try {
			manifest = await loadManifest(appSlug);
			resolveAction(manifest, featureAction); // throws on unknown
		} catch (err) {
			if (err instanceof Error && /Unknown (feature|action)/.test(err.message)) {
				throw err;
			}
			// other errors (manifest unreachable) — fall through to live POST
		}
	}

	const dot = featureAction.indexOf('.');
	const feature = featureAction.slice(0, dot);
	const action = featureAction.slice(dot + 1);

	const headers: Record<string, string> = { 'content-type': 'application/json' };
	const auth = loadAuth();
	if (auth) {
		headers['authorization'] = `Bearer ${auth.token}`;
		if (auth.userId) headers['x-hub-user-id'] = auth.userId;
	}
	const effectiveInstanceId =
		opts.instanceId ?? (appSlug === 'crm' ? CRM_INSTANCE : undefined);
	if (effectiveInstanceId) headers['x-hub-instance-id'] = effectiveInstanceId;

	const res = await fetch(`${app.url}/api/v2`, {
		method: 'POST',
		headers,
		body: JSON.stringify({ feature, action, payload }),
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

// ─── App /api/rpc (legacy — kb + crm) ──────────────────────────────────

/**
 * POST `<app>/api/rpc` with `{feature, action, payload}`. Returns the
 * parsed JSON body. Throws on HTTP error (the response shape varies by
 * app — most return `{data: ...}` but some return `{meta: ..., data: ...}`).
 *
 * The app's bearer-auth hook validates the token via meta-me-auth and
 * uses the validation response for identity. The X-Hub-User-Id header
 * is sent but ignored by the auth path — it's there for app code that
 * wants to skip a `locals.user.id` lookup.
 */
export async function rpc<T = unknown>(
	appSlug: string,
	feature: string,
	action: string,
	payload?: Record<string, unknown>,
): Promise<T> {
	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const app = resolveApp(appSlug);
	const headers: Record<string, string> = {
		Authorization: `Bearer ${auth.token}`,
		'X-Hub-User-Id': auth.userId,
		'Content-Type': 'application/json',
	};
	// Pin the CRM instance from config (MM_CRM_INSTANCE in ~/.mm/.env or
	// an inline override) so the server doesn't have to guess from the
	// user's session-wide instance list. Cookie-session web flows fall
	// back through hooks.server.ts; for CLI Bearer auth this header is
	// the only way to be unambiguous when the user owns >1 instance.
	if (appSlug === 'crm' && CRM_INSTANCE) {
		headers['X-Hub-Instance-Id'] = CRM_INSTANCE;
	}
	const res = await fetch(`${app.url}/api/rpc`, {
		method: 'POST',
		headers,
		body: JSON.stringify({ feature, action, payload: payload ?? {} }),
	});

	const contentType = res.headers.get('content-type') ?? '';
	if (!res.ok) {
		if (!contentType.includes('application/json')) {
			const text = await res.text();
			const snippet = text.slice(0, 120).trim();
			const hint = snippet.startsWith('<') ? ' — server returned HTML, likely a timeout or proxy error' : '';
			throw new Error(`${appSlug} ${feature}.${action} failed (${res.status})${hint}`);
		}
		const text = await res.text();
		throw new Error(`${appSlug} ${feature}.${action} failed (${res.status}): ${text.slice(0, 200)}`);
	}

	if (!contentType.includes('application/json')) {
		const text = await res.text();
		const snippet = text.slice(0, 120).trim();
		const hint = snippet.startsWith('<') ? ' — server returned HTML, likely a timeout or proxy error' : '';
		throw new Error(`${appSlug} ${feature}.${action} failed (${res.status})${hint}`);
	}

	return res.json() as Promise<T>;
}

// ─── Local agent REST + WS ─────────────────────────────────────────────

export interface AgentTarget {
	http: string;
	ws: string;
	displayName: string;
}

/**
 * HTTP + WS base URL for the targeted agent. No node = local agent
 * (`AGENT_BASE`). With node = resolve via hub `instance.list` + the
 * local tailscaled's MagicDNS suffix.
 */
export async function agentBase(node: string | undefined): Promise<AgentTarget> {
	if (!node) {
		return {
			http: AGENT_BASE,
			ws: AGENT_BASE.replace(/^http/, 'ws'),
			displayName: 'local',
		};
	}
	const resolved = await resolveNode(node);
	return {
		http: resolved.baseUrl,
		ws: resolved.baseUrl.replace(/^https/, 'wss').replace(/^http/, 'ws'),
		displayName: resolved.displayName,
	};
}

/**
 * `fetch` against the agent at `node` (or localhost if undefined).
 * Caller handles non-2xx as appropriate — REST shapes vary per endpoint.
 */
export async function agentFetch(
	node: string | undefined,
	path: string,
	init?: RequestInit,
): Promise<Response> {
	const { http, displayName } = await agentBase(node);
	const url = `${http}${path}`;
	try {
		return await fetch(url, init);
	} catch (err) {
		throw new Error(`fetch ${url} failed (${displayName}): ${err}`);
	}
}

// ─── Internal: --node name → tailnet URL ───────────────────────────────

import type { HubInstance, HubInstanceListResp } from '../wire';

let nodesCache: HubInstance[] | null = null;

/**
 * Cached list of registered agent nodes. Hits the hub once per CLI
 * process via `instance.list { slugs: ['chat', 'agent'] }`.
 *
 * Exported so `mm chat nodes` can render the list without a second
 * round trip after `--node` resolution caches.
 */
export async function loadNodes(): Promise<HubInstance[]> {
	if (nodesCache) return nodesCache;
	const data = await hub<HubInstanceListResp>('instance', 'list', {
		slugs: ['chat', 'agent'],
	});
	nodesCache = data.instances ?? [];
	return nodesCache;
}

/**
 * Resolve `--node <name>` to a base URL fronted by the local
 * tailscaled's MagicDNS suffix (so the cert + connection survive
 * Tailscale's occasional suffix rotation). The stored
 * `app_instance.url` provides bare hostname + port; the suffix comes
 * from `tailscale status --json`.
 */
export async function resolveNode(name: string): Promise<{ baseUrl: string; displayName: string }> {
	const nodes = await loadNodes();
	const lower = name.toLowerCase();
	const matches = nodes.filter((n) => n.name.toLowerCase() === lower);
	if (matches.length === 0) {
		const known = nodes.map((n) => n.name).join(', ') || '(none registered)';
		throw new Error(`No node named '${name}'. Known: ${known}. Try: mm chat nodes`);
	}
	if (matches.length > 1) {
		throw new Error(`Multiple nodes named '${name}'. Disambiguate via the hub.`);
	}
	const row = matches[0];
	if (!row.url) {
		throw new Error(`Node '${row.name}' has no URL registered.`);
	}
	const parsed = new URL(row.url);
	const bare = parsed.hostname.split('.')[0];
	const port = parsed.port || (parsed.protocol === 'https:' ? '443' : '80');
	const suffix = getTailscaleSuffix();
	return {
		baseUrl: `https://${bare}.${suffix}:${port}`,
		displayName: row.name,
	};
}


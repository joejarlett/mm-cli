/**
 * hubApi() — shared helper for calling the platform RPC at
 * meta-me.uk/api/mm. Used by every `mm <feature>` command that talks
 * to the hub (email, calendar, tasks, drive, etc).
 *
 * Auth: the user's `mm_…` API key from `~/.config/mm/auth.json`,
 * resolved on the hub via /api/cli/validate and surfaced as
 * ctx.userId to RPC handlers.
 */

import { loadAuth } from './auth';

const HUB_URL = 'https://meta-me.uk';

export async function hubApi<T = unknown>(
	feature: string,
	action: string,
	payload?: Record<string, unknown>
): Promise<T> {
	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const res = await fetch(`${HUB_URL}/api/mm`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${auth.token}`,
			'X-Hub-User-Id': auth.userId,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ feature, action, payload: payload ?? {} })
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

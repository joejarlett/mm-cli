/**
 * API module — HTTP client for Meta-Me services.
 */

import { loadAuth } from './auth';
import { loadConfig } from './config';

const { authUrl: AUTH_URL, hubUrl: HUB_URL } = loadConfig();

interface ApiResponse {
	ok: boolean;
	status: number;
	data: any;
}

async function api(method: string, url: string, body?: any): Promise<ApiResponse> {
	const headers: Record<string, string> = { 'Content-Type': 'application/json' };

	// Attach auth if available
	const auth = loadAuth();
	if (auth) {
		headers['Authorization'] = `Bearer ${auth.token}`;
	}

	const res = await fetch(url, {
		method,
		headers,
		body: body ? JSON.stringify(body) : undefined
	});

	let data: any;
	const contentType = res.headers.get('content-type') || '';
	if (contentType.includes('application/json')) {
		data = await res.json();
	} else {
		data = await res.text();
	}

	return { ok: res.ok, status: res.status, data };
}

// ---------------------------------------------------------------------------
// Device flow
// ---------------------------------------------------------------------------

export async function deviceInitiate(): Promise<{
	deviceCode: string;
	userCode: string;
	verificationUri: string;
	expiresIn: number;
	interval: number;
}> {
	const { ok, status, data } = await api('POST', `${AUTH_URL}/api/cli/device`);
	if (!ok) throw new Error(`Device flow initiation failed (${status}): ${data.error_description || data.error}`);
	return {
		deviceCode: data.device_code,
		userCode: data.user_code,
		verificationUri: data.verification_uri_complete || data.verification_uri,
		expiresIn: data.expires_in,
		interval: data.interval
	};
}

export async function devicePoll(deviceCode: string, name?: string): Promise<{
	accessToken: string;
	key: { id: string; name: string; prefix: string; scopes: string[] };
}> {
	const { ok, status, data } = await api('POST', `${AUTH_URL}/api/cli/token`, {
		device_code: deviceCode,
		client_name: name || 'mm CLI'
	});
	if (!ok) {
		if (data.error === 'authorization_pending') {
			throw new PendingError();
		}
		if (data.error === 'expired_token') {
			throw new Error('The device code has expired. Run `mm login` again.');
		}
		throw new Error(`Token exchange failed (${status}): ${data.error_description || data.error}`);
	}
	return {
		accessToken: data.access_token,
		key: data.key
	};
}

export class PendingError extends Error {
	constructor() {
		super('Authorization pending — waiting for user to authorize in browser.');
		this.name = 'PendingError';
	}
}

// ---------------------------------------------------------------------------
// Token validation
// ---------------------------------------------------------------------------

export async function validateToken(token: string): Promise<{
	user: { id: string; name: string; email: string; role: string };
	key: { id: string; name: string; scopes: string[] };
} | null> {
	const { ok, data } = await api('POST', `${AUTH_URL}/api/cli/validate`, { token });
	if (!ok) return null;
	return data;
}

// ---------------------------------------------------------------------------
// App dispatch
// ---------------------------------------------------------------------------

const APP_URLS: Record<string, string> = {
	kb: 'https://kb.meta-me.uk',
	crm: 'https://crm.meta-me.uk'
};

export async function appDispatch(
	app: string,
	command: string,
	args: Record<string, string>,
	flags: { json?: boolean }
): Promise<any> {
	const baseUrl = APP_URLS[app];
	if (!baseUrl) throw new Error(`Unknown app: ${app}`);

	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const params = new URLSearchParams({ command, ...args });
	// For now, dispatch via the app's /api/v2/rpc or /api/rpc endpoint
	// This is a simple GET-based dispatch for read operations.
	// Write operations will use POST with a body.
	const url = `${baseUrl}/api/rpc?${params}`;

	const res = await fetch(url, {
		method: 'GET',
		headers: {
			'Authorization': `Bearer ${auth.token}`,
			'Accept': 'application/json',
			'X-Hub-User-Id': auth.userId
		}
	});

	if (!res.ok) {
		const text = await res.text();
		throw new Error(`${app} ${command} failed (${res.status}): ${text.slice(0, 200)}`);
	}

	const data = await res.json();

	if (flags?.json) {
		return data;
	}

	return data;
}

export { AUTH_URL, HUB_URL };

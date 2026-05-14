/**
 * mm crm — CRM commands.
 *
 * CRM v2 is currently in development. These commands dispatch to the CRM API.
 */

import { loadAuth } from '../auth';

const CRM_URL = 'https://crm.meta-me.uk';

async function crmApi(path: string, options?: { method?: string; body?: any }) {
	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const method = options?.method || 'GET';
	const headers: Record<string, string> = {
		'Authorization': `Bearer ${auth.token}`,
		'X-Hub-User-Id': auth.userId,
		'Accept': 'application/json'
	};

	if (options?.body) {
		headers['Content-Type'] = 'application/json';
	}

	const res = await fetch(`${CRM_URL}${path}`, {
		method,
		headers,
		body: options?.body ? JSON.stringify(options.body) : undefined
	});

	if (!res.ok) {
		const text = await res.text();
		throw new Error(`CRM API error (${res.status}): ${text.slice(0, 200)}`);
	}

	return res.json();
}

export async function crmDispatch(command: string, args: string[], flags: { json?: boolean }) {
	const json = flags?.json || false;

	switch (command) {
		case 'contacts':
			if (args[0] === 'list' || !args[0]) {
				return await crmContactsList(json);
			}
			// Pass through: mm crm contacts get id=...
			return await crmPassThrough('contacts', args, json);
		case 'projects':
			return await crmPassThrough('projects', args, json);
		case 'interactions':
			return await crmPassThrough('interactions', args, json);
		case 'persons':
			return await crmPassThrough('persons', args, json);
		case 'status':
			return await crmStatus(json);
		default:
			return await crmPassThrough(command, args, json);
	}
}

async function crmContactsList(json: boolean) {
	const data = await crmApi('/api/rpc?feature=contacts&action=list');
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	const contacts = data?.data || [];
	if (contacts.length === 0) {
		console.log('No contacts found.');
		return;
	}
	for (const c of contacts) {
		const attrs = c.attributes || {};
		const name = attrs.name || attrs.email || c.id?.slice(0, 8);
		console.log(`  ${name}`);
	}
}

async function crmStatus(json: boolean) {
	const data = await crmApi('/api/rpc?feature=status&action=get');
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log('CRM:', JSON.stringify(data, null, 2));
}

async function crmPassThrough(feature: string, args: string[], json: boolean) {
	const params = new URLSearchParams();
	params.set('feature', feature);
	if (args.length > 0) {
		params.set('action', args[0]);
	}
	for (let i = 1; i < args.length; i++) {
		const eq = args[i].indexOf('=');
		if (eq > 0) {
			params.set(args[i].slice(0, eq), args[i].slice(eq + 1));
		}
	}

	const data = await crmApi(`/api/rpc?${params}`);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log(JSON.stringify(data, null, 2));
}

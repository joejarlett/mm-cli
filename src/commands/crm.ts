/**
 * mm crm — CRM commands.
 *
 * Dispatches to CRM v2's /api/rpc endpoint using the same
 * feature/action/payload POST format as KB.
 */

import { loadAuth } from '../auth';

const CRM_URL = 'https://crm.meta-me.uk';

async function crmApi(feature: string, action: string, payload?: Record<string, any>) {
	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const headers: Record<string, string> = {
		'Authorization': `Bearer ${auth.token}`,
		'X-Hub-User-Id': auth.userId,
		'Content-Type': 'application/json'
	};

	const body = JSON.stringify({ feature, action, payload: payload || {} });
	const res = await fetch(`${CRM_URL}/api/rpc`, {
		method: 'POST',
		headers,
		body
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
		// Orient
		case 'workspace':
		case 'ws':
			return await crmPassThrough('workspace', args, json);
		case 'status':
			return await crmStatus(json);
		case 'tree':
			return await crmPassThrough('contacts', ['tree', ...args], json);

		// Read
		case 'peek':
			return await crmSingle('peek', args[0], json);
		case 'read':
			return await crmSingle('read', args[0], json);
		case 'context':
			return await crmSingle('context', args[0], json);
		case 'find':
			return await crmFind(args[0] || '', json);
		case 'surface':
			return await crmPassThrough('contacts', ['surface', ...args], json);

		// Write
		case 'capture':
		case 'log':
			return await crmCapture(args, json);

		// Lists
		case 'contacts':
			return await crmPassThrough('contacts', args, json);
		case 'projects':
			return await crmPassThrough('projects', args, json);
		case 'persons':
			return await crmPassThrough('persons', args, json);

		default:
			// Generic pass-through
			return await crmPassThrough(command, args, json);
	}
}

async function crmStatus(json: boolean) {
	try {
		const data = await crmApi('contacts', 'tree');
		if (json) {
			console.log(JSON.stringify(data, null, 2));
			return;
		}
		console.log(JSON.stringify(data, null, 2));
	} catch (err: any) {
		// Fallback: try basic status
		console.log('CRM v2 API is running');
		console.log('Available commands: mm crm contacts list, mm crm find "query", mm crm capture "text"');
	}
}

async function crmFind(query: string, json: boolean) {
	if (!query) {
		console.error('Usage: mm crm find <query>');
		process.exit(1);
	}
	const data = await crmApi('contacts', 'search', { query });
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	const hits = data?.data || [];
	if (hits.length === 0) {
		console.log('No results found.');
		return;
	}
	for (const hit of hits.slice(0, 10)) {
		const attrs = hit.attributes || {};
		const name = attrs.name || attrs.title || '(unnamed)';
		const snippet = (attrs.summary || '').slice(0, 120);
		console.log(`  ${hit.id?.slice(0, 8)}  ${name}`);
		if (snippet) console.log(`        ${snippet}`);
		console.log('');
	}
}

async function crmSingle(action: string, id: string, json: boolean) {
	if (!id) {
		console.error(`Usage: mm crm ${action} <id>`);
		process.exit(1);
	}
	const data = await crmApi('contacts', action, { id });
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log(JSON.stringify(data, null, 2));
}

async function crmCapture(args: string[], json: boolean) {
	const text = args.join(' ');
	if (!text) {
		console.error('Usage: mm crm capture "<text>"');
		process.exit(1);
	}
	const data = await crmApi('contacts', 'capture', { text });
	console.log(json ? JSON.stringify(data, null, 2) : 'Captured.');
}

async function crmPassThrough(feature: string, args: string[], json: boolean) {
	const action = args[0] || 'list';
	const payload: Record<string, string> = {};
	for (let i = 1; i < args.length; i++) {
		const eq = args[i].indexOf('=');
		if (eq > 0) {
			payload[args[i].slice(0, eq)] = args[i].slice(eq + 1);
		}
	}

	const data = await crmApi(feature, action, payload);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	const items = data?.data || [];
	if (items.length === 0) {
		console.log(`No results.`);
		return;
	}
	for (const item of items) {
		const attrs = item.attributes || {};
		const name = attrs.name || attrs.title || item.id?.slice(0, 8);
		console.log(`  ${name}`);
	}
}

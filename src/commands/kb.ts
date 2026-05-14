/**
 * mm kb — Knowledge Base commands.
 *
 * Each command dispatches to the KB API at kb.meta-me.uk/api/rpc
 * with the user's mm API key for authentication.
 */

import { loadAuth } from '../auth';

const KB_URL = 'https://kb.meta-me.uk';

async function kbApi(feature: string, action: string, payload?: Record<string, any>) {
	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const headers: Record<string, string> = {
		'Authorization': `Bearer ${auth.token}`,
		'X-Hub-User-Id': auth.userId,
		'Content-Type': 'application/json'
	};

	const body = JSON.stringify({ feature, action, payload: payload || {} });
	const res = await fetch(`${KB_URL}/api/rpc`, {
		method: 'POST',
		headers,
		body
	});

	if (!res.ok) {
		const text = await res.text();
		throw new Error(`KB API error (${res.status}): ${text.slice(0, 200)}`);
	}

	return res.json();
}

export async function kbDispatch(command: string, args: string[], flags: { json?: boolean }) {
	const json = flags?.json || false;

	switch (command) {
		case 'find':
			return await kbFind(args[0] || '', json);
		case 'tree':
			return await kbTree(args[0], json);
		case 'peek':
			return await kbPeek(args[0] || '', json);
		case 'read':
			return await kbRead(args[0] || '', json);
		case 'collections':
		case 'col':
		case 'notebooks':
			return await kbCollectionsList(json);
		case 'status':
			return await kbStatus(json);
		default:
			return await kbPassThrough(command, args, json);
	}
}

async function kbFind(query: string, json: boolean) {
	if (!query) {
		console.error('Usage: mm kb find <query>');
		process.exit(1);
	}
	const data = await kbApi('documents', 'searchCorpus', { query });
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
		const title = attrs.shortTitle || attrs.title || '(untitled)';
		const snippet = (attrs.summary || attrs.content || '').slice(0, 120);
		console.log(`  ${hit.id?.slice(0, 8)}  ${title}`);
		if (snippet) console.log(`        ${snippet}`);
		console.log('');
	}
}

async function kbTree(notebook: string | undefined, json: boolean) {
	const body = notebook
		? { feature: 'collections', action: 'get', name: notebook }
		: { feature: 'collections', action: 'list' };
	const data = await kbApi(body.feature, body.action, body);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	const collections = data?.data || [];
	for (const col of collections) {
		const attrs = col.attributes || {};
		const name = attrs.name || col.id;
		console.log(`  ${name}`);
	}
}

async function kbPeek(id: string, json: boolean) {
	if (!id) {
		console.error('Usage: mm kb peek <doc-id|collection-name>');
		process.exit(1);
	}
	const data = await kbApi('documents', 'get', { id });
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	const doc = data?.data;
	if (!doc) {
		console.log('Not found.');
		return;
	}
	const attrs = doc.attributes || {};
	console.log(`Title:    ${attrs.title || attrs.shortTitle || '(untitled)'}`);
	console.log(`Type:     ${attrs.docType || attrs.type || '?'}`);
	console.log(`Labels:   ${(attrs.labels || []).join(', ') || 'none'}`);
	if (attrs.summary) {
		console.log(`\n${attrs.summary.slice(0, 500)}`);
	}
}

async function kbRead(id: string, json: boolean) {
	if (!id) {
		console.error('Usage: mm kb read <doc-id>');
		process.exit(1);
	}
	const data = await kbApi('documents', 'get', { id, includeContent: 'true' });
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	const doc = data?.data;
	if (!doc) {
		console.log('Not found.');
		return;
	}
	const attrs = doc.attributes || {};
	console.log(`# ${attrs.title || attrs.shortTitle || '(untitled)'}`);
	console.log('');
	if (attrs.content) {
		console.log(attrs.content);
	} else if (attrs.url) {
		console.log(`Source: ${attrs.url}`);
	}
}

async function kbCollectionsList(json: boolean) {
	const data = await kbApi('collections', 'list');
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	const collections = data?.data || [];
	for (const col of collections) {
		const attrs = col.attributes || {};
		console.log(`  ${attrs.name || col.id}`);
	}
}

async function kbStatus(json: boolean) {
	const data = await kbApi('status', 'get');
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log('KB:', JSON.stringify(data, null, 2));
}

async function kbPassThrough(command: string, args: string[], json: boolean) {
	const params = new URLSearchParams();
	params.set('feature', command);
	if (args.length > 0) {
		params.set('action', args[0]);
	}
	for (let i = 1; i < args.length; i++) {
		const eq = args[i].indexOf('=');
		if (eq > 0) {
			params.set(args[i].slice(0, eq), args[i].slice(eq + 1));
		}
	}

	const data = await kbApi(body.feature, body.action, body);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log(JSON.stringify(data, null, 2));
}

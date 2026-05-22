/**
 * mm crm — CRM commands.
 *
 * Dispatches to CRM v2's /api/rpc endpoint.
 *
 * Intuitive surface (core verbs only):
 *   mm crm surface               Today's priorities
 *   mm crm contacts [find <q>]   List contacts or search
 *   mm crm projects              List projects
 *   mm crm log "<text>"          Log an interaction
 *   mm crm context <person>      Person context
 *   mm crm peek <id>             Preview anything
 *   mm crm read <id>             Full content
 *
 * Escape hatch: mm crm rpc <feature> <action> [k=v ...]
 */

import { rpc } from '../http/client';

const crmApi = (feature: string, action: string, payload?: Record<string, any>) =>
	rpc<any>('crm', feature, action, payload);

export function printCrmHelp() {
	console.log(`mm crm — CRM (crm-v2)

Subcommands:
  surface [key=value …]   What's worth attention right now
  contacts                List/tree your contacts
  contacts find <query>   Search contacts
  projects                List projects
  log <args…>             Log an interaction
  context <id>            Full contact profile + recent activity
  peek <id>               Quick preview of a node (contact/project/…)
  read <id>               Full read of a node
  find <query>            Search across the CRM
  rpc <feature.action> [json]
                          Raw RPC against the CRM's /api/v2

Add --json for parseable output. Auth via \`mm login\`.

Pin an instance (single-user, multi-CRM): set MM_CRM_INSTANCE=<uuid> in
~/.mm/.env or prefix inline (\`MM_CRM_INSTANCE=<uuid> mm crm log "..."\`).
Without a pin the server falls back to the user's first CRM instance,
which is fine for most accounts but ambiguous when you own several.`);
}

export async function crmDispatch(command: string, args: string[], flags: { json?: boolean }) {
	const json = flags?.json || false;

	switch (command) {
		case '':
		case 'help':
		case '--help':
		case '-h':
			printCrmHelp();
			return;

		case 'surface':
			return await crmSurface(args, json);

		case 'contacts':
			if (args[0] === 'find') {
				const all = args.includes('--all');
				const query = args.slice(1).filter((a) => a !== '--all').join(' ');
				return await crmContactsFind(query, all, json);
			}
			return await crmTree(json);

		case 'projects':
			return await crmProjects(json);

		case 'log':
			return await crmLog(args, json);

		case 'context':
			return await crmContext(args[0], json);

		case 'peek':
			return await crmPeek(args[0], json);

		case 'read':
			return await crmRead(args[0], json);

		case 'find':
			return await crmFind(args.join(' '), json);

		case 'rpc':
			return await crmRpc(args, json);

		default:
			console.error(`Unknown command: mm crm ${command}`);
			console.error('Try `mm crm help` for the full list.');
			process.exit(1);
	}
}

// ─── Surface ───────────────────────────────────────────────────────────

async function crmSurface(args: string[], json: boolean) {
	const payload: Record<string, string> = {};
	for (const arg of args) {
		const eq = arg.indexOf('=');
		if (eq > 0) payload[arg.slice(0, eq)] = arg.slice(eq + 1);
	}
	const limit = payload.limit ? Number(payload.limit) : undefined;
	const data = await crmApi('surface', 'list', limit ? { limit } : {});
	if (json) { console.log(JSON.stringify(data, null, 2)); return; }

	const meta = data?.meta || {};
	if (meta?.emptyState) { console.log('All quiet. Nothing to surface.'); return; }

	for (const section of meta?.sections || []) {
		console.log(`## ${section.title} (${section.items?.length || 0})`);
		for (const item of section.items || []) {
			const who = item.person?.title && item.person.title !== item.title
				? ` — with ${item.person.title}` : '';
			console.log(`- **${item.title}** [${item.reason}]${who}`);
		}
		console.log('');
	}
	if (meta?.spilloverCount) console.log(`(+${meta.spilloverCount} more below capacity cut)`);
}

// ─── Contacts (tree) ───────────────────────────────────────────────────

async function crmTree(json: boolean) {
	const data = await crmApi('tree', 'show');
	if (json) { console.log(JSON.stringify(data, null, 2)); return; }

	const meta = data?.meta || {};
	const counts = meta?.counts || {};
	console.log(Object.entries(counts).map(([k, v]) => `${v} ${k}`).join(' · '));
	for (const c of meta?.contacts || []) {
		const touch = c.lastMeaningfulTouch ? ` (last: ${c.lastMeaningfulTouch?.slice(0, 10)})` : '';
		console.log(`  ${c.title} — ${c.interactionsCount} interactions${touch}`);
	}
}

// ─── Projects ──────────────────────────────────────────────────────────

async function crmProjects(json: boolean) {
	const data = await crmApi('project', 'list');
	if (json) { console.log(JSON.stringify(data, null, 2)); return; }

	const items = data?.data || [];
	if (items.length === 0) { console.log('No projects.'); return; }
	for (const p of items) {
		const d = p.attributes?.data || {};
		console.log(`  ${p.attributes?.title} · ${d.state || '?'} · ${p.attributes?.memberCount || 0} members`);
	}
}

// ─── Find ──────────────────────────────────────────────────────────────

async function crmFind(query: string, json: boolean) {
	if (!query) { console.error('Usage: mm crm find <query>  or  mm crm contacts find <query>'); process.exit(1); }
	const data = await crmApi('find', 'search', { query });
	if (json) { console.log(JSON.stringify(data, null, 2)); return; }

	const hits = data?.data || [];
	if (hits.length === 0) { console.log('No results.'); return; }
	for (const hit of hits.slice(0, 10)) {
		const a = hit.attributes || {};
		const name = a.name || a.title || '(unnamed)';
		const snippet = (a.summary || '').slice(0, 120);
		console.log(`  ${hit.id?.slice(0, 8)}  ${name}`);
		if (snippet) console.log(`        ${snippet}`);
		console.log('');
	}
}

// `mm crm contacts find "Milbotix"` — by-name lookup over contact
// nodes. Different intent (and different action) to `mm crm find`,
// which is a semantic search over interactions. By default this hides
// untriaged prospects; pass --all to include them.
async function crmContactsFind(query: string, includeProspects: boolean, json: boolean) {
	if (!query) {
		console.error('Usage: mm crm contacts find <query> [--all]');
		process.exit(1);
	}
	const data = await crmApi('contact', 'search', { query, includeProspects });
	if (json) { console.log(JSON.stringify(data, null, 2)); return; }

	const hits = data?.data || [];
	const hidden = Number(data?.meta?.prospectsHidden ?? 0);
	if (hits.length === 0) {
		if (hidden > 0) {
			console.log(`No matching members. ${hidden} matching prospect${hidden === 1 ? '' : 's'} hidden — re-run with --all to include, or triage from /review.`);
		} else {
			console.log('No matching contacts.');
		}
		return;
	}
	for (const hit of hits) {
		const a = hit.attributes || {};
		const tag = a.isProspect ? ' [prospect]' : '';
		const projects = a.projects ? ` · ${a.projects}` : '';
		const touch = a.lastInteractionAt ? ` · last ${String(a.lastInteractionAt).slice(0, 10)}` : '';
		console.log(`  ${String(hit.id).slice(0, 8)}  ${a.title}${tag}${projects}${touch}`);
	}
	if (hidden > 0) {
		console.log(`\n  (+${hidden} prospect${hidden === 1 ? '' : 's'} hidden — pass --all to include)`);
	}
}

// ─── Log ───────────────────────────────────────────────────────────────

async function crmLog(args: string[], json: boolean) {
	const text = args.join(' ');
	if (!text) { console.error('Usage: mm crm log "<text>"'); process.exit(1); }
	const data = await crmApi('interaction', 'log', { text });
	if (json) { console.log(JSON.stringify(data, null, 2)); return; }
	console.log(`Logged: ${data?.data?.attributes?.title || '(untitled)'}`);
}

// ─── Context ───────────────────────────────────────────────────────────

async function crmContext(id: string, json: boolean) {
	if (!id) { console.error('Usage: mm crm context <person-name-or-id>'); process.exit(1); }
	const data = await crmApi('contact', 'context', { target: id });
	if (json) { console.log(JSON.stringify(data, null, 2)); return; }

	const attrs = data?.data?.attributes || {};
	const meta = data?.meta || {};
	console.log(`# ${attrs.title || '(unnamed)'}`);
	if (attrs.summary) console.log(`\n${attrs.summary.slice(0, 500)}`);
	if (meta.person) console.log(`\nWith: ${meta.person.title || meta.person.id}`);
	if (meta.snippet) console.log(`\n> ${String(meta.snippet).replace(/\n/g, '\n> ')}`);
}

// ─── Peek ──────────────────────────────────────────────────────────────

async function crmPeek(id: string, json: boolean) {
	if (!id) { console.error('Usage: mm crm peek <id-or-name>'); process.exit(1); }
	const data = await crmApi('peek', 'show', { target: id });
	if (json) { console.log(JSON.stringify(data, null, 2)); return; }

	const attrs = data?.data?.attributes || {};
	const meta = data?.meta || {};
	console.log(`# ${attrs.title || '(untitled)'}`);
	console.log(`Type: ${data?.data?.type || '?'}  ID: ${data?.data?.id?.slice(0, 8) || '?'}`);
	if (attrs.summary) console.log(`\n${attrs.summary.slice(0, 500)}`);
	if (meta.snippet) console.log(`\n> ${String(meta.snippet).replace(/\n/g, '\n> ')}`);
}

// ─── Read ──────────────────────────────────────────────────────────────

async function crmRead(id: string, json: boolean) {
	if (!id) { console.error('Usage: mm crm read <id-or-name>'); process.exit(1); }
	const data = await crmApi('read', 'show', { target: id });
	if (json) { console.log(JSON.stringify(data, null, 2)); return; }

	const attrs = data?.data?.attributes || {};
	console.log(`# ${attrs.title || '(untitled)'}`);
	if (attrs.content) console.log(`\n${attrs.content}`);
	else console.log(JSON.stringify(data, null, 2));
}

// ─── RPC escape hatch ──────────────────────────────────────────────────

async function crmRpc(args: string[], json: boolean) {
	const [feature, action, ...rest] = args;
	if (!feature || !action) {
		console.error('Usage: mm crm rpc <feature> <action> [k=v ...]');
		process.exit(1);
	}
	const payload: Record<string, string> = {};
	for (const arg of rest) {
		const eq = arg.indexOf('=');
		if (eq > 0) payload[arg.slice(0, eq)] = arg.slice(eq + 1);
	}
	const data = await crmApi(feature, action, payload);
	console.log(JSON.stringify(data, null, 2));
}

/**
 * mm kb — Knowledge Base commands.
 *
 * Dispatches to the KB API at kb.meta-me.uk/api/rpc with the user's mm
 * API key for auth. Two surfaces:
 *
 *   Intent verbs (operate on meaning; names resolve to UUIDs anywhere)
 *     find · tree · peek · read · related · tagged · mentions · digest ·
 *     surface · rename · move · add · rm · tag · untag · label · describe
 *
 *   Resource verbs (raw feature.action passthrough for scripting)
 *     mm kb <feature> <action> [key=value ...]
 *
 * Read verbs are markdown-first (denser for an agent/human in a
 * terminal); pass --json for the structured payload.
 *
 * Ported from the project `kb` CLI (cli/kb.ts, deleted 2026-05-28) so the
 * full surface survives under `mm kb`.
 */

import { rpc } from '../http/client';
import { kbDb } from '../db';
import { sqlDispatch } from './hub';
import { readFileSync, mkdirSync, writeFileSync } from 'fs';
import { dirname } from 'path';

const kbApi = (feature: string, action: string, payload?: Record<string, unknown>) =>
	rpc<any>('kb', feature, action, payload);

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const SHORT_UUID_RE = /^[0-9a-f]{8,35}$/i;
const isId = (s: string) => UUID_RE.test(s) || SHORT_UUID_RE.test(s);

// ─── Output helpers ──────────────────────────────────────────────────────

function emit(json: boolean, structured: unknown, markdown: string) {
	console.log(json ? JSON.stringify(structured, null, 2) : markdown);
}

function fail(msg: string): never {
	console.error(`Error: ${msg}`);
	process.exit(1);
}

function apiErr(result: any, fallback: string): string {
	return result?.errors?.[0]?.detail || result?.errors?.[0]?.title || fallback;
}

// key=value → typed payload. `key@=path` reads file content as the value.
function parseArgs(argv: string[]): Record<string, unknown> {
	const out: Record<string, unknown> = {};
	for (const arg of argv) {
		const eq = arg.indexOf('=');
		if (eq <= 0) continue;
		let key = arg.slice(0, eq);
		let val: unknown = arg.slice(eq + 1);
		if (key.endsWith('@')) {
			out[key.slice(0, -1)] = readFileSync(val as string, 'utf8');
			continue;
		}
		if (val === 'true') val = true;
		else if (val === 'false') val = false;
		else if (/^\d+$/.test(val as string)) val = parseInt(val as string, 10);
		else if (/^\d+\.\d+$/.test(val as string)) val = parseFloat(val as string);
		else if ((val as string).startsWith('{') || (val as string).startsWith('[')) {
			try {
				val = JSON.parse(val as string);
			} catch {
				/* keep as string */
			}
		}
		out[key] = val;
	}
	return out;
}

// First token that isn't a key=value pair or a --flag.
function firstPositional(args: string[]): string | undefined {
	return args.find((a) => !a.includes('=') && !a.startsWith('--'));
}

function fmtDate(d: string | Date | null | undefined): string {
	if (!d) return '';
	const dt = typeof d === 'string' ? new Date(d) : d;
	if (Number.isNaN(dt.getTime())) return String(d);
	return dt.toISOString().slice(0, 10);
}

function fmtScore(s: unknown): string {
	return typeof s === 'number' ? s.toFixed(2) : String(s ?? '');
}

function fmtLabels(labels: unknown): string {
	if (!Array.isArray(labels) || labels.length === 0) return '';
	return labels
		.map((l) => (typeof l === 'string' ? l : (l as any)?.name || (l as any)?.slug || ''))
		.filter(Boolean)
		.join(', ');
}

function writeTmp(path: string, content: string) {
	mkdirSync(dirname(path), { recursive: true });
	writeFileSync(path, content, 'utf-8');
}

// ─── Name → UUID resolution ──────────────────────────────────────────────

let _collectionsCache: Array<{ id: string; name: string; description?: string }> | null = null;

async function listCollections() {
	if (_collectionsCache) return _collectionsCache;
	const result = await kbApi('collections', 'list');
	if (result?.errors) throw new Error(apiErr(result, 'collections list failed'));
	const items = (result?.data || []) as Array<{ id: string; attributes: any }>;
	_collectionsCache = items.map((c) => ({
		id: c.id,
		name: c.attributes?.name || '',
		description: c.attributes?.description
	}));
	return _collectionsCache;
}

async function resolveCollection(input: string): Promise<{ id: string; name: string }> {
	const all = await listCollections();
	if (isId(input)) {
		return all.find((c) => c.id === input || c.id.startsWith(input.toLowerCase())) ?? { id: input, name: '' };
	}
	const lower = input.toLowerCase();
	let matches = all.filter((c) => c.name === input);
	if (matches.length === 0) matches = all.filter((c) => c.name.toLowerCase() === lower);
	if (matches.length === 0) matches = all.filter((c) => c.name.toLowerCase().includes(lower));
	if (matches.length === 0) throw new Error(`No collection matching "${input}"`);
	if (matches.length > 1)
		throw new Error(`Ambiguous "${input}". Matches: ${matches.map((m) => `"${m.name}"`).join(', ')}`);
	return matches[0];
}

async function listDocuments(collectionId: string) {
	const result = await kbApi('documents', 'list', { collectionId });
	if (result?.errors) throw new Error(apiErr(result, 'documents list failed'));
	return ((result?.data || []) as Array<any>).map((d) => ({
		id: d.id,
		title: d.attributes?.title || '(untitled)',
		updatedAt: d.attributes?.updatedAt || ''
	}));
}

async function resolveDocument(input: string, scope?: string) {
	if (isId(input)) {
		const result = await kbApi('documents', 'get', { id: input });
		if (result?.errors) throw new Error(apiErr(result, 'document not found'));
		const a = result.data.attributes;
		return { id: result.data.id, title: a?.title || '', collectionId: a?.collectionId || '' };
	}
	if (!scope) throw new Error(`Document name lookup needs scope. Add in=<notebook>.`);
	const coll = await resolveCollection(scope);
	const docs = await listDocuments(coll.id);
	const lower = input.toLowerCase();
	let matches = docs.filter((d) => d.title === input);
	if (matches.length === 0) matches = docs.filter((d) => d.title.toLowerCase() === lower);
	if (matches.length === 0) matches = docs.filter((d) => d.title.toLowerCase().includes(lower));
	if (matches.length === 0) throw new Error(`No document matching "${input}" in "${coll.name}"`);
	if (matches.length > 1)
		throw new Error(`Ambiguous "${input}" in "${coll.name}". Matches: ${matches.map((m) => `"${m.title}"`).join(', ')}`);
	return { id: matches[0].id, title: matches[0].title, collectionId: coll.id };
}

// A label target may be a notebook or a document. Try notebook first.
async function resolveItem(
	target: string,
	scope?: string
): Promise<{ itemType: 'document' | 'collection'; itemId: string; label: string }> {
	if (!scope) {
		try {
			const coll = await resolveCollection(target);
			return { itemType: 'collection', itemId: coll.id, label: coll.name };
		} catch {
			/* fall through to document */
		}
	}
	const doc = await resolveDocument(target, scope);
	return { itemType: 'document', itemId: doc.id, label: doc.title };
}

function resolveSince(raw: unknown): string | null {
	if (typeof raw !== 'string' || !raw.trim()) return null;
	const trimmed = raw.trim();
	const m = trimmed.match(/^(\d+)\s*([dhm])$/i);
	if (m) {
		const n = parseInt(m[1], 10);
		const unit = m[2].toLowerCase();
		const unitMs = unit === 'd' ? 86400_000 : unit === 'h' ? 3600_000 : 60_000;
		return new Date(Date.now() - n * unitMs).toISOString();
	}
	const parsed = Date.parse(trimmed);
	return isNaN(parsed) ? null : new Date(parsed).toISOString();
}

// ─── Navigation verbs ────────────────────────────────────────────────────

async function cmdTree(target: string | undefined, args: string[], json: boolean) {
	let labelSlug: string | undefined;
	let byLabel = false;
	for (const a of args) {
		if (a.startsWith('--label=')) labelSlug = a.slice('--label='.length);
		else if (a === '--by-label') byLabel = true;
		else if (a.startsWith('label=')) labelSlug = a.slice('label='.length);
	}

	if (!target) {
		const colls = await listCollections();
		const tree = await Promise.all(
			colls.map(async (c) => ({ id: c.id, name: c.name, docCount: (await listDocuments(c.id)).length }))
		);

		if (byLabel) {
			const groups = new Map<string, { slug: string; name: string; docs: any[] }>();
			for (const c of colls) {
				const r = await kbApi('documents', 'list', { collectionId: c.id });
				for (const d of (r?.data || []) as Array<any>) {
					for (const l of (d.attributes?.labels ?? []) as Array<any>) {
						const g = groups.get(l.slug) ?? { slug: l.slug, name: l.name, docs: [] as any[] };
						g.docs.push({ id: d.id, title: d.attributes?.title, notebook: c.name });
						groups.set(l.slug, g);
					}
				}
			}
			const grouped = [...groups.values()].sort((a, b) => b.docs.length - a.docs.length);
			const lines = [`# Notebooks grouped by label (${grouped.length} labels)`, ''];
			for (const g of grouped) {
				lines.push(`## ${g.name} (${g.docs.length})`);
				for (const d of g.docs) lines.push(`- ${d.title} _(in ${d.notebook})_`);
				lines.push('');
			}
			return emit(json, { byLabel: grouped }, lines.join('\n').trimEnd());
		}

		if (labelSlug) {
			const tagged: any[] = [];
			for (const c of colls) {
				const r = await kbApi('documents', 'list', { collectionId: c.id, labels: [labelSlug] });
				const matching = (r?.data || []) as Array<any>;
				if (matching.length > 0)
					tagged.push({
						id: c.id,
						name: c.name,
						matchingDocs: matching.map((d) => ({ id: d.id, title: d.attributes?.title }))
					});
			}
			const lines = [`# Notebooks tagged "${labelSlug}" (${tagged.length})`, ''];
			if (tagged.length === 0) lines.push('_None._');
			for (const n of tagged) {
				lines.push(`## ${n.name} (${n.matchingDocs.length} matching)`, `\`${n.id}\``);
				for (const d of n.matchingDocs) lines.push(`- ${d.title} \`${d.id}\``);
				lines.push('');
			}
			return emit(json, { filter: { label: labelSlug }, notebooks: tagged }, lines.join('\n').trimEnd());
		}

		const sorted = [...tree].sort((a, b) => b.docCount - a.docCount);
		const lines = [`# Notebooks (${tree.length})`, ''];
		for (const n of sorted) lines.push(`- **${n.name}** — ${n.docCount} docs \`${n.id}\``);
		return emit(json, { notebooks: tree }, lines.join('\n'));
	}

	const coll = await resolveCollection(target);
	const r = await kbApi('documents', 'list', {
		collectionId: coll.id,
		...(labelSlug ? { labels: [labelSlug] } : {})
	});
	const docs = ((r?.data || []) as Array<any>).map((d) => ({
		id: d.id,
		title: d.attributes?.title || '(untitled)',
		labels: d.attributes?.labels ?? [],
		updatedAt: d.attributes?.updatedAt || ''
	}));
	const lines = [`# ${coll.name} (${docs.length} docs)`, `\`${coll.id}\``];
	if (labelSlug) lines.push(`_filter: label=${labelSlug}_`);
	lines.push('');
	if (docs.length === 0) lines.push('_No documents._');
	for (const d of docs) {
		const labels = fmtLabels(d.labels);
		const labelStr = labels ? ` _(${labels})_` : '';
		const date = d.updatedAt ? ` _${fmtDate(d.updatedAt)}_` : '';
		lines.push(`- ${d.title}${date}${labelStr} \`${d.id}\``);
	}
	return emit(json, { notebook: { id: coll.id, name: coll.name, docCount: docs.length }, documents: docs }, lines.join('\n'));
}

async function cmdPeek(target: string, json: boolean) {
	if (!target) fail('Usage: mm kb peek <name-or-id>');

	const peekCollection = async (id: string, fallbackName = '') => {
		const c = await kbApi('collections', 'get', { id });
		if (c?.errors || !c?.data) return null;
		const a = c.data.attributes;
		const docs = await listDocuments(id);
		const rRes = await kbApi('research', 'list', { collectionId: id });
		const runs = (rRes?.data || []) as Array<any>;
		return {
			type: 'collection',
			id,
			name: a?.name || fallbackName,
			description: a?.description,
			labels: a?.labels ?? [],
			docCount: docs.length,
			recent: docs.slice(0, 5).map((d) => ({ id: d.id, title: d.title, updatedAt: d.updatedAt })),
			researchCount: runs.length,
			recentResearch: runs.slice(0, 5).map((r) => ({
				id: r.id,
				status: r.attributes?.status,
				topic: r.attributes?.topic,
				createdAt: r.attributes?.createdAt
			}))
		};
	};

	const peekDocument = async (id: string) => {
		const d = await kbApi('documents', 'get', { id });
		if (d?.errors || !d?.data) return null;
		const a = d.data.attributes;
		const content = (a?.content as string) || '';
		return {
			type: 'document',
			id,
			title: a?.title,
			shortTitle: a?.shortTitle,
			collectionId: a?.collectionId,
			labels: a?.labels ?? [],
			summary: a?.summary,
			outline: Array.isArray(a?.outline) ? a.outline : null,
			contentLength: content.length,
			snippet: content.slice(0, 300) + (content.length > 300 ? '…' : ''),
			createdAt: a?.createdAt
		};
	};

	let data: any = null;
	if (isId(target)) {
		data = (await peekCollection(target)) ?? (await peekDocument(target));
		if (!data) fail(`${target} not found as collection or document`);
	} else {
		const coll = await resolveCollection(target); // throws with candidates if ambiguous
		data = await peekCollection(coll.id, coll.name);
		if (!data) fail(`Collection "${target}" exists but couldn't load`);
	}
	return emit(json, data, renderPeek(data));
}

function renderPeek(data: any): string {
	const lines: string[] = [];
	if (data.type === 'collection') {
		lines.push(`# ${data.name}`, '');
		const meta = [`id: \`${data.id}\``, `${data.docCount} docs`];
		const labels = fmtLabels(data.labels);
		if (labels) meta.push(`labels: ${labels}`);
		lines.push(meta.join(' · '), '');
		if (data.description) lines.push(data.description, '');
		if (data.recent?.length) {
			lines.push('## Recent documents', '');
			for (const d of data.recent) lines.push(`- ${d.title} \`${d.id}\` _(${fmtDate(d.updatedAt)})_`);
			lines.push('');
		}
		if (data.recentResearch?.length) {
			lines.push(`## Research runs (${data.researchCount})`, '');
			for (const r of data.recentResearch) {
				const status = r.status ? ` _[${r.status}]_` : '';
				lines.push(`- \`${r.id}\`${status} _(${fmtDate(r.createdAt)})_`);
			}
		}
		return lines.join('\n').trimEnd();
	}
	const title = data.shortTitle || data.title;
	lines.push(`# ${title}`);
	if (data.shortTitle && data.title && data.shortTitle !== data.title) lines.push(`_${data.title}_`);
	lines.push('');
	const meta = [`id: \`${data.id}\``];
	if (data.collectionId) meta.push(`collection: \`${data.collectionId}\``);
	if (data.createdAt) meta.push(`created: ${fmtDate(data.createdAt)}`);
	meta.push(`${data.contentLength} chars`);
	lines.push(meta.join(' · '), '');
	const labels = fmtLabels(data.labels);
	if (labels) lines.push(`**Labels:** ${labels}`, '');
	if (data.summary) lines.push('## Summary', '', data.summary, '');
	if (Array.isArray(data.outline) && data.outline.length) {
		lines.push(`## Outline (${data.outline.length})`, '');
		for (const o of data.outline) lines.push(`${o.chunkIndex + 1}. ${o.heading}  \`chunkIndex=${o.chunkIndex}\``);
		lines.push('');
	}
	if (data.snippet) lines.push('## First 300 chars', '', '```', data.snippet, '```');
	return lines.join('\n').trimEnd();
}

async function cmdRead(target: string, args: string[], json: boolean) {
	if (!target) fail('Usage: mm kb read <doc-id-or-name> [path=/tmp/file.md] [inline=true] [in=<notebook>]');
	const payload = parseArgs(args);
	let docId = target;
	let title = '';
	if (!isId(target)) {
		const resolved = await resolveDocument(target, payload.in as string | undefined);
		docId = resolved.id;
		title = resolved.title;
	}
	const result = await kbApi('documents', 'get', { id: docId });
	if (result?.errors) fail(apiErr(result, 'document not found'));
	const a = result?.data?.attributes;
	if (!a) fail('Document not found');
	const content = a.content as string | undefined;
	if (!content) fail('Document has no content');

	const INLINE_MAX = 8192;
	const path = (payload.path as string) || `/tmp/kb-${docId}.md`;

	if (payload.inline === true && content.length <= INLINE_MAX) {
		return emit(json, { id: docId, title: a.title || title, size: content.length, content }, content);
	}
	writeTmp(path, content);
	const structured = { written: true, path, id: docId, title: a.title || title, size: content.length };
	const note =
		payload.inline === true
			? `Doc is ${content.length} chars (inline limit ${INLINE_MAX}). Wrote "${a.title || docId}" → \`${path}\` instead.`
			: `Wrote "${a.title || docId}" → \`${path}\` (${content.length} chars)`;
	return emit(json, structured, note);
}

async function cmdFind(query: string, args: string[], json: boolean) {
	if (!query) fail('Usage: mm kb find "<query>" [in=<notebook>] [limit=10] [minScore=0.45] [since=7d] [full=true]');
	const payload = parseArgs(args);
	const scope = payload.in as string | undefined;
	const limit = (payload.limit as number) || 10;
	const minScore = payload.minScore as number | undefined;
	const full = payload.full === true;
	const since = resolveSince(payload.since);
	if (payload.since !== undefined && !since)
		fail(`Could not parse since="${payload.since}". Use ISO date or duration (7d, 24h, 30m).`);

	let result: any;
	if (scope) {
		const coll = await resolveCollection(scope);
		result = await kbApi('documents', 'search', {
			query,
			collectionId: coll.id,
			limit,
			...(minScore !== undefined && { minScore }),
			...(since && { since })
		});
	} else {
		result = await kbApi('documents', 'searchCorpus', {
			query,
			limit,
			...(minScore !== undefined && { minScore }),
			...(since && { since })
		});
	}
	if (result?.errors) fail(apiErr(result, 'search failed'));

	const SNIPPET = full ? Infinity : 200;
	const results = ((result?.data || []) as Array<any>).map((it) => {
		const a = it.attributes || {};
		const chunk: string = a.chunk || '';
		return {
			level: a.level === 1 ? 'doc-summary' : 'chunk',
			score: typeof a.score === 'number' ? Number(a.score.toFixed(3)) : a.score,
			heading: a.heading || null,
			doc: { id: a.documentId || a.document_id, title: a.documentTitle || a.title },
			chunkIndex: a.chunkIndex ?? a.chunk_index,
			snippet: chunk.length > SNIPPET ? chunk.slice(0, SNIPPET) + '…' : chunk
		};
	});

	const lines = [`# Search: "${query}" — ${results.length} result${results.length === 1 ? '' : 's'}`, `_scope: ${scope ?? 'all-notebooks'}_`, ''];
	if (results.length === 0) lines.push('_No results._');
	results.forEach((r, i) => {
		lines.push(`## ${i + 1}. ${r.heading || `(no heading, ${r.level})`}`);
		const meta = [
			r.doc.title ? `_${r.doc.title}_` : '',
			`score ${fmtScore(r.score)}`,
			r.level !== 'chunk' ? `_${r.level}_` : '',
			r.chunkIndex != null ? `chunkIndex ${r.chunkIndex}` : ''
		].filter(Boolean);
		lines.push(meta.join(' · '), `\`${r.doc.id}\``, '');
		if (r.snippet) {
			const block = full ? r.snippet : r.snippet.split('\n').slice(0, 6).join('\n');
			lines.push('> ' + block.replace(/\n/g, '\n> '), '');
		}
	});
	return emit(json, { query, scope: scope ?? 'all-notebooks', count: results.length, results }, lines.join('\n').trimEnd());
}

async function cmdRelated(target: string, args: string[], json: boolean) {
	if (!target) fail('Usage: mm kb related <doc-id-or-name> [in=<notebook>] [scope=<notebook>] [limit=10] [minScore=0.5]');
	const payload = parseArgs(args);
	const limit = (payload.limit as number) || 10;
	const minScore = payload.minScore as number | undefined;
	const docId = isId(target) ? target : (await resolveDocument(target, payload.in as string | undefined)).id;
	let collectionId: string | undefined;
	if (payload.scope) collectionId = (await resolveCollection(payload.scope as string)).id;

	const result = await kbApi('documents', 'related', {
		id: docId,
		limit,
		...(minScore !== undefined && { minScore }),
		...(collectionId && { collectionId })
	});
	if (result?.errors) fail(apiErr(result, 'related failed'));

	const results = ((result?.data || []) as Array<any>).map((it) => {
		const a = it.attributes || {};
		return {
			score: typeof a.score === 'number' ? Number(a.score.toFixed(3)) : a.score,
			doc: { id: a.id, title: a.shortTitle || a.title },
			summary: a.summary || ''
		};
	});
	const lines = [`# Related to \`${docId}\` — ${results.length} result${results.length === 1 ? '' : 's'}`, ''];
	if (results.length === 0) lines.push('_No related docs above threshold._');
	results.forEach((r, i) => {
		lines.push(`## ${i + 1}. ${r.doc.title}`, `score ${fmtScore(r.score)} · \`${r.doc.id}\``, '');
		if (r.summary) lines.push(r.summary, '');
	});
	return emit(json, { source: docId, count: results.length, results }, lines.join('\n').trimEnd());
}

async function cmdTagged(slug: string, args: string[], json: boolean) {
	if (!slug) fail('Usage: mm kb tagged <label-slug> [in=<notebook>] [limit=50]');
	const payload = parseArgs(args);
	const out: Record<string, unknown> = { labels: [slug], labelMode: 'any', limit: (payload.limit as number) || 50 };
	if (payload.in) out.collectionId = (await resolveCollection(payload.in as string)).id;
	const result = await kbApi('documents', 'list', out);
	if (result?.errors) fail(apiErr(result, 'tagged failed'));

	const docs = ((result?.data || []) as Array<any>).map((it) => {
		const a = it.attributes || {};
		return { id: it.id || a.id, title: a.shortTitle || a.title, updatedAt: a.updatedAt || a.createdAt };
	});
	const lines = [`# Tagged "${slug}" — ${docs.length} doc${docs.length === 1 ? '' : 's'}`, ''];
	if (docs.length === 0) lines.push('_No documents._');
	for (const d of docs) lines.push(`- ${d.title}${d.updatedAt ? ` _(${fmtDate(d.updatedAt)})_` : ''} \`${d.id}\``);
	return emit(json, { tag: slug, count: docs.length, documents: docs }, lines.join('\n'));
}

async function cmdMentions(entity: string, args: string[], json: boolean) {
	if (!entity) fail('Usage: mm kb mentions "<entity>" [in=<notebook>] [limit=25]');
	const payload = parseArgs(args);
	const out: Record<string, unknown> = { entity, limit: (payload.limit as number) || 25 };
	if (payload.in) out.collectionId = (await resolveCollection(payload.in as string)).id;
	const result = await kbApi('documents', 'mentions', out);
	if (result?.errors) fail(apiErr(result, 'mentions failed'));

	const items = (result?.data || []) as Array<any>;
	const entityMatches = items
		.filter((it) => it.attributes?.matchType === 'entity')
		.map((it) => {
			const a = it.attributes || {};
			const matched = (a.entities as Array<any> | undefined)?.find(
				(e) => e.name.toLowerCase() === entity.toLowerCase()
			);
			return { doc: { id: a.documentId, title: a.shortTitle || a.documentTitle }, as: matched ? `${matched.name} (${matched.type})` : entity };
		});
	const textMatches = items
		.filter((it) => it.attributes?.matchType === 'text')
		.map((it) => {
			const a = it.attributes || {};
			return { doc: { id: a.documentId, title: a.shortTitle || a.documentTitle }, heading: a.heading, chunkIndex: a.chunkIndex, snippet: a.snippet };
		});

	const total = entityMatches.length + textMatches.length;
	const lines = [`# Mentions of "${entity}" — ${total} hit${total === 1 ? '' : 's'}`, ''];
	if (entityMatches.length) {
		lines.push(`## Entity matches (${entityMatches.length})`, '');
		for (const m of entityMatches) lines.push(`- **${m.doc.title}** \`${m.doc.id}\` — as ${m.as}`);
		lines.push('');
	}
	if (textMatches.length) {
		lines.push(`## Text matches (${textMatches.length})`, '');
		for (const m of textMatches) {
			lines.push(`### ${m.heading || '(no heading)'}`, `_${m.doc.title}_ · chunk ${m.chunkIndex} · \`${m.doc.id}\``);
			if (m.snippet) lines.push('', '> ' + m.snippet.replace(/\n/g, '\n> '));
			lines.push('');
		}
	}
	if (total === 0) lines.push('_No mentions._');
	return emit(
		json,
		{ entity, entityMatches: { count: entityMatches.length, results: entityMatches }, textMatches: { count: textMatches.length, results: textMatches } },
		lines.join('\n').trimEnd()
	);
}

async function cmdDigest(target: string, args: string[], json: boolean) {
	if (!target) fail('Usage: mm kb digest <notebook> [force=true]');
	const payload = parseArgs(args);
	const coll = await resolveCollection(target);
	const result = await kbApi('collections', 'digest', { id: coll.id, ...(payload.force === true && { force: true }) });
	if (result?.errors) fail(apiErr(result, 'digest failed'));
	const a = result?.data?.attributes || {};
	const lines = [
		`# ${coll.name} — digest`,
		[`${a.docCount} docs`, `generated ${fmtDate(a.generatedAt)}`, a.fromCache ? '_(cached)_' : '_(fresh)_'].join(' · '),
		'',
		a.digest || '_No digest yet — add docs to the notebook to generate one._'
	];
	return emit(json, { notebook: { id: coll.id, name: coll.name }, digest: a.digest, docCount: a.docCount, fromCache: a.fromCache }, lines.join('\n').trimEnd());
}

async function cmdSurface(target: string | undefined, args: string[], json: boolean) {
	const payload = parseArgs(args);
	const out: Record<string, unknown> = {};
	if (target) out.id = (await resolveCollection(target)).id;
	for (const k of ['recentLimit', 'questionLimit', 'contradictionLimit'])
		if (payload[k] !== undefined) out[k] = payload[k];

	const result = await kbApi('collections', 'surface', out);
	if (result?.errors) fail(apiErr(result, 'surface failed'));
	const a = result?.data?.attributes || {};

	const recent = (a.recentlyAddedHighImportance || []).map((r: any) => ({
		doc: { id: r.documentId, title: r.shortTitle || r.title },
		importance: r.importanceScore != null ? Number(r.importanceScore.toFixed(2)) : null,
		decayed: r.decayedScore != null ? Number(r.decayedScore.toFixed(2)) : null,
		summary: r.summary,
		addedAt: r.createdAt
	}));
	const questions = (a.openQuestions || []).map((q: any) => ({
		question: q.question,
		raisedByDocId: q.raisedByDocId,
		raisedAt: q.raisedAt,
		resolvedByDocId: q.resolvedByDocId || null
	}));
	const contradictions = (a.contradictions || []).map((c: any) => ({
		between: [
			{ id: c.sourceDocId, title: c.sourceTitle },
			{ id: c.targetDocId, title: c.targetTitle }
		],
		reason: c.reason,
		evidence: c.evidenceQuote,
		confidence: Number(c.confidence?.toFixed(2)),
		observedAt: c.observedAt
	}));

	const lines = [`# Surface — ${target ? target : 'all notebooks'}`, ''];
	lines.push(`## Recently added (${recent.length})`, '');
	if (recent.length === 0) lines.push('_None._');
	for (const r of recent) {
		lines.push(`### ${r.doc.title}`);
		const meta = [`id: \`${r.doc.id}\``, `added: ${fmtDate(r.addedAt)}`];
		if (r.importance != null) meta.push(`importance: ${fmtScore(r.importance)}`);
		if (r.decayed != null && r.decayed !== r.importance) meta.push(`decayed: ${fmtScore(r.decayed)}`);
		lines.push(meta.join(' · '), '');
		if (r.summary) lines.push(r.summary, '');
	}
	lines.push(`## Open questions (${questions.length})`, '');
	if (questions.length === 0) lines.push('_None._');
	for (const q of questions)
		lines.push(`- ${q.question}${q.resolvedByDocId ? ' _(resolved)_' : ''}`, `  · raised ${fmtDate(q.raisedAt)} by \`${q.raisedByDocId}\``);
	lines.push('', `## Contradictions (${contradictions.length})`, '');
	if (contradictions.length === 0) lines.push('_None._');
	for (const c of contradictions) {
		lines.push(`### "${c.between[0].title}" ⇄ "${c.between[1].title}"`, `confidence: ${fmtScore(c.confidence)} · ${fmtDate(c.observedAt)}`, '', c.reason, '');
		if (c.evidence) lines.push(`> ${c.evidence}`, '');
	}
	return emit(
		json,
		{
			scope: a.collectionId ?? 'all-notebooks',
			recentlyAdded: { count: recent.length, items: recent },
			openQuestions: { count: questions.length, items: questions },
			contradictions: { count: contradictions.length, items: contradictions }
		},
		lines.join('\n').trimEnd()
	);
}

// ─── Mutation verbs ──────────────────────────────────────────────────────

async function cmdRename(target: string, args: string[], json: boolean) {
	const newName = args.slice(1).filter((a) => !a.includes('=')).join(' ').trim() || (parseArgs(args).name as string);
	if (!target || !newName) fail('Usage: mm kb rename <notebook-or-doc> "<new name>" [in=<notebook>]');
	const payload = parseArgs(args);
	const scope = payload.in as string | undefined;

	// Notebook unless a scope (in=) forces document resolution.
	let result: any;
	let kind: string;
	if (!scope && !isId(target)) {
		const coll = await resolveCollection(target);
		result = await kbApi('collections', 'update', { id: coll.id, name: newName });
		kind = 'notebook';
	} else if (isId(target)) {
		// Ambiguous id — try collection first, fall back to document title.
		const asColl = await kbApi('collections', 'update', { id: target, name: newName });
		if (!asColl?.errors) {
			result = asColl;
			kind = 'notebook';
		} else {
			result = await kbApi('documents', 'update', { id: target, title: newName });
			kind = 'document';
		}
	} else {
		const doc = await resolveDocument(target, scope);
		result = await kbApi('documents', 'update', { id: doc.id, title: newName });
		kind = 'document';
	}
	if (result?.errors) fail(apiErr(result, 'rename failed'));
	return emit(json, result, `Renamed ${kind} → "${newName}"`);
}

async function cmdMove(target: string, args: string[], json: boolean) {
	const payload = parseArgs(args);
	// Accept `mm kb move <doc> to <notebook>` or `to=<notebook>`.
	const toIdx = args.indexOf('to');
	const dest = (payload.to as string) || (toIdx >= 0 ? args[toIdx + 1] : undefined);
	if (!target || !dest) fail('Usage: mm kb move <doc> to "<notebook>" [in=<notebook>]');
	const doc = await resolveDocument(target, payload.in as string | undefined);
	const coll = await resolveCollection(dest);
	const result = await kbApi('documents', 'move', { id: doc.id, toCollectionId: coll.id });
	if (result?.errors) fail(apiErr(result, 'move failed'));
	return emit(json, result, `Moved "${doc.title}" → ${coll.name}`);
}

async function cmdAdd(target: string, args: string[], json: boolean) {
	if (!target) fail('Usage: mm kb add <notebook> url=<url> | content=<text> [title=<title>]');
	const payload = parseArgs(args);
	if (!payload.url && !payload.content)
		fail('Provide url= or content= (use content@=/path/to/file to read a file)');
	const coll = await resolveCollection(target);
	const result = await kbApi('documents', 'create', { collectionId: coll.id, ...payload });
	if (result?.errors) fail(apiErr(result, 'add failed'));
	const a = result?.data?.attributes || {};
	return emit(json, result, `Added "${a.title || payload.title || payload.url || '(untitled)'}" → ${coll.name} \`${result?.data?.id}\``);
}

async function cmdRm(target: string, args: string[], json: boolean) {
	if (!target) fail('Usage: mm kb rm <doc> [in=<notebook>]');
	const payload = parseArgs(args);
	const docId = isId(target) ? target : (await resolveDocument(target, payload.in as string | undefined)).id;
	const result = await kbApi('documents', 'remove', { id: docId });
	if (result?.errors) fail(apiErr(result, 'remove failed'));
	return emit(json, result, `Removed \`${docId}\``);
}

// ─── Label verbs ─────────────────────────────────────────────────────────

async function cmdLabel(sub: string, args: string[], json: boolean) {
	const target = firstPositional(args);
	const payload = parseArgs(args);
	const scope = payload.in as string | undefined;

	if (sub === 'add' || sub === 'set') {
		const labels = args.filter((a) => a !== target && !a.includes('=') && !a.startsWith('--'));
		if (!target || labels.length === 0) fail(`Usage: mm kb label ${sub} "<target>" <label> [<label> ...] [in=<notebook>]`);
		const item = await resolveItem(target, scope);
		if (sub === 'set') {
			const result = await kbApi('labels', 'replaceFor', { itemType: item.itemType, itemId: item.itemId, labels });
			if (result?.errors) fail(apiErr(result, 'replaceFor failed'));
			return emit(json, { item, labels: result.data }, `Set labels on ${item.itemType} "${item.label}": ${labels.join(', ')}`);
		}
		let last: any;
		for (const l of labels) last = await kbApi('labels', 'attach', { itemType: item.itemType, itemId: item.itemId, label: l });
		return emit(json, { item, attached: labels, current: last?.data ?? [] }, `Tagged ${item.itemType} "${item.label}" with: ${labels.join(', ')}`);
	}

	if (sub === 'rm') {
		const labelInput = args.filter((a) => a !== target && !a.includes('=') && !a.startsWith('--'))[0];
		if (!target || !labelInput) fail('Usage: mm kb label rm "<target>" <label-id-or-slug> [in=<notebook>]');
		const item = await resolveItem(target, scope);
		let labelId = labelInput;
		if (!isId(labelInput)) {
			const lookup = await kbApi('labels', 'get', { slug: labelInput });
			if (lookup?.errors || !lookup?.data?.id) fail(`Label not found: ${labelInput}`);
			labelId = lookup.data.id;
		}
		const result = await kbApi('labels', 'detach', { itemType: item.itemType, itemId: item.itemId, labelId });
		if (result?.errors) fail(apiErr(result, 'detach failed'));
		return emit(json, { item, detached: labelInput, current: result.data }, `Removed "${labelInput}" from ${item.itemType} "${item.label}"`);
	}

	if (sub === 'suggest') {
		if (!target) fail('Usage: mm kb label suggest "<notebook>" [--apply] [--force]');
		const apply = args.includes('--apply') || payload.apply === true;
		const force = args.includes('--force') || payload.force === true;
		const coll = await resolveCollection(target);
		const result = await kbApi('labels', 'suggestForCollection', { collectionId: coll.id, force });
		if (result?.errors) fail(apiErr(result, 'suggestForCollection failed'));
		const candidates = result?.data?.attributes?.candidates ?? [];
		if (!apply) return emit(json, { notebook: coll, cached: result?.data?.attributes?.cached ?? false, candidates }, JSON.stringify(candidates, null, 2));
		if (candidates.length === 0) return emit(json, { notebook: coll, applied: [] }, 'No candidates to apply.');
		const applied = await kbApi('labels', 'applySuggestion', { collectionId: coll.id, candidates });
		if (applied?.errors) fail(apiErr(applied, 'applySuggestion failed'));
		return emit(json, { notebook: coll, applied: applied?.data?.attributes?.applied ?? [] }, `Applied ${applied?.data?.attributes?.applied?.length ?? 0} labels to ${coll.name}`);
	}

	if (sub === 'rename') {
		const name = payload.name as string | undefined;
		if (!name) fail('Usage: mm kb label rename slug=<slug>|id=<uuid> name=<name>');
		let labelId = payload.id as string | undefined;
		if (!labelId && payload.slug) {
			const lookup = await kbApi('labels', 'get', { slug: payload.slug });
			if (lookup?.errors || !lookup?.data?.id) fail(`Label not found: ${payload.slug}`);
			labelId = lookup.data.id;
		}
		if (!labelId) fail('Provide id= or slug=');
		const result = await kbApi('labels', 'rename', { id: labelId, name });
		if (result?.errors) fail(apiErr(result, 'rename failed'));
		return emit(json, result, `Renamed label → "${name}"`);
	}

	if (sub === 'merge') {
		const resolveLabel = async (input: string): Promise<string> => {
			if (isId(input)) return input;
			const lookup = await kbApi('labels', 'get', { slug: input });
			if (lookup?.errors || !lookup?.data?.id) fail(`Label not found: ${input}`);
			return lookup.data.id;
		};
		if (!payload.from || !payload.to) fail('Usage: mm kb label merge from=<id-or-slug> to=<id-or-slug>');
		const result = await kbApi('labels', 'merge', { fromId: await resolveLabel(payload.from as string), toId: await resolveLabel(payload.to as string) });
		if (result?.errors) fail(apiErr(result, 'merge failed'));
		return emit(json, result, `Merged ${payload.from} → ${payload.to}`);
	}

	if (sub === 'list' || sub === undefined) {
		const result = await kbApi('labels', 'list');
		if (result?.errors) fail(apiErr(result, 'labels list failed'));
		const labels = ((result?.data || []) as Array<any>).map((l) => ({ slug: l.attributes?.slug || l.id, name: l.attributes?.name, count: l.attributes?.usageCount ?? l.attributes?.count }));
		const lines = [`# Labels (${labels.length})`, ''];
		for (const l of labels) lines.push(`- ${l.name} \`${l.slug}\`${l.count != null ? ` _(${l.count})_` : ''}`);
		return emit(json, { labels }, lines.join('\n'));
	}

	fail(`Unknown label subcommand "${sub}". Use: add | rm | set | suggest | rename | merge | list`);
}

async function cmdDescribe(target: string | undefined, args: string[], json: boolean) {
	const payload = parseArgs(args);
	const dryRun = args.includes('--dry-run') || payload.dryRun === true;
	const placeholders = args.includes('--placeholders') || payload.placeholders === true;

	const targets: { id: string; name: string }[] = [];
	if (placeholders) {
		const list = await listCollections();
		for (const c of list) {
			const d = c.description ?? '';
			if (!d || d === 'A new notebook collection' || d.startsWith('Automatically imported from') || d.trim().length < 10)
				targets.push({ id: c.id, name: c.name });
		}
		if (targets.length === 0) return emit(json, { message: 'No placeholder descriptions found.' }, 'No placeholder descriptions found.');
	} else {
		if (!target) fail('Usage: mm kb describe "<notebook>" [--dry-run]  |  mm kb describe --placeholders [--dry-run]');
		targets.push(await resolveCollection(target));
	}

	const results: any[] = [];
	for (const t of targets) {
		const r = await kbApi('labels', 'synthesiseForCollection', { collectionId: t.id, dryRun });
		if (r?.errors) {
			results.push({ notebook: t.name, error: apiErr(r, 'failed') });
			continue;
		}
		const a = r?.data?.attributes ?? {};
		results.push({ notebook: t.name, description: a.description, currentDescription: a.currentDescription, labels: a.labels, change: a.change });
	}
	const lines: string[] = [];
	for (const r of results) {
		lines.push(`# ${r.notebook}${dryRun ? ' (dry-run)' : ''}`);
		if (r.error) lines.push(`_error: ${r.error}_`, '');
		else lines.push(r.description || '_no change_', '');
	}
	return emit(json, results.length === 1 ? results[0] : results, lines.join('\n').trimEnd());
}

// ─── Research / docs convenience ─────────────────────────────────────────

async function cmdResearchList(args: string[], json: boolean) {
	const payload = parseArgs(args);
	let collectionId = payload.collectionId as string | undefined;
	const nb = (payload.in as string) || firstPositional(args);
	if (!collectionId && nb) collectionId = (await resolveCollection(nb)).id;
	if (!collectionId) fail('Usage: mm kb research list <notebook>|collectionId=<uuid>');
	const result = await kbApi('research', 'list', { collectionId });
	if (result?.errors) fail(apiErr(result, 'research list failed'));
	const items = ((result?.data || []) as Array<any>).map((it) => {
		const a = it.attributes || {};
		return { id: it.id, status: a.status, documentId: a.documentId, topic: a.topic, promptHead: typeof a.prompt === 'string' ? a.prompt.slice(0, 200) : '', createdAt: a.createdAt };
	});
	const lines = [`# Research runs (${items.length})`, ''];
	if (items.length === 0) lines.push('_No research runs in this collection._');
	for (const r of items) {
		const docHint = r.documentId ? ` · doc \`${String(r.documentId).slice(0, 8)}\`` : '';
		lines.push(`- \`${r.id}\`${r.status ? ` _[${r.status}]_` : ''} _(${fmtDate(r.createdAt)})_${docHint}`);
		if (r.promptHead) lines.push(`  · ${r.promptHead.replace(/\s+/g, ' ').trim()}${r.promptHead.length >= 200 ? '…' : ''}`);
	}
	return emit(json, { count: items.length, items }, lines.join('\n'));
}

async function cmdDownload(id: string, args: string[], json: boolean) {
	const payload = parseArgs(args);
	const path = payload.path as string;
	if (!id || !path) fail('Usage: mm kb download <doc-id> path=/path/to/file');
	const result = await kbApi('documents', 'get', { id });
	if (result?.errors) fail(apiErr(result, 'document not found'));
	const a = result?.data?.attributes;
	if (!a?.content) fail('Document has no content');
	writeTmp(path, a.content);
	return emit(json, { written: true, path, id, title: a.title, size: a.content.length }, `Wrote "${a.title}" → ${path} (${a.content.length} chars)`);
}

async function cmdActions(json: boolean) {
	const result = await kbApi('meta', 'actions');
	if (result?.errors) fail(apiErr(result, 'meta.actions failed — server may predate the introspection endpoint'));
	const features = (result?.data?.features || result?.data?.attributes?.features || []) as Array<any>;
	const lines = ['# KB RPC surface', ''];
	for (const f of features) lines.push(`## ${f.feature} _(${f.type})_`, '', f.actions.map((a: string) => `\`${a}\``).join(' · '), '');
	return emit(json, result?.data ?? result, lines.join('\n').trimEnd());
}

async function cmdStatus(json: boolean) {
	const data = await kbApi('status', 'get').catch((e) => ({ errors: [{ detail: String(e) }] }));
	return emit(json, data, `KB: ${JSON.stringify(data, null, 2)}`);
}

// Generic passthrough — any feature.action the API knows.
async function kbPassThrough(feature: string, args: string[], json: boolean) {
	if (args.length === 0) fail(`Usage: mm kb ${feature} <action> [key=value ...]\nRun 'mm kb help' or 'mm kb actions' for the full surface.`);
	const action = args[0];
	const payload = parseArgs(args.slice(1));
	const result = await kbApi(feature, action, payload);
	if (result?.errors) fail(apiErr(result, `${feature}.${action} failed`));
	console.log(JSON.stringify(result, null, 2));
}

// ─── Dispatch ────────────────────────────────────────────────────────────

export async function kbDispatch(command: string, args: string[], flags: { json?: boolean }) {
	const json = flags?.json || parseArgs(args).json === true;

	try {
		switch (command) {
			case '':
			case 'help':
			case '--help':
			case '-h':
				return printKbHelp();

			// Navigation
			case 'find':
				return await cmdFind(args[0] || '', args.slice(1), json);
			case 'tree':
				return await cmdTree(firstPositional(args), args, json);
			case 'peek':
				return await cmdPeek(args[0] || '', json);
			case 'read':
				return await cmdRead(args[0] || '', args.slice(1), json);
			case 'related':
				return await cmdRelated(args[0] || '', args.slice(1), json);
			case 'tagged':
				return await cmdTagged(args[0] || '', args.slice(1), json);
			case 'mentions':
				return await cmdMentions(args[0] || '', args.slice(1), json);
			case 'digest':
				return await cmdDigest(args[0] || '', args.slice(1), json);
			case 'surface':
				return await cmdSurface(firstPositional(args), args, json);

			// Mutation
			case 'rename':
				return await cmdRename(args[0] || '', args, json);
			case 'move':
				return await cmdMove(args[0] || '', args.slice(1), json);
			case 'add':
				return await cmdAdd(args[0] || '', args.slice(1), json);
			case 'rm':
			case 'remove':
				return await cmdRm(args[0] || '', args.slice(1), json);
			case 'tag':
				return await cmdLabel('add', args, json);
			case 'untag':
				return await cmdLabel('rm', args, json);
			case 'label':
			case 'labels':
				return await cmdLabel(args[0], args.slice(1), json);
			case 'describe':
				return await cmdDescribe(firstPositional(args), args, json);

			// Collections / notebooks listing
			case 'collections':
			case 'col':
			case 'notebooks': {
				const sub = args[0];
				if (!sub || sub === 'list') return await cmdTree(undefined, [], json);
				return await kbPassThrough('collections', args, json);
			}

			// Research triage
			case 'research': {
				if (args[0] === 'list') return await cmdResearchList(args.slice(1), json);
				return await kbPassThrough('research', args, json);
			}

			// Misc
			case 'download':
				return await cmdDownload(args[0] || '', args.slice(1), json);
			case 'actions':
			case 'introspect':
				return await cmdActions(json);
			case 'status':
				return await cmdStatus(json);
			case 'sql':
				return await sqlDispatch(args, flags, kbDb());

			default:
				return await kbPassThrough(command, args, json);
		}
	} catch (err) {
		fail(err instanceof Error ? err.message : String(err));
	}
}

export function printKbHelp() {
	console.log(`mm kb — Knowledge Base

Names resolve to UUIDs anywhere. Read verbs are markdown by default; add --json for structured output.

Navigation:
  find "<query>" [in=<nb>] [limit=10] [minScore=0.45] [since=7d] [full=true]
  tree [<notebook>] [--label=<slug>] [--by-label]   Notebooks, or docs in one
  peek <name-or-id>                Summary of a notebook or document (no full body)
  read <doc> [path=…] [inline=true] [in=<nb>]        Write doc to /tmp (or inline if small)
  related <doc> [in=<nb>] [scope=<nb>] [limit] [minScore]
  tagged <label-slug> [in=<nb>] [limit=50]
  mentions "<entity>" [in=<nb>] [limit=25]           Backlinks: entity + text matches
  digest <notebook> [force=true]   ~400-token narrative overview (cached)
  surface [<notebook>]             Recently-added + open questions + contradictions

Mutation:
  rename <notebook-or-doc> "<new name>" [in=<nb>]    Notebook name / doc title
  move <doc> to "<notebook>" [in=<nb>]
  add <notebook> url=<url> | content=<text> [title=…]  (content@=/path to read a file)
  rm <doc> [in=<nb>]
  tag <target> <label> [<label> …] [in=<nb>]         Attach labels
  untag <target> <label> [in=<nb>]                   Detach a label
  label set <target> <l1> <l2> … [in=<nb>]           Replace label set
  label suggest "<notebook>" [--apply] [--force]
  label rename slug=<slug>|id=<uuid> name=<name>
  label merge from=<id-or-slug> to=<id-or-slug>
  label list
  describe "<notebook>" [--dry-run] | describe --placeholders   Auto description + labels

Other:
  collections | notebooks          List notebooks (alias for tree)
  research list <notebook>         Research runs in a notebook
  download <doc-id> path=<file>    Write doc content to a path
  actions                          List the full RPC surface (self-discovery)
  status                           KB health + auth check
  sql "<query>"                    Run SQL against the KB DB (read+write)

Raw dispatch (any feature.action the API knows):
  mm kb <feature> <action> [key=value ...]
  e.g. mm kb documents searchCorpus query="…"  ·  mm kb jobs list

Run 'mm kb actions' for the live list of every feature.action.`);
}

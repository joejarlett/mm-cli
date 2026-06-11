// ⚠️ LEGACY TYPESCRIPT PORT — NOT the live `mm` binary. The live CLI is Go;
// this command lives in internal/cmd/. Editing this file changes nothing in
// `mm` (it only builds the separate, unused `mm-ts`). Fix the .go file instead.

/**
 * Generic per-app dispatcher — the universal verb set.
 *
 *   mm <app>                         render the app's Agent Card
 *   mm <app> ask "..."               POST agent.chat
 *   mm <app> find "..."              POST agent.search   (caps.search gated)
 *   mm <app> do <tool> [k=v…]        invoke a Card-declared tool
 *   mm <app> <feature> <action> …    raw fallback (replaces mm v2)
 *   mm <app> <alias>                 if the Card publishes an `aliases` entry
 *
 * Per-app wrappers (`mm kb …`, `mm crm …`) take precedence in
 * [index.ts](./index.ts) — this generic path runs for any registered
 * app that isn't otherwise handled. Long-term the wrappers go away and
 * everything routes through here.
 */

import { dispatch } from '../dispatcher';
import {
	loadAgentCard,
	hasCapability,
	findTool,
	findAlias,
	type AgentCard
} from '../agent-card';
import { cardsDispatch } from './cards';

function parseKeyValueArgs(args: string[]): Record<string, unknown> {
	const out: Record<string, unknown> = {};
	for (const a of args) {
		const eq = a.indexOf('=');
		if (eq <= 0) continue;
		const key = a.slice(0, eq);
		let val: unknown = a.slice(eq + 1);
		if (val === 'true') val = true;
		else if (val === 'false') val = false;
		else if (typeof val === 'string' && /^-?\d+$/.test(val)) val = parseInt(val, 10);
		else if (typeof val === 'string' && /^-?\d+\.\d+$/.test(val)) val = parseFloat(val);
		out[key] = val;
	}
	return out;
}

interface DispatchFlags {
	json?: boolean;
	instance?: string;
	noValidate?: boolean;
	refresh?: boolean;
}

async function runDispatch(
	slug: string,
	featureAction: string,
	payload: Record<string, unknown>,
	flags: DispatchFlags
) {
	const result = await dispatch(slug, featureAction, payload, {
		validate: !flags.noValidate,
		instanceId: flags.instance
	});

	if (flags.json) {
		console.log(JSON.stringify(result.body, null, 2));
	} else {
		if (
			!result.ok &&
			typeof result.body === 'object' &&
			result.body !== null &&
			'errors' in result.body
		) {
			const errors = (result.body as { errors: Array<{ code?: string; message?: string }> }).errors;
			for (const e of errors) {
				console.error(`✗ HTTP ${result.status} [${e.code ?? '?'}] ${e.message ?? '(no message)'}`);
			}
		} else {
			if (!result.ok) console.error(`HTTP ${result.status}`);
			console.log(typeof result.body === 'string' ? result.body : JSON.stringify(result.body, null, 2));
		}
	}

	if (!result.ok) process.exit(1);
}

function unwrapData<T = unknown>(body: unknown): T {
	if (body && typeof body === 'object' && 'data' in body) {
		return (body as { data: T }).data;
	}
	return body as T;
}

async function runAsk(slug: string, question: string, flags: DispatchFlags) {
	if (!question) {
		console.error(`Usage: mm ${slug} ask "<question>"`);
		process.exit(1);
	}
	const result = await dispatch(slug, 'agent.chat', { question }, {
		validate: !flags.noValidate,
		instanceId: flags.instance
	});

	if (flags.json) {
		console.log(JSON.stringify(result.body, null, 2));
		if (!result.ok) process.exit(1);
		return;
	}

	if (!result.ok) {
		console.error(`HTTP ${result.status}`);
		console.error(typeof result.body === 'string' ? result.body : JSON.stringify(result.body, null, 2));
		process.exit(1);
	}

	const data = unwrapData<{
		markdown_snapshot?: string;
		intent?: string;
		writes?: Array<{ tool: string; resultId?: string }>;
	}>(result.body);

	if (data?.markdown_snapshot) {
		console.log(data.markdown_snapshot);
	} else {
		console.log(JSON.stringify(data, null, 2));
	}
	if (data?.writes?.length) {
		console.log('');
		console.log(`Writes (${data.writes.length}):`);
		for (const w of data.writes) {
			console.log(`  ${w.tool}${w.resultId ? `  → ${w.resultId}` : ''}`);
		}
	}
}

async function runFind(slug: string, query: string, flags: DispatchFlags & { limit?: number; types?: string[] }) {
	if (!query) {
		console.error(`Usage: mm ${slug} find "<query>"`);
		process.exit(1);
	}
	const payload: Record<string, unknown> = { query };
	if (flags.limit) payload.limit = flags.limit;
	if (flags.types?.length) payload.types = flags.types;

	const result = await dispatch(slug, 'agent.search', payload, {
		validate: !flags.noValidate,
		instanceId: flags.instance
	});

	if (flags.json) {
		console.log(JSON.stringify(result.body, null, 2));
		if (!result.ok) process.exit(1);
		return;
	}

	if (!result.ok) {
		console.error(`HTTP ${result.status}`);
		console.error(typeof result.body === 'string' ? result.body : JSON.stringify(result.body, null, 2));
		process.exit(1);
	}

	const data = unwrapData<{
		results?: Array<{ id: string; type?: string; title?: string; snippet?: string; url?: string }>;
		meta?: { total?: number };
	}>(result.body);

	const results = data?.results ?? [];
	if (results.length === 0) {
		console.log('(no results)');
		return;
	}
	for (const r of results) {
		const title = r.title ?? '(untitled)';
		const type = r.type ? ` [${r.type}]` : '';
		console.log(`  ${r.id.slice(0, 8)}${type}  ${title}`);
		if (r.snippet) console.log(`        ${r.snippet.slice(0, 140)}`);
	}
}

async function runDo(
	slug: string,
	card: AgentCard,
	toolName: string,
	args: string[],
	flags: DispatchFlags
) {
	const tool = findTool(card, toolName);
	if (!tool) {
		const known = (card.tools ?? []).map((t) => t.name).join(', ');
		console.error(`Unknown tool '${toolName}' on ${slug}.`);
		if (known) console.error(`Known tools: ${known}`);
		process.exit(1);
	}
	const payload = parseKeyValueArgs(args);
	// Tool names follow `<app>.<feature>.<verb>` — strip the app prefix
	// and pass the rest as feature.action to /api/v2.
	const stripped = tool.name.startsWith(`${slug}.`) ? tool.name.slice(slug.length + 1) : tool.name;
	await runDispatch(slug, stripped, payload, flags);
}

function isFeatureActionShape(token: string | undefined): boolean {
	return !!token && token.includes('.');
}

export async function appDispatch(slug: string, args: string[], flags: DispatchFlags) {
	// `mm <app>` with no args → render Card
	if (args.length === 0 || args[0] === 'help' || args[0] === '--help' || args[0] === '-h') {
		await cardsDispatch([slug], { json: flags.json, refresh: flags.refresh });
		return;
	}

	// Load Card once for capability/alias/tool lookups. Tolerate failure
	// so raw feature.action dispatch still works if the Card endpoint is
	// down.
	let card: AgentCard | null = null;
	try {
		card = await loadAgentCard(slug, { refresh: flags.refresh });
	} catch {
		// fall through
	}

	const verb = args[0];
	const rest = args.slice(1);

	// Card alias takes precedence over universal verbs (apps may override).
	if (card) {
		const alias = findAlias(card, verb);
		if (alias) {
			const payload = parseKeyValueArgs(rest);
			await runDispatch(slug, `${alias.feature}.${alias.action}`, payload, flags);
			return;
		}
	}

	switch (verb) {
		case 'ask':
		case 'chat': {
			const question = rest.join(' ').trim();
			await runAsk(slug, question, flags);
			return;
		}
		case 'find':
		case 'search': {
			if (card && !hasCapability(card, 'search')) {
				console.error(`${slug} does not advertise the 'search' capability.`);
				console.error(`Try: mm ${slug} ask "${rest.join(' ')}"`);
				process.exit(1);
			}
			const query = rest.filter((a) => !a.startsWith('--')).join(' ').trim();
			await runFind(slug, query, flags);
			return;
		}
		case 'do': {
			if (!card) {
				console.error(`Cannot resolve tools — Agent Card for ${slug} unreachable.`);
				process.exit(1);
			}
			const tool = rest[0];
			if (!tool) {
				console.error(`Usage: mm ${slug} do <tool-name> [key=value …]`);
				process.exit(1);
			}
			await runDo(slug, card, tool, rest.slice(1), flags);
			return;
		}
	}

	// Raw fallback: either `mm <app> <feature.action> [k=v…]` or
	// `mm <app> <feature> <action> [k=v…]`.
	let featureAction: string;
	let payloadArgs: string[];
	if (isFeatureActionShape(verb)) {
		featureAction = verb;
		payloadArgs = rest;
	} else if (rest.length > 0 && !rest[0].includes('=')) {
		featureAction = `${verb}.${rest[0]}`;
		payloadArgs = rest.slice(1);
	} else {
		console.error(`Usage: mm ${slug} <feature> <action> [key=value …]`);
		console.error(`Or:    mm ${slug} ask "..."   |   mm ${slug} find "..."`);
		console.error(`See:   mm cards ${slug}`);
		process.exit(1);
	}

	const payload = parseKeyValueArgs(payloadArgs);
	await runDispatch(slug, featureAction, payload, flags);
}
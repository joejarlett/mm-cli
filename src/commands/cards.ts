// ⚠️ LEGACY TYPESCRIPT PORT — NOT the live `mm` binary. The live CLI is Go;
// this command lives in internal/cmd/. Editing this file changes nothing in
// `mm` (it only builds the separate, unused `mm-ts`). Fix the .go file instead.

/**
 * `mm cards` — discovery surface across all registered apps.
 *
 *   mm cards                    # capability matrix across all apps
 *   mm cards <slug>             # full Card for one app
 *   mm cards <slug> --json      # raw JSON
 *   mm cards [<slug>] --refresh # bypass 24h cache
 *
 * The Card is each app's `/.well-known/agent.json` — name, description,
 * capabilities, curated tools (with destructive/readonly hints), and
 * optional aliases. This is the agent-facing view; for the full wire
 * surface use `mm manifest <app>`.
 */

import { listApps, resolveApp } from '../apps';
import { loadAgentCard, type AgentCard } from '../agent-card';

const CAPS = ['ask', 'chat', 'search', 'writes'] as const;

function capRow(card: AgentCard | null, slug: string, url: string): string {
	const caps = card?.capabilities ?? [];
	const cells = CAPS.map((c) => (caps.includes(c) ? c : '·'.padEnd(c.length))).join('  ');
	const status = card ? cells : 'unreachable';
	const desc = card?.description ? ` — ${card.description.slice(0, 60)}` : '';
	return `  ${slug.padEnd(10)} ${status.padEnd(28)} ${url}${desc}`;
}

export async function cardsDispatch(
	args: string[],
	flags: { json?: boolean; refresh?: boolean }
) {
	const slug = args[0];

	if (!slug) {
		const apps = listApps();
		const results: { slug: string; url: string; card: AgentCard | null }[] = [];
		await Promise.all(
			apps.map(async (app) => {
				try {
					const card = await loadAgentCard(app.slug, { refresh: flags.refresh });
					results.push({ slug: app.slug, url: app.url, card });
				} catch {
					results.push({ slug: app.slug, url: app.url, card: null });
				}
			})
		);
		results.sort((a, b) => a.slug.localeCompare(b.slug));

		if (flags.json) {
			console.log(JSON.stringify(results, null, 2));
			return;
		}

		console.log(`Apps (${results.length}) — capability matrix\n`);
		console.log(`  ${'slug'.padEnd(10)} ${CAPS.join('  ').padEnd(28)} url`);
		for (const r of results) {
			console.log(capRow(r.card, r.slug, r.url));
		}
		console.log('');
		console.log('Use: mm cards <slug>          full Card for one app');
		console.log('     mm <slug> ask "..."      ask the app a question (agent.chat)');
		console.log('     mm <slug> find "..."     search the app (agent.search)');
		return;
	}

	resolveApp(slug); // throws on unknown
	const card = await loadAgentCard(slug, { refresh: flags.refresh });

	if (flags.json) {
		console.log(JSON.stringify(card, null, 2));
		return;
	}

	console.log(`${card.name}${card.version ? ` (v${card.version})` : ''}`);
	if (card.description) console.log(`  ${card.description}`);
	console.log('');
	console.log(`  capabilities  ${(card.capabilities ?? []).join(', ') || '(none)'}`);
	if (card.chatUrl) console.log(`  chat          ${card.chatUrl}`);
	if (card.mcpUrl) console.log(`  mcp           ${card.mcpUrl}`);
	if (card.auth?.length) console.log(`  auth          ${card.auth.join(', ')}`);

	if (card.tools?.length) {
		console.log(`\nTools (${card.tools.length}):`);
		for (const t of card.tools) {
			const hints: string[] = [];
			if (t.readOnlyHint) hints.push('read-only');
			if (t.destructiveHint) hints.push('destructive');
			if (t.idempotentHint) hints.push('idempotent');
			const tag = hints.length ? `  [${hints.join(', ')}]` : '';
			console.log(`  ${t.name}${tag}`);
			if (t.description) console.log(`    ${t.description}`);
		}
	}

	if (card.aliases && Object.keys(card.aliases).length) {
		console.log(`\nAliases:`);
		for (const [verb, target] of Object.entries(card.aliases)) {
			const desc = target.description ? ` — ${target.description}` : '';
			console.log(`  mm ${slug} ${verb.padEnd(12)}  → ${target.feature}.${target.action}${desc}`);
		}
	}

	console.log('');
	console.log(`Wire surface: mm manifest ${slug}`);
}
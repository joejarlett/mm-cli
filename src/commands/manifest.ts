// ⚠️ LEGACY TYPESCRIPT PORT — NOT the live `mm` binary. The live CLI is Go;
// this command lives in internal/cmd/. Editing this file changes nothing in
// `mm` (it only builds the separate, unused `mm-ts`). Fix the .go file instead.

/**
 * `mm manifest <app>` — print an app's manifest, summary or full.
 *
 *   mm manifest kb              # one-line per feature, action count
 *   mm manifest kb --json       # raw manifest JSON
 *   mm manifest kb --refresh    # bypass 24h cache
 *   mm manifest                 # list all apps + their cached state
 */

import { loadManifest, actionCount } from '../manifest';
import { listApps, resolveApp } from '../apps';

export async function manifestDispatch(
	args: string[],
	flags: { json?: boolean; refresh?: boolean }
) {
	const slug = args[0];

	if (!slug) {
		// Show all apps with their cached state.
		console.log('Known apps:');
		for (const app of listApps()) {
			console.log(`  ${app.slug.padEnd(12)} ${app.url}  — ${app.description ?? ''}`);
		}
		console.log('');
		console.log('Use: mm manifest <slug>          to fetch + show one app');
		console.log('     mm manifest <slug> --json   for raw JSON');
		console.log('     mm manifest <slug> --refresh to bypass 24h cache');
		return;
	}

	resolveApp(slug); // throws if unknown
	const manifest = await loadManifest(slug, { refresh: flags.refresh });

	if (flags.json) {
		console.log(JSON.stringify(manifest, null, 2));
		return;
	}

	const total = actionCount(manifest);
	console.log(`${manifest.appSlug} (${manifest.version}) — ${total} actions across ${Object.keys(manifest.features).length} features`);
	console.log('');

	const features = Object.entries(manifest.features).sort(([a], [b]) => a.localeCompare(b));
	for (const [feature, actions] of features) {
		const actionEntries = Object.entries(actions).sort(([a], [b]) => a.localeCompare(b));
		console.log(`  ${feature}`);
		for (const [action, def] of actionEntries) {
			const desc = def.description ? `  — ${def.description}` : '';
			console.log(`    ${feature}.${action.padEnd(30)} [${def.auth}]${desc}`);
		}
	}
	console.log('');
	console.log(`Use: mm v2 ${slug} <feature.action> [json-payload]   to invoke`);
}
/**
 * `mm v2 <app> <feature.action> [json-payload]` — generic dispatch.
 *
 * Examples:
 *   mm v2 kb collections.list
 *   mm v2 kb documents.search '{"q":"meta"}'
 *   mm v2 finances agent.chat '{"question":"what was my last txn?"}' --instance <uuid>
 *
 * Manifest pre-validation catches typos (e.g. `documens` → suggestion).
 * Pass `--no-validate` to skip the manifest check (faster but less
 * helpful errors).
 */

import { dispatch } from '../dispatcher';

export async function v2Dispatch(
	args: string[],
	flags: { json?: boolean; instance?: string; noValidate?: boolean }
) {
	const [slug, featureAction, payloadStr] = args;

	if (slug === 'help' || slug === '--help' || slug === '-h' || !slug || !featureAction) {
		console.log(`mm v2 <app> <feature.action> [json-payload] — generic dispatch

Posts {feature, action, payload} to <app>/api/v2 with your bearer
token. Pre-validates against the app's manifest unless --no-validate.

Examples:
  mm v2 kb collections.list
  mm v2 kb documents.search '{"q":"meta"}'
  mm v2 finances agent.chat '{"question":"hi"}' --instance abc-123

Tips:
  - Use \`mm manifest <app>\` to see the available feature.actions.
  - For ergonomic aliases prefer the per-app commands: \`mm kb …\`,
    \`mm crm …\`. The v2 path is for everything those don't cover.`);
		if (!slug || !featureAction) process.exit(1);
		return;
	}

	let payload: Record<string, unknown> = {};
	if (payloadStr) {
		try {
			payload = JSON.parse(payloadStr);
		} catch (err) {
			console.error(`Failed to parse payload as JSON: ${err instanceof Error ? err.message : err}`);
			console.error(`Got: ${payloadStr}`);
			process.exit(1);
		}
	}

	let result;
	try {
		result = await dispatch(slug, featureAction, payload, {
			validate: !flags.noValidate,
			instanceId: flags.instance
		});
	} catch (err) {
		console.error(err instanceof Error ? err.message : String(err));
		process.exit(1);
	}

	if (flags.json) {
		console.log(JSON.stringify(result.body, null, 2));
	} else {
		// Pretty: HTTP status + body. Errors envelope (`{errors:[...]}`)
		// gets a friendlier render than blob JSON.
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
			if (!result.ok) {
				console.error(`HTTP ${result.status}`);
			}
			console.log(typeof result.body === 'string' ? result.body : JSON.stringify(result.body, null, 2));
		}
	}

	if (!result.ok) process.exit(1);
}

/**
 * mm email — admin commands for the platform email log.
 *
 * Hub-side feature (lives on meta-me.uk/api/mm, not per-app), so the
 * dispatch URL differs from `mm kb` / `mm crm`. Admin-only — non-admin
 * callers get 403.
 */

import { loadAuth } from '../auth';

const HUB_URL = 'https://meta-me.uk';

async function hubApi(feature: string, action: string, payload?: Record<string, unknown>) {
	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const res = await fetch(`${HUB_URL}/api/mm`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${auth.token}`,
			'X-Hub-User-Id': auth.userId,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ feature, action, payload: payload ?? {} })
	});

	const text = await res.text();
	let parsed: { data?: unknown; errors?: Array<{ code: string; title?: string; detail?: string }> };
	try {
		parsed = JSON.parse(text);
	} catch {
		throw new Error(`Hub API non-JSON response (${res.status}): ${text.slice(0, 200)}`);
	}

	if (!res.ok || 'errors' in parsed) {
		const first = parsed.errors?.[0];
		const msg = first?.detail || first?.title || `${feature}.${action} failed (${res.status})`;
		throw new Error(msg);
	}
	return (parsed as { data: unknown }).data;
}

type EmailListRow = {
	id: string;
	userId: string | null;
	toAddress: string;
	subject: string;
	template: string | null;
	status: string;
	triggeredBy: string | null;
	createdAt: string;
	sentAt: string | null;
	failedAt: string | null;
	error: string | null;
	parentId: string | null;
};

type EmailListResponse = { rows: EmailListRow[]; nextCursor: string | null };

type EmailDetail = EmailListRow & {
	fromAddress: string;
	bodyHtml: string;
	bodyText: string;
	templateParams: Record<string, unknown> | null;
	messageId: string | null;
};

export async function emailDispatch(
	command: string,
	args: string[],
	flags: { json?: boolean }
) {
	const json = flags?.json || false;
	switch (command) {
		case 'list':
		case 'ls':
			return emailList(args, json);
		case 'get':
		case 'show':
			return emailGet(args[0] || '', json);
		case 'resend':
			return emailResend(args[0] || '', json);
		case 'send':
			return emailSend(args, json);
		case 'draft':
			return emailSend(args, json, { draftOnly: true });
		default:
			console.error(`Unknown command: mm email ${command}`);
			console.error('Run `mm --help` for available commands.');
			process.exit(1);
	}
}

function parseListFlags(args: string[]): {
	status?: string;
	template?: string;
	q?: string;
	limit?: number;
} {
	const out: Record<string, string | number> = {};
	for (const a of args) {
		const m = a.match(/^--(status|template|q|limit)=(.+)$/);
		if (!m) continue;
		out[m[1]] = m[1] === 'limit' ? Number(m[2]) : m[2];
	}
	return out as { status?: string; template?: string; q?: string; limit?: number };
}

async function emailList(args: string[], json: boolean) {
	const payload = parseListFlags(args);
	const data = (await hubApi('email', 'list', payload)) as EmailListResponse;
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	const rows = data?.rows ?? [];
	if (rows.length === 0) {
		console.log('No emails match.');
		return;
	}
	const statusPad = 6;
	const templatePad = 22;
	const toPad = 28;
	for (const r of rows) {
		const status = r.status.padEnd(statusPad);
		const template = (r.template ?? '—').padEnd(templatePad).slice(0, templatePad);
		const to = r.toAddress.padEnd(toPad).slice(0, toPad);
		const when = fmtRelative(r.createdAt);
		console.log(`  ${r.id.slice(0, 8)}  ${status}  ${template}  ${to}  ${when}  ${r.subject}`);
	}
	if (data.nextCursor) {
		console.log('');
		console.log(`  …more. Resume with --cursor='${data.nextCursor}' (not yet exposed in CLI; use the web UI to page deeper).`);
	}
}

async function emailGet(id: string, json: boolean) {
	if (!id) {
		console.error('Usage: mm email get <id>');
		process.exit(1);
	}
	const data = (await hubApi('email', 'get', { id })) as EmailDetail;
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log(`ID:        ${data.id}`);
	console.log(`Status:    ${data.status}`);
	console.log(`Template:  ${data.template ?? '—'}`);
	console.log(`Trigger:   ${data.triggeredBy ?? '—'}`);
	console.log(`To:        ${data.toAddress}`);
	console.log(`From:      ${data.fromAddress}`);
	console.log(`Subject:   ${data.subject}`);
	console.log(`Created:   ${data.createdAt}`);
	if (data.sentAt) console.log(`Sent:      ${data.sentAt}`);
	if (data.failedAt) console.log(`Failed:    ${data.failedAt}`);
	if (data.messageId) console.log(`Message:   ${data.messageId}`);
	if (data.parentId) console.log(`Resend of: ${data.parentId}`);
	if (data.userId) console.log(`User:      ${data.userId}`);
	if (data.templateParams) {
		console.log(`Params:    ${JSON.stringify(data.templateParams)}`);
	}
	if (data.error) {
		console.log('');
		console.log('Error:');
		console.log('  ' + data.error.split('\n').join('\n  '));
	}
	console.log('');
	console.log('--- Text body ---');
	console.log(data.bodyText);
}

function parseSendFlags(args: string[]): {
	to?: string;
	subject?: string;
	body?: string;
	text?: string;
	template?: string;
} {
	const out: Record<string, string> = {};
	// Support both `--key=value` and `--key value` forms.
	for (let i = 0; i < args.length; i++) {
		const a = args[i];
		const eq = a.match(/^--(to|subject|body|text|template)=(.+)$/s);
		if (eq) {
			out[eq[1]] = eq[2];
			continue;
		}
		const flag = a.match(/^--(to|subject|body|text|template)$/);
		if (flag && i + 1 < args.length) {
			out[flag[1]] = args[++i];
		}
	}
	return out as {
		to?: string;
		subject?: string;
		body?: string;
		text?: string;
		template?: string;
	};
}

function stripHtml(s: string): string {
	return s
		.replace(/<style[^>]*>[\s\S]*?<\/style>/gi, '')
		.replace(/<script[^>]*>[\s\S]*?<\/script>/gi, '')
		.replace(/<[^>]+>/g, '')
		.replace(/&nbsp;/g, ' ')
		.replace(/&amp;/g, '&')
		.replace(/&lt;/g, '<')
		.replace(/&gt;/g, '>')
		.replace(/&quot;/g, '"')
		.replace(/\s+/g, ' ')
		.trim();
}

/**
 * `mm email send` and `mm email draft`.
 *
 * Drives the same hub RPC the admin compose page uses:
 *   1. `email.create`      → writes a draft row to `mm.public.email`
 *   2. `email.send`        → flips to queued → sent | failed
 *      (when `GWS_GATEWAY_URL` is configured on the hub, this routes
 *      through google-workspace-gateway → Gmail API; otherwise SMTP.)
 *
 * `mm email draft` does step 1 only. Useful to preview-via-`mm email get`
 * before firing.
 */
async function emailSend(args: string[], json: boolean, opts: { draftOnly?: boolean } = {}) {
	const flags = parseSendFlags(args);
	if (!flags.to || !flags.subject || !flags.body) {
		console.error(
			'Usage: mm email send --to <addr> --subject <s> --body <html> [--text <plain>] [--template <name>]'
		);
		console.error('       mm email draft …same args…    (creates a draft, does not send)');
		process.exit(1);
	}

	const created = (await hubApi('email', 'create', {
		to: flags.to,
		subject: flags.subject,
		html: flags.body,
		text: flags.text ?? stripHtml(flags.body),
		template: flags.template
	})) as { id: string };

	if (opts.draftOnly) {
		if (json) {
			console.log(JSON.stringify({ id: created.id, status: 'draft' }, null, 2));
			return;
		}
		console.log(`✓ Draft saved: ${created.id}`);
		console.log(`  Preview: mm email get ${created.id}`);
		console.log(`  Send:    mm email send <re-run> or via /admin/emails/${created.id}`);
		return;
	}

	const sent = (await hubApi('email', 'send', { id: created.id })) as {
		success: boolean;
		error?: string;
	};

	if (json) {
		console.log(JSON.stringify({ id: created.id, ...sent }, null, 2));
		return;
	}
	if (sent.success) {
		console.log(`✓ Sent. Row: ${created.id}`);
		console.log(`  Detail: mm email get ${created.id}`);
	} else {
		console.log(`✗ Created row ${created.id} but send failed:`);
		console.log(`  ${sent.error ?? 'unknown'}`);
		process.exit(1);
	}
}

async function emailResend(id: string, json: boolean) {
	if (!id) {
		console.error('Usage: mm email resend <id>');
		process.exit(1);
	}
	const data = (await hubApi('email', 'resend', { id })) as {
		newId: string;
		success: boolean;
		error?: string;
	};
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	if (data.success) {
		console.log(`✓ Resent. New row: ${data.newId}`);
	} else {
		console.log(`✗ Resend row ${data.newId} created but SMTP failed:`);
		console.log(`  ${data.error ?? 'unknown'}`);
	}
}

function fmtRelative(iso: string): string {
	const t = Date.parse(iso);
	if (Number.isNaN(t)) return iso;
	const diff = Date.now() - t;
	if (diff < 60_000) return 'just now';
	if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`;
	if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`;
	if (diff < 7 * 86_400_000) return `${Math.round(diff / 86_400_000)}d ago`;
	return new Date(t).toISOString().slice(0, 10);
}

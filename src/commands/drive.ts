/**
 * mm drive — light Drive surface, mainly for the doc-from-markdown
 * flow that turns a local `.md` into a native Google Doc.
 *
 *   mm drive ls [--q "name contains 'invoice'"] [--max 25]
 *
 *   mm drive doc <name> --file path/to/notes.md
 *   mm drive doc <name> < notes.md               # stdin works too
 *   mm drive doc <name> --text "Plain string body" --mime text/plain
 *
 * `<name>` is what the Doc will be called in Drive. The content is
 * converted by Drive on import — markdown becomes formatted Doc
 * (headings, lists, code blocks), text/html becomes the same, plain
 * text becomes plain text.
 */
import { readFileSync } from 'node:fs';
import { hubApi } from '../hub';

type DriveFile = {
	id: string;
	name: string;
	mimeType: string;
	modifiedTime?: string;
	webViewLink?: string;
};

type ListResp = { files: DriveFile[]; accountSlug: string | null };
type CreateDocResp = { id: string; name: string; webViewLink: string | null };

export async function driveDispatch(
	command: string,
	args: string[],
	flags: { json?: boolean }
) {
	const json = flags?.json || false;
	switch (command) {
		case 'ls':
		case 'list':
			return driveList(args, json);
		case 'doc':
			return driveDoc(args, json);
		default:
			console.error(`Unknown command: mm drive ${command}`);
			console.error('Run `mm --help` for available commands.');
			process.exit(1);
	}
}

function parseFlags(args: string[]): { flags: Record<string, string>; positional: string[] } {
	const flags: Record<string, string> = {};
	const positional: string[] = [];
	for (let i = 0; i < args.length; i++) {
		const a = args[i];
		const eq = a.match(/^--([a-z][a-z-]*)=(.+)$/s);
		if (eq) {
			flags[eq[1]] = eq[2];
			continue;
		}
		const longFlag = a.match(/^--([a-z][a-z-]*)$/);
		if (longFlag) {
			const next = args[i + 1];
			if (next === undefined || next.startsWith('--')) {
				flags[longFlag[1]] = 'true';
			} else {
				flags[longFlag[1]] = next;
				i++;
			}
			continue;
		}
		positional.push(a);
	}
	return { flags, positional };
}

function fmtWhen(iso: string | undefined): string {
	if (!iso) return '—';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return iso;
	const diff = Date.now() - d.getTime();
	if (diff < 60_000) return 'just now';
	if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m`;
	if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h`;
	if (diff < 7 * 86_400_000) return `${Math.round(diff / 86_400_000)}d`;
	return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
}

function mimeLabel(m: string): string {
	if (m === 'application/vnd.google-apps.document') return 'doc';
	if (m === 'application/vnd.google-apps.spreadsheet') return 'sheet';
	if (m === 'application/vnd.google-apps.presentation') return 'slide';
	if (m === 'application/vnd.google-apps.folder') return 'folder';
	if (m.startsWith('image/')) return 'img';
	if (m === 'application/pdf') return 'pdf';
	return m.split('/').pop() ?? m;
}

async function driveList(args: string[], json: boolean) {
	const { flags } = parseFlags(args);
	const payload: Record<string, unknown> = {};
	if (flags.q) payload.q = flags.q;
	if (flags.max) payload.max = Number(flags.max);
	if (flags.account) payload.accountSlug = flags.account;

	const data = await hubApi<ListResp>('drive', 'list', payload);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	if (data.files.length === 0) {
		console.log('No files match.');
		return;
	}
	for (const f of data.files) {
		const when = fmtWhen(f.modifiedTime).padStart(4);
		const kind = mimeLabel(f.mimeType).padEnd(6).slice(0, 6);
		console.log(`  ${when}  ${kind}  ${f.name}`);
		console.log(`        ${f.webViewLink ?? '(no link)'}`);
	}
}

async function readStdin(): Promise<string> {
	// Bun + Node compatible.
	const chunks: Buffer[] = [];
	for await (const chunk of process.stdin as unknown as AsyncIterable<Buffer | string>) {
		chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
	}
	return Buffer.concat(chunks).toString('utf-8');
}

async function driveDoc(args: string[], json: boolean) {
	const { flags, positional } = parseFlags(args);
	const name = positional.join(' ').trim();
	if (!name) {
		console.error('Usage: mm drive doc <name> --file path.md  (or pipe in via stdin)');
		console.error('       mm drive doc <name> --text "literal body" [--mime text/plain|text/markdown|text/html]');
		process.exit(1);
	}

	// Pick the content source: --file > --text > stdin.
	let content: string;
	const sourceMime =
		(flags.mime as 'text/markdown' | 'text/plain' | 'text/html' | undefined) ?? 'text/markdown';
	if (flags.file) {
		try {
			content = readFileSync(flags.file, 'utf-8');
		} catch (err) {
			console.error(`Couldn't read --file ${flags.file}: ${(err as Error).message}`);
			process.exit(1);
		}
	} else if (flags.text) {
		content = flags.text;
	} else if (!process.stdin.isTTY) {
		content = await readStdin();
	} else {
		console.error('No content. Pass --file PATH, --text "...", or pipe markdown via stdin.');
		process.exit(1);
	}

	const data = await hubApi<CreateDocResp>('drive', 'createDoc', {
		name,
		content,
		sourceMime,
		...(flags.account ? { accountSlug: flags.account } : {})
	});
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log(`✓ Created Google Doc: ${data.name}`);
	if (data.webViewLink) console.log(`  ${data.webViewLink}`);
}

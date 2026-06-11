// ⚠️ LEGACY TYPESCRIPT PORT — NOT the live `mm` binary. The live CLI is Go;
// this command lives in internal/cmd/. Editing this file changes nothing in
// `mm` (it only builds the separate, unused `mm-ts`). Fix the .go file instead.

/**
 * mm drive — light Drive surface: list, doc-from-markdown, rename/move.
 *
 *   mm drive ls [--q "name contains 'invoice'"] [--max 25]
 *
 *   mm drive doc <name> --file path/to/notes.md
 *   mm drive doc <name> < notes.md               # stdin works too
 *   mm drive doc <name> --text "Plain string body" --mime text/plain
 *
 *   mm drive mv <id> --name "New name"
 *   mm drive mv <id> --parent <folder-id> [--unparent <old-folder-id>]
 *
 * `<name>` is what the Doc will be called in Drive. The content is
 * converted by Drive on import — markdown becomes formatted Doc
 * (headings, lists, code blocks), text/html becomes the same, plain
 * text becomes plain text.
 */
import { readFileSync } from 'node:fs';
import { hubApi } from '../hub';
import type {
	HubDriveListResp,
	HubDriveCreateDocResp,
	HubDriveUpdateResp,
} from '../wire';

export async function driveDispatch(
	command: string,
	args: string[],
	flags: { json?: boolean }
) {
	const json = flags?.json || false;
	switch (command) {
		case '':
		case 'help':
		case '--help':
		case '-h':
			printDriveHelp();
			return;
		case 'ls':
		case 'list':
			return driveList(args, json);
		case 'doc':
			return driveDoc(args, json);
		case 'rename':
		case 'mv':
		case 'move':
			return driveMove(args, json);
		default:
			console.error(`Unknown command: mm drive ${command}`);
			console.error('Try `mm drive help`.');
			process.exit(1);
	}
}

export function printDriveHelp() {
	console.log(`mm drive — Google Drive

Subcommands:
  ls | list                List files (filter with --q)
  doc <file.md>            Create a Google Doc from a markdown file
  mv | rename | move <id>  Rename and/or move a file

Examples:
  mm drive mv <id> --name "New name"
  mm drive mv <id> --parent <folder-id>
  mm drive mv <id> --parent <new> --unparent <old>      # move
  mm drive mv <id> --name "X" --parent <new> --unparent <old>

Flags:
  --q "<query>"            Search query (Drive search syntax)
  --max <n>                Max results (default 20)
  --name "<name>"          New name for mv
  --parent <folder-id>     Add a parent (move into)
  --unparent <folder-id>   Remove a parent (move out of)
  --folder <id>            Target folder for doc creation
  --account <slug|email>   Pick a linked Google account
  --json                   Parseable output`);
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

	const data = await hubApi<HubDriveListResp>('drive', 'list', payload);
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

async function driveMove(args: string[], json: boolean) {
	const { flags, positional } = parseFlags(args);
	const fileId = positional[0];
	if (!fileId) {
		console.error('Usage: mm drive mv <id> [--name "X"] [--parent <folder-id>] [--unparent <folder-id>]');
		process.exit(1);
	}
	const payload: Record<string, unknown> = { fileId };
	if (flags.name) payload.name = flags.name;
	if (flags.parent) payload.addParents = [flags.parent];
	if (flags.unparent) payload.removeParents = [flags.unparent];
	if (flags.account) payload.accountSlug = flags.account;
	if (!payload.name && !payload.addParents && !payload.removeParents) {
		console.error('Nothing to do — pass at least one of --name, --parent, --unparent.');
		process.exit(1);
	}

	const data = await hubApi<HubDriveUpdateResp>('drive', 'update', payload);
	if (json) {
		console.log(JSON.stringify(data, null, 2));
		return;
	}
	console.log(`✓ ${data.name}`);
	if (data.webViewLink) console.log(`  ${data.webViewLink}`);
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

	const data = await hubApi<HubDriveCreateDocResp>('drive', 'createDoc', {
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
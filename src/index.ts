#!/usr/bin/env bun
/**
 * mm — Meta-Me CLI
 *
 * Usage:
 *   mm login                    Authenticate via browser
 *   mm logout                   Clear stored credentials
 *   mm whoami                   Show current user
 *   mm status                   Show auth status and available apps
 *   mm kb <command> [...]       Knowledge Base commands
 *   mm crm <command> [...]      CRM commands
 */

import { login, logout, whoami } from './commands/login';
import { status } from './commands/status';
import { kbDispatch } from './commands/kb';
import { crmDispatch } from './commands/crm';
import { emailDispatch } from './commands/email';
import { calendarDispatch } from './commands/calendar';
import { tasksDispatch } from './commands/tasks';
import { driveDispatch } from './commands/drive';
import { sttDispatch } from './commands/stt';
import { ttsDispatch } from './commands/tts';
import { v2Dispatch } from './commands/v2';
import { manifestDispatch } from './commands/manifest';
import { cardsDispatch } from './commands/cards';
import { appDispatch } from './commands/app';
import { APPS } from './apps';
import { chatDispatch } from './commands/chat';
import { projectDispatch } from './commands/project';
import {
	sqlDispatch,
	appsDispatch,
	appDispatch as adminAppDispatch,
	healthDispatch,
	errorsDispatch,
	errorDispatch,
} from './commands/hub';
import { shutdown as shutdownDb } from './db';

const VERSION = '0.1.0';

function printHelp() {
	console.log(`mm v${VERSION} — Meta-Me CLI

Usage:
  mm login                     Authenticate via browser
  mm logout                    Clear stored credentials
  mm whoami                    Show current user
  mm status                    Show auth status and available apps

  mm kb <command> [args...]    Knowledge Base commands
  mm crm <command> [args...]   CRM commands
  mm chat <command> [args...]  Local agent threads (list/show/search)
  mm project <command> [args...] Local agent project index (overview/detail/add)
  mm email <command> [args...] Platform email log (admin only)
  mm calendar [list|new] [args] Google Calendar — agenda + quick create
  mm tasks [list|add|done] [args] Google Tasks — list, add, complete
  mm drive [ls|doc|mv] [args]   Google Drive — list, doc-from-markdown, rename/move
  mm stt <file>                Transcribe audio (wav/mp3/m4a/…)
  mm tts "<text>" [--out f] [--play] [--voice id] [--format wav|mp3]
                                Synthesise speech

  mm cards [<app>] [--refresh] Capability matrix / per-app Agent Card
  mm manifest [<app>]          Wire-level manifest (deeper than the Card)
  mm <app> ask "..."           Ask any app a question (agent.chat)
  mm <app> find "..."          Search an app (agent.search, where supported)
  mm <app> do <tool> [k=v…]    Invoke a Card-declared tool
  mm <app> <feature> <action>  Raw dispatch to <app>/api/v2

  mm v2 <app> <feature.action> [json] [--instance <uuid>]
                                (deprecated alias for the raw dispatch
                                form above — prefer mm <app> <f> <a>)

KB Commands:
  mm kb find <query>           Search documents
  mm kb tree [notebook]        List notebooks
  mm kb peek <id>              Preview a document
  mm kb read <id>              Read full document
  mm kb collections            List collections

Chat Commands (local agent threads):
  mm chat                       List recent threads
  mm chat list [--limit N] [--project <id>]
  mm chat show <id> [--limit N]
                                Print messages in a thread
  mm chat search <query>        Substring search across messages
  mm chat projects              List known projects + thread counts

Project Commands (local agent project index):
  mm project list               List registered projects
  mm project overview <name|path> [subpath]
                                Folder-level summaries with drift counts
                                (preferred first move for shape questions)
  mm project detail <name|path> [subpath] [--search q] [--limit n] [--shallow]
                                Per-file summaries under a folder
  mm project add <path> [label] Register a folder as a project
  mm project rebuild <name|path> [subpath]
                                Drop cached rows and re-summarise

CRM Commands:
  mm crm surface                Today's priorities
  mm crm contacts               List contacts
  mm crm contacts find <q>      Search contacts
  mm crm projects               List projects
  mm crm log "<text>"           Log an interaction
  mm crm context <person>       Person context
  mm crm peek <id>              Preview anything
  mm crm read <id>              Full content
  mm crm find <query>           Search (shorthand)

Calendar Commands:
  mm calendar                   Agenda for the next 7 days
  mm calendar list [--days N] [--q "search"]
                                List events in a date range
  mm calendar new --title "X" --when "2026-05-20 14:00" [--end "15:00"]
       [--at "place"] [--invite a@x.com,b@y.com] [--describe "..."]

Tasks Commands:
  mm tasks                      Pending tasks due in next 7 days
  mm tasks list [--days N] [--all]
  mm tasks add "<title>" [--due YYYY-MM-DD] [--list "List name"] [--notes "..."]
  mm tasks done <task-id> --list-id <list-id>
                                (run "mm tasks" to see ids)

Drive Commands:
  mm drive ls [--q "name contains 'invoice'"] [--max N]
  mm drive doc <name> --file path.md
                                Create a native Google Doc converted
                                from local markdown (also accepts stdin).
  mm drive mv <id> [--name "<new name>"]
                   [--parent <folder-id>] [--unparent <folder-id>]
                                Rename and/or move a file. Combine any
                                of --name, --parent, --unparent.

Email Commands (admin only):
  mm email list [--status=…] [--template=…] [--q=…]
                                List outbound emails, newest first
  mm email get <id>             Show full detail (incl. body text)
  mm email send --to <addr> --subject <s> --body <html> [--text <plain>] [--template <name>]
                                Compose + send through the hub. Routes
                                via gws-gateway when GWS_GATEWAY_URL is
                                set server-side; otherwise SMTP.
  mm email draft <same args>    Create a draft without sending.
  mm email resend <id>          Resend; creates a new row referencing
                                the original via parent_id

Flags:
  --json                       Output as JSON
  --help, -h                   Show this help
  --version, -v                Show version

Setup: https://meta-me.uk/settings/api
`);
}

async function main() {
	const args = process.argv.slice(2);

	// Global flags
	const flags = {
		help: args.includes('--help') || args.includes('-h'),
		version: args.includes('--version') || args.includes('-v'),
		json: args.includes('--json')
	};

	// Strip only the known global flags from positional args. Per-command
	// flags (e.g. `mm email list --status=sent`) need to pass through to
	// the command's own parser. Previous behaviour was to strip every
	// `--*` arg, which swallowed filter flags silently.
	const GLOBAL_FLAGS = new Set(['--json', '--help', '-h', '--version', '-v', '--refresh', '--no-validate']);
	const positional = args.filter((a, i) => {
		if (GLOBAL_FLAGS.has(a)) return false;
		// Drop --instance plus its value (next arg).
		if (a === '--instance') return false;
		if (i > 0 && args[i - 1] === '--instance') return false;
		return true;
	});

	function getFlagValue(allArgs: string[], flag: string): string | undefined {
		const i = allArgs.indexOf(flag);
		return i >= 0 && i + 1 < allArgs.length ? allArgs[i + 1] : undefined;
	}

	if (flags.version) {
		console.log(`mm v${VERSION}`);
		process.exit(0);
	}

	if (flags.help || positional.length === 0) {
		printHelp();
		process.exit(0);
	}

	const command = positional[0]?.toLowerCase();

	try {
		switch (command) {
			case 'login':
				await login(positional[1]);
				break;
			case 'logout':
				await logout();
				break;
			case 'whoami':
				await whoami();
				break;
			case 'status':
				await status();
				break;
			case 'kb':
				await kbDispatch(positional[1] || '', positional.slice(2), flags);
				break;
			case 'crm':
				await crmDispatch(positional[1] || '', positional.slice(2), flags);
				break;
			case 'chat':
				await chatDispatch(positional[1] || '', positional.slice(2), flags);
				break;
			case 'project':
			case 'projects':
				await projectDispatch(positional[1] || '', positional.slice(2), flags);
				break;
			case 'email':
				await emailDispatch(positional[1] || '', positional.slice(2), flags);
				break;
			case 'calendar':
				await calendarDispatch(positional[1] || '', positional.slice(2), flags);
				break;
			case 'tasks':
				await tasksDispatch(positional[1] || '', positional.slice(2), flags);
				break;
			case 'drive':
				await driveDispatch(positional[1] || '', positional.slice(2), flags);
				break;
			case 'stt':
				await sttDispatch(positional.slice(1), flags);
				break;
			case 'tts':
				await ttsDispatch(positional.slice(1));
				break;
			case 'v2':
				// Generic dispatcher: mm v2 <app> <feature.action> [json] [--instance <uuid>] [--no-validate]
				await v2Dispatch(positional.slice(1), {
					json: flags.json,
					instance: getFlagValue(args, '--instance'),
					noValidate: args.includes('--no-validate')
				});
				break;
			case 'manifest':
				// mm manifest [<app>] [--refresh]
				await manifestDispatch(positional.slice(1), {
					json: flags.json,
					refresh: args.includes('--refresh')
				});
				break;
			case 'cards':
			case 'card':
				// mm cards [<app>] [--refresh]
				await cardsDispatch(positional.slice(1), {
					json: flags.json,
					refresh: args.includes('--refresh')
				});
				break;
			// ─── Hub-admin (ported from old mm) ───
			case 'sql':
				await sqlDispatch(positional.slice(1), { json: flags.json });
				break;
			case 'apps':
				await appsDispatch(positional.slice(1), { json: flags.json });
				break;
			case 'app':
				await adminAppDispatch(positional.slice(1), { json: flags.json });
				break;
			case 'health':
				await healthDispatch(positional.slice(1), { json: flags.json });
				break;
			case 'errors':
				await errorsDispatch(positional.slice(1), { ...flags });
				break;
			case 'error':
				await errorDispatch(positional.slice(1), { ...flags });
				break;
			default:
				// Generic app dispatch: if `command` is a registered app slug
				// that isn't otherwise handled above (kb/crm have explicit
				// cases), route through the universal-verb dispatcher.
				if (command && command in APPS) {
					await appDispatch(command, positional.slice(1), {
						json: flags.json,
						instance: getFlagValue(args, '--instance'),
						noValidate: args.includes('--no-validate'),
						refresh: args.includes('--refresh')
					});
					break;
				}
				console.error(`Unknown command: ${command}`);
				console.error('Run `mm --help` for available commands.');
				process.exit(1);
		}
	} catch (err: any) {
		console.error(`❌ ${err.message}`);
		process.exit(1);
	} finally {
		// Close any DB connection opened by hub commands so the process
		// exits cleanly. No-op if `db()` was never called.
		await shutdownDb();
	}
}

main();

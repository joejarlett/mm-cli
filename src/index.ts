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
  mm email <command> [args...] Platform email log (admin only)
  mm calendar [list|new] [args] Google Calendar — agenda + quick create
  mm tasks [list|add|done] [args] Google Tasks — list, add, complete
  mm drive [ls|doc] [args]      Google Drive — list + doc-from-markdown
  mm stt <file>                Transcribe audio (wav/mp3/m4a/…)
  mm tts "<text>" [--out f] [--play] [--voice id] [--format wav|mp3]
                                Synthesise speech

  mm manifest [<app>]          List apps / show one app's full surface
  mm v2 <app> <feature.action> [json] [--instance <uuid>]
                                Generic dispatcher — call any app's
                                /api/v2 endpoint with any action.

KB Commands:
  mm kb find <query>           Search documents
  mm kb tree [notebook]        List notebooks
  mm kb peek <id>              Preview a document
  mm kb read <id>              Read full document
  mm kb collections            List collections

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
			default:
				console.error(`Unknown command: ${command}`);
				console.error('Run `mm --help` for available commands.');
				process.exit(1);
		}
	} catch (err: any) {
		console.error(`❌ ${err.message}`);
		process.exit(1);
	}
}

main();

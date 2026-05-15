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

Email Commands (admin only):
  mm email list [--status=…] [--template=…] [--q=…]
                                List outbound emails, newest first
  mm email get <id>             Show full detail (incl. body text)
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

	// Filter out flags from positional args
	const positional = args.filter((a) => !a.startsWith('--') && a !== '-h' && a !== '-v');

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

/**
 * mm login — Authenticate via device flow.
 *
 * 1. Initiates device flow with auth.meta-me.uk
 * 2. Opens browser to the verification URL
 * 3. Polls until user authorizes (or timeout)
 * 4. Validates the token and saves it to ~/.config/mm/auth.json
 */

import { execSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { deviceInitiate, devicePoll, PendingError, validateToken } from '../api';
import { saveAuth, loadAuth, getConfigDir } from '../auth';

function openBrowser(url: string) {
	const platform = process.platform;
	try {
		if (platform === 'darwin') {
			execSync(`open "${url}"`);
		} else if (platform === 'linux') {
			execSync(`xdg-open "${url}"`);
		} else if (platform === 'win32') {
			execSync(`start "" "${url}"`);
		}
	} catch {
		// Non-fatal — user can manually open the URL
	}
}

export async function login(name?: string) {
	const existing = loadAuth();
	if (existing) {
		console.error(`Already authenticated as ${existing.userName} (${existing.userEmail})`);
		console.error('Run `mm logout` first to re-authenticate.');
		process.exit(1);
	}

	console.log('Starting device authentication...\n');

	let init: Awaited<ReturnType<typeof deviceInitiate>>;
	try {
		init = await deviceInitiate();
	} catch (err: any) {
		console.error(`❌ ${err.message}`);
		process.exit(1);
	}

	console.log(`1. Opening browser: ${init.verificationUri}`);
	console.log(`2. Enter code: ${init.userCode}\n`);

	openBrowser(init.verificationUri);

	const pollInterval = init.interval * 1000;
	const startTime = Date.now();
	const timeout = (init.expiresIn + 10) * 1000;

	process.stdout.write('Waiting for authorization');
	const dots = ['.', '..', '...'];
	let dotIdx = 0;

	while (Date.now() - startTime < timeout) {
		try {
			const result = await devicePoll(init.deviceCode, name);
			process.stdout.write('\n');

			// Validate the token to get user info
			const validated = await validateToken(result.accessToken);
			if (!validated) {
				console.error('❌ Token validation failed.');
				process.exit(1);
			}

			saveAuth({
				token: result.accessToken,
				prefix: result.key.prefix,
				userId: validated.user.id,
				userName: validated.user.name,
				userEmail: validated.user.email,
				createdAt: new Date().toISOString()
			});

			console.log(`✅ Authenticated as ${validated.user.name} (${validated.user.email})`);
			return;
		} catch (err) {
			if (err instanceof PendingError) {
				process.stdout.write(`\rWaiting for authorization${dots[dotIdx]}`);
				dotIdx = (dotIdx + 1) % dots.length;
				await new Promise((r) => setTimeout(r, pollInterval));
				continue;
			}
			process.stdout.write('\n');
			console.error(`❌ ${(err as Error).message}`);
			process.exit(1);
		}
	}

	process.stdout.write('\n');
	console.error('❌ Timed out waiting for authorization. Run `mm login` again.');
	process.exit(1);
}

export async function logout() {
	const auth = loadAuth();
	if (!auth) {
		console.error('Not authenticated.');
		process.exit(1);
	}
	writeFileSync(join(getConfigDir(), 'auth.json'), '{"loggedOut": true}', { mode: 0o600 });
	console.log(`Logged out. (Was ${auth.userName})`);
}

export async function whoami() {
	const auth = loadAuth();
	if (!auth) {
		console.log('Not authenticated. Run `mm login` first.');
		process.exit(1);
	}
	console.log(`User:  ${auth.userName} (${auth.userEmail})`);
	console.log(`ID:    ${auth.userId}`);
	console.log(`Token: ${auth.prefix}... (created ${auth.createdAt.slice(0, 10)})`);
}

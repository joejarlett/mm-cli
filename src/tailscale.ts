/**
 * Tailscale binary locator + MagicDNS suffix probe.
 *
 * The macOS App Store build of Tailscale does NOT put its CLI on $PATH,
 * so bare `tailscale status --json` fails with command-not-found. This
 * module probes a fallback list (see specs/agent-cli-and-delegation.md
 * § 5 step 2) and caches the resolved binary + the current MagicDNS
 * suffix for the process lifetime.
 *
 * Suffix usage: `mm chat <cmd> --node fedora` computes the connection
 * hostname as `${bareHostname}.${suffix}` so the URL stays valid across
 * Tailscale's occasional suffix rotation. The stored `app_instance.url`
 * is treated as "what nodes exist + where", while the local tailscaled
 * is the source of truth for "what's the current suffix."
 */

import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';

const TAILSCALE_PATHS = [
	'tailscale', // resolved via $PATH; first try
	'/Applications/Tailscale.app/Contents/MacOS/Tailscale', // macOS App Store
	'/Applications/Tailscale.app/Contents/Resources/bin/tailscale', // macOS standalone DMG
	'/usr/local/bin/tailscale', // Homebrew Intel
	'/opt/homebrew/bin/tailscale', // Homebrew Apple-silicon
	'/usr/bin/tailscale', // Linux fallback
];

let cachedPath: string | null = null;
let cachedSuffix: string | null = null;

function tryBinary(p: string): boolean {
	// For absolute paths, check existence. For bare `tailscale`, attempt a
	// quick exec — if PATH doesn't have it, spawnSync returns ENOENT in error.
	if (p.startsWith('/')) return existsSync(p);
	const r = spawnSync(p, ['--version'], { stdio: 'pipe', timeout: 2000 });
	return r.status === 0 && !r.error;
}

export function getTailscalePath(): string {
	if (cachedPath) return cachedPath;
	for (const p of TAILSCALE_PATHS) {
		if (tryBinary(p)) {
			cachedPath = p;
			return p;
		}
	}
	throw new Error(
		'Tailscale CLI not found. Install Tailscale and ensure it is on PATH or in a standard location.',
	);
}

type TailscaleStatus = {
	MagicDNSSuffix?: string;
	Self?: { DNSName?: string };
};

export function getTailscaleSuffix(): string {
	if (cachedSuffix) return cachedSuffix;
	const bin = getTailscalePath();
	const r = spawnSync(bin, ['status', '--json'], { stdio: 'pipe', timeout: 5000 });
	if (r.status !== 0) {
		throw new Error(
			`tailscale status --json failed (exit ${r.status}): ${r.stderr?.toString().slice(0, 200) ?? ''}`,
		);
	}
	let parsed: TailscaleStatus;
	try {
		parsed = JSON.parse(r.stdout.toString()) as TailscaleStatus;
	} catch (err) {
		throw new Error(`tailscale status --json: invalid JSON (${err})`);
	}
	let suffix = parsed.MagicDNSSuffix;
	if (!suffix && parsed.Self?.DNSName) {
		// Fallback: derive from Self.DNSName ("m4.taildd974e.ts.net." → "taildd974e.ts.net")
		const trimmed = parsed.Self.DNSName.replace(/\.$/, '');
		const dot = trimmed.indexOf('.');
		if (dot > 0) suffix = trimmed.slice(dot + 1);
	}
	if (!suffix) {
		throw new Error('tailscale status: could not determine MagicDNS suffix');
	}
	cachedSuffix = suffix;
	return suffix;
}

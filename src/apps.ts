/**
 * App registry — slug → base URL mapping.
 *
 * Centralised so the dispatcher and manifest fetcher can resolve any
 * app slug to its production URL without per-command duplication.
 */

export interface AppRegistryEntry {
	slug: string;
	url: string;
	description?: string;
}

export const APPS: Record<string, AppRegistryEntry> = {
	kb: { slug: 'kb', url: 'https://kb.meta-me.uk', description: 'Knowledge Base' },
	crm: { slug: 'crm', url: 'https://crm.meta-me.uk', description: 'CRM' },
	finances: { slug: 'finances', url: 'https://finances.meta-me.uk', description: 'Finances' },
	gn: { slug: 'gn', url: 'https://grounded.ninja', description: 'GroundedNinja' },
	analytics: {
		slug: 'analytics',
		url: 'https://analytics.meta-me.uk',
		description: 'Analytics'
	}
};

export function resolveApp(slug: string): AppRegistryEntry {
	const entry = APPS[slug];
	if (!entry) {
		throw new Error(
			`Unknown app: '${slug}'. Known apps: ${Object.keys(APPS).join(', ')}`
		);
	}
	return entry;
}

export function listApps(): AppRegistryEntry[] {
	return Object.values(APPS);
}

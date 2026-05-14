/**
 * mm status — Show auth status and available apps.
 */

import { loadAuth } from '../auth';

export async function status() {
	const auth = loadAuth();
	if (!auth) {
		console.log('Not authenticated. Run `mm login` first.');
		console.log('');
		console.log('Available apps:');
		console.log('  kb     Knowledge Base — search, read, manage documents');
		console.log('  crm    CRM — contacts, projects, interactions');
		console.log('');
		console.log('Run `mm login` to get started.');
		return;
	}

	console.log(`Authenticated as: ${auth.userName} (${auth.userEmail})`);
	console.log(`Token: ${auth.prefix}...`);
	console.log('');
	console.log('Available apps:');
	console.log('  kb     Knowledge Base — search, read, manage documents');
	console.log('  crm    CRM — contacts, projects, interactions');
	console.log('');
	console.log('Try:');
	console.log('  mm kb find "machine learning"');
	console.log('  mm crm contacts list');
}

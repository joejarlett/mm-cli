/**
 * Hub mm-RPC wire types — request/response shapes for every endpoint
 * mm-cli calls on `meta-me.uk/api/mm`.
 *
 * Naming: `Hub<Feature><Action>{Req|Resp}`. The feature/action pair is
 * what `hubApi(feature, action, payload)` sends; the prefix matches.
 *
 * Single source of truth — referenced from every command, never inlined.
 * Used both as the TS-side type contract and as the catalogue the
 * `specs/go-port/01-wire.md` doc renders from.
 *
 * Convention: optional fields use `?`. Nullable-on-the-wire fields use
 * `T | null` (Google APIs returning explicit `null` rather than absent).
 */

// ─── Calendar ──────────────────────────────────────────────────────────

export type HubCalendarEvent = {
	id: string;
	summary: string;
	start: string;
	end: string;
	location: string | null;
	htmlLink: string | null;
	attendees: number;
	allDay: boolean;
};

export type HubCalendarListReq = {
	days?: number;
	q?: string;
	accountSlug?: string;
};
export type HubCalendarListResp = {
	events: HubCalendarEvent[];
	rangeDays: number;
	accountSlug: string | null;
};

export type HubCalendarCreateReq = {
	title: string;
	when: string;
	end?: string;
	location?: string;
	description?: string;
	attendees?: string[];
	sendUpdates?: 'all' | 'externalOnly' | 'none';
	accountSlug?: string;
};
export type HubCalendarCreateResp = {
	id: string;
	htmlLink: string | null;
	summary: string;
	start: string;
	end: string;
};

// ─── Tasks ─────────────────────────────────────────────────────────────

export type HubTasksTask = {
	id: string;
	title: string;
	notes?: string;
	due?: string;
	status: 'needsAction' | 'completed';
	webViewLink?: string;
};

export type HubTasksGroup = {
	listId: string;
	listTitle: string;
	tasks: HubTasksTask[];
};

export type HubTasksListReq = {
	days?: number;
	all?: boolean;
	accountSlug?: string;
};
export type HubTasksListResp = {
	groups: HubTasksGroup[];
	accountSlug: string | null;
};

export type HubTasksAddReq = {
	title: string;
	due?: string;
	listTitle?: string;
	listId?: string;
	notes?: string;
	accountSlug?: string;
};
export type HubTasksAddResp = {
	id: string;
	listId: string;
	listTitle: string;
};

export type HubTasksCompleteReq = {
	listId: string;
	taskId: string;
	accountSlug?: string;
};
export type HubTasksCompleteResp = { ok: true };

// ─── Drive ─────────────────────────────────────────────────────────────

export type HubDriveFile = {
	id: string;
	name: string;
	mimeType: string;
	modifiedTime?: string;
	webViewLink?: string;
};

export type HubDriveListReq = {
	q?: string;
	max?: number;
	accountSlug?: string;
};
export type HubDriveListResp = {
	files: HubDriveFile[];
	accountSlug: string | null;
};

export type HubDriveCreateDocReq = {
	name: string;
	content: string;
	sourceMime: 'text/markdown' | 'text/plain' | 'text/html';
	accountSlug?: string;
};
export type HubDriveCreateDocResp = {
	id: string;
	name: string;
	webViewLink: string | null;
};

export type HubDriveUpdateReq = {
	fileId: string;
	name?: string;
	addParents?: string[];
	removeParents?: string[];
	accountSlug?: string;
};
export type HubDriveUpdateResp = {
	id: string;
	name: string;
	mimeType: string;
	parents: string[];
	modifiedTime: string | null;
	webViewLink: string | null;
};

// ─── Email — platform outbound log (admin) ─────────────────────────────

export type HubEmailLogRow = {
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

export type HubEmailListReq = {
	status?: string;
	template?: string;
	q?: string;
	limit?: number;
};
export type HubEmailListResp = {
	rows: HubEmailLogRow[];
	nextCursor: string | null;
};

export type HubEmailGetReq = { id: string };
export type HubEmailGetResp = HubEmailLogRow & {
	fromAddress: string;
	bodyHtml: string;
	bodyText: string;
	templateParams: Record<string, unknown> | null;
	messageId: string | null;
};

export type HubEmailCreateReq = {
	to: string;
	subject: string;
	html: string;
	text?: string;
	template?: string;
};
export type HubEmailCreateResp = { id: string };

export type HubEmailSendReq = { id: string };
export type HubEmailSendResp = {
	success: boolean;
	error?: string;
};

export type HubEmailResendReq = { id: string };
export type HubEmailResendResp = {
	newId: string;
	success: boolean;
	error?: string;
};

// ─── Email — Gmail inbox (via gws-gateway) ─────────────────────────────

export type HubInboxMessage = {
	id: string;
	threadId: string;
	subject: string;
	from: string;
	to: string;
	date: string;
	snippet: string;
	unread: boolean;
};

export type HubInboxSearchReq = {
	q?: string;
	maxResults?: number;
	accountSlug?: string;
};
export type HubInboxSearchResp = {
	messages: HubInboxMessage[];
	accountSlug: string | null;
};

export type HubInboxReadReq = { id: string };
export type HubInboxReadResp = HubInboxMessage & {
	cc: string;
	body: string;
	labels: string[];
};

// ─── Instance (cross-app — used by `mm chat nodes` to discover agents) ─

export type HubInstanceListReq = {
	slugs?: string[];
};
export type HubInstance = {
	id: string;
	appSlug: string;
	name: string;
	url: string | null;
	isOwner: boolean;
};
export type HubInstanceListResp = {
	instances: HubInstance[];
};

/**
 * Local-agent REST + WS wire types — request/response shapes for every
 * endpoint mm-cli calls on `http://localhost:3142` (or the tailnet URL
 * of a `--node` target).
 *
 * Naming: `Agent<Resource><Verb>` for REST; `AgentWs<EventType>` for WS.
 *
 * Reference for the agent source: ~/Documents/dev/meta-me-local-agent.
 */

// ─── Threads + messages ────────────────────────────────────────────────

export type AgentThread = {
	id: string;
	title: string;
	project_id: string | null;
	model_provider: string | null;
	model_id: string | null;
	updated_at: number;
	msg_count?: number;
};

export type AgentThreadsListResp = {
	threads: AgentThread[];
};

export type AgentMessage = {
	id: string;
	role: 'user' | 'assistant' | 'system' | 'tool';
	content: string;
	created_at: number;
};

export type AgentThreadMessagesResp = {
	messages: AgentMessage[];
};

export type AgentMessageSearchMatch = AgentMessage & {
	thread_id: string;
	title: string;
};

export type AgentMessageSearchResp = {
	matches: AgentMessageSearchMatch[];
};

// ─── Projects ──────────────────────────────────────────────────────────

export type AgentProject = {
	id: string;
	label: string;
	root_path: string;
	last_opened_at: number | null;
	thread_count?: number;
};

export type AgentProjectsListResp = {
	projects: AgentProject[];
};

// ─── Models ────────────────────────────────────────────────────────────

export type AgentModel = {
	provider: string;
	id: string;
	label: string;
	input: string[];
};

export type AgentModelsListResp = {
	models: AgentModel[];
};

// ─── Health ────────────────────────────────────────────────────────────

export type AgentHealthResp = {
	ok: boolean;
	ts: number;
	version: string;
	uptime: number;
	sockets: number;
	sessions: number;
	installId: string | null;
	tcc: {
		ok: boolean;
		platform: string;
		checkedAt: number;
		failedPaths: string[];
		recentDenialAt?: number;
	};
};

// ─── WebSocket protocol — outbound (CLI → agent) ───────────────────────

export type AgentWsSend = {
	type: 'send';
	threadId: string;
	content: string;
	provider?: string;
	modelId?: string;
	projectId?: string;
};

export type AgentWsResume = {
	type: 'resume';
	threadId: string;
	cursor: number;
};

export type AgentWsPing = { type: 'ping' };

export type AgentWsOutbound = AgentWsSend | AgentWsResume | AgentWsPing;

// ─── WebSocket protocol — inbound (agent → CLI) ────────────────────────

export type AgentWsDelta = {
	type: 'delta';
	threadId: string;
	cursor: number;
	text: string;
};

export type AgentWsThinkingDelta = {
	type: 'thinking_delta';
	threadId: string;
	cursor: number;
	text: string;
};

export type AgentWsToolStart = {
	type: 'tool_start';
	threadId: string;
	cursor: number;
	toolName: string;
	args?: unknown;
};

export type AgentWsToolEnd = {
	type: 'tool_end';
	threadId: string;
	cursor: number;
	toolName: string;
	result?: unknown;
};

export type AgentWsStatus = {
	type: 'status';
	threadId: string;
	cursor: number;
	message: string;
};

export type AgentWsDone = {
	type: 'done';
	threadId: string;
	cursor: number;
	fullText?: string;
	fullThinking?: string;
};

export type AgentWsError = {
	type: 'error';
	threadId: string;
	cursor: number;
	message: string;
};

export type AgentWsResumeEmpty = {
	type: 'resume_empty';
	threadId: string;
};

export type AgentWsInbound =
	| AgentWsDelta
	| AgentWsThinkingDelta
	| AgentWsToolStart
	| AgentWsToolEnd
	| AgentWsStatus
	| AgentWsDone
	| AgentWsError
	| AgentWsResumeEmpty;

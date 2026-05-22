package wire

// Local-agent REST + WS wire types. Mirrors src/wire/agent.ts.
// Reference source: ~/Documents/dev/meta-me-local-agent.

// ─── Threads + messages ────────────────────────────────────────────────

type AgentThread struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	ProjectID     *string `json:"project_id"`
	ModelProvider *string `json:"model_provider"`
	ModelID       *string `json:"model_id"`
	UpdatedAt     int64   `json:"updated_at"`
	MsgCount      *int    `json:"msg_count,omitempty"`
}

type AgentThreadsListResp struct {
	Threads []AgentThread `json:"threads"`
}

type AgentMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}

type AgentThreadMessagesResp struct {
	Messages []AgentMessage `json:"messages"`
}

type AgentMessageSearchMatch struct {
	AgentMessage
	ThreadID string `json:"thread_id"`
	Title    string `json:"title"`
}

type AgentMessageSearchResp struct {
	Matches []AgentMessageSearchMatch `json:"matches"`
}

// ─── Projects ──────────────────────────────────────────────────────────

type AgentProject struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	RootPath     string  `json:"root_path"`
	LastOpenedAt *int64  `json:"last_opened_at"`
	ThreadCount  *int    `json:"thread_count,omitempty"`
}

type AgentProjectsListResp struct {
	Projects []AgentProject `json:"projects"`
}

// ─── Models ────────────────────────────────────────────────────────────

type AgentModel struct {
	Provider string   `json:"provider"`
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Input    []string `json:"input"`
}

type AgentModelsListResp struct {
	Models []AgentModel `json:"models"`
}

// ─── Health ────────────────────────────────────────────────────────────

type AgentHealthResp struct {
	OK         bool   `json:"ok"`
	TS         int64  `json:"ts"`
	Version    string `json:"version"`
	Uptime     int    `json:"uptime"`
	Sockets    int    `json:"sockets"`
	Sessions   int    `json:"sessions"`
	InstallID  *string `json:"installId"`
}

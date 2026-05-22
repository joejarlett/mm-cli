// Package wire holds the request/response structs for every mm-cli HTTP
// surface. Mirrors src/wire/ in the TS source — same JSON shapes, same
// field names. The Go port either codegens these from the TS or hand-
// translates 1:1; this file is the hand-translated version.
package wire

// ─── Calendar ──────────────────────────────────────────────────────────

type HubCalendarEvent struct {
	ID        string  `json:"id"`
	Summary   string  `json:"summary"`
	Start     string  `json:"start"`
	End       string  `json:"end"`
	Location  *string `json:"location"`
	HTMLLink  *string `json:"htmlLink"`
	Attendees int     `json:"attendees"`
	AllDay    bool   `json:"allDay"`
}

type HubCalendarListReq struct {
	Days        *int    `json:"days,omitempty"`
	Q           string  `json:"q,omitempty"`
	AccountSlug string  `json:"accountSlug,omitempty"`
}

type HubCalendarListResp struct {
	Events      []HubCalendarEvent `json:"events"`
	RangeDays   int                `json:"rangeDays"`
	AccountSlug *string            `json:"accountSlug"`
}

type HubCalendarCreateReq struct {
	Title       string   `json:"title"`
	When        string   `json:"when"`
	End         string   `json:"end,omitempty"`
	Location    string   `json:"location,omitempty"`
	Description string   `json:"description,omitempty"`
	Attendees   []string `json:"attendees,omitempty"`
	SendUpdates string   `json:"sendUpdates,omitempty"` // "all"|"externalOnly"|"none"
	AccountSlug string   `json:"accountSlug,omitempty"`
}

type HubCalendarCreateResp struct {
	ID       string  `json:"id"`
	HTMLLink *string `json:"htmlLink"`
	Summary  string  `json:"summary"`
	Start    string  `json:"start"`
	End      string  `json:"end"`
}

// ─── Tasks ─────────────────────────────────────────────────────────────

type HubTasksTask struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Notes       string `json:"notes,omitempty"`
	Due         string `json:"due,omitempty"`
	Status      string `json:"status"` // "needsAction"|"completed"
	WebViewLink string `json:"webViewLink,omitempty"`
}

type HubTasksGroup struct {
	ListID    string         `json:"listId"`
	ListTitle string         `json:"listTitle"`
	Tasks     []HubTasksTask `json:"tasks"`
}

type HubTasksListReq struct {
	Days        *int   `json:"days,omitempty"`
	All         bool   `json:"all,omitempty"`
	AccountSlug string `json:"accountSlug,omitempty"`
}

type HubTasksListResp struct {
	Groups      []HubTasksGroup `json:"groups"`
	AccountSlug *string         `json:"accountSlug"`
}

type HubTasksAddReq struct {
	Title       string `json:"title"`
	Due         string `json:"due,omitempty"`
	ListTitle   string `json:"listTitle,omitempty"`
	ListID      string `json:"listId,omitempty"`
	Notes       string `json:"notes,omitempty"`
	AccountSlug string `json:"accountSlug,omitempty"`
}

type HubTasksAddResp struct {
	ID        string `json:"id"`
	ListID    string `json:"listId"`
	ListTitle string `json:"listTitle"`
}

type HubTasksCompleteReq struct {
	ListID      string `json:"listId"`
	TaskID      string `json:"taskId"`
	AccountSlug string `json:"accountSlug,omitempty"`
}

type HubTasksCompleteResp struct {
	OK bool `json:"ok"`
}

// ─── Drive ─────────────────────────────────────────────────────────────

type HubDriveFile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	ModifiedTime string `json:"modifiedTime,omitempty"`
	WebViewLink  string `json:"webViewLink,omitempty"`
}

type HubDriveListReq struct {
	Q           string `json:"q,omitempty"`
	Max         *int   `json:"max,omitempty"`
	AccountSlug string `json:"accountSlug,omitempty"`
}

type HubDriveListResp struct {
	Files       []HubDriveFile `json:"files"`
	AccountSlug *string        `json:"accountSlug"`
}

type HubDriveCreateDocReq struct {
	Name        string `json:"name"`
	Content     string `json:"content"`
	SourceMime  string `json:"sourceMime"` // "text/markdown"|"text/plain"|"text/html"
	AccountSlug string `json:"accountSlug,omitempty"`
}

type HubDriveCreateDocResp struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	WebViewLink *string `json:"webViewLink"`
}

type HubDriveUpdateReq struct {
	FileID        string   `json:"fileId"`
	Name          string   `json:"name,omitempty"`
	AddParents    []string `json:"addParents,omitempty"`
	RemoveParents []string `json:"removeParents,omitempty"`
	AccountSlug   string   `json:"accountSlug,omitempty"`
}

type HubDriveUpdateResp struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	MimeType     string   `json:"mimeType"`
	Parents      []string `json:"parents"`
	ModifiedTime *string  `json:"modifiedTime"`
	WebViewLink  *string  `json:"webViewLink"`
}

// ─── Email — platform outbound log (admin) ─────────────────────────────

type HubEmailLogRow struct {
	ID          string  `json:"id"`
	UserID      *string `json:"userId"`
	ToAddress   string  `json:"toAddress"`
	Subject     string  `json:"subject"`
	Template    *string `json:"template"`
	Status      string  `json:"status"`
	TriggeredBy *string `json:"triggeredBy"`
	CreatedAt   string  `json:"createdAt"`
	SentAt      *string `json:"sentAt"`
	FailedAt    *string `json:"failedAt"`
	Error       *string `json:"error"`
	ParentID    *string `json:"parentId"`
}

type HubEmailListReq struct {
	Status   string `json:"status,omitempty"`
	Template string `json:"template,omitempty"`
	Q        string `json:"q,omitempty"`
	Limit    *int   `json:"limit,omitempty"`
}

type HubEmailListResp struct {
	Rows       []HubEmailLogRow `json:"rows"`
	NextCursor *string          `json:"nextCursor"`
}

type HubEmailGetReq struct {
	ID string `json:"id"`
}

type HubEmailGetResp struct {
	HubEmailLogRow
	FromAddress    string                 `json:"fromAddress"`
	BodyHTML       string                 `json:"bodyHtml"`
	BodyText       string                 `json:"bodyText"`
	TemplateParams map[string]interface{} `json:"templateParams"`
	MessageID      *string                `json:"messageId"`
}

type HubEmailCreateReq struct {
	To       string `json:"to"`
	Subject  string `json:"subject"`
	HTML     string `json:"html"`
	Text     string `json:"text,omitempty"`
	Template string `json:"template,omitempty"`
}

type HubEmailCreateResp struct {
	ID string `json:"id"`
}

type HubEmailSendReq struct {
	ID string `json:"id"`
}

type HubEmailSendResp struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type HubEmailResendReq struct {
	ID string `json:"id"`
}

type HubEmailResendResp struct {
	NewID   string `json:"newId"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ─── Email — Gmail inbox (via gws-gateway) ─────────────────────────────

type HubInboxMessage struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
	Subject  string `json:"subject"`
	From     string `json:"from"`
	To       string `json:"to"`
	Date     string `json:"date"`
	Snippet  string `json:"snippet"`
	Unread   bool   `json:"unread"`
}

type HubInboxSearchReq struct {
	Q           string `json:"q,omitempty"`
	MaxResults  *int   `json:"maxResults,omitempty"`
	AccountSlug string `json:"accountSlug,omitempty"`
}

type HubInboxSearchResp struct {
	Messages    []HubInboxMessage `json:"messages"`
	AccountSlug *string           `json:"accountSlug"`
}

type HubInboxReadReq struct {
	ID string `json:"id"`
}

type HubInboxReadResp struct {
	HubInboxMessage
	CC     string   `json:"cc"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

// ─── Instance discovery ────────────────────────────────────────────────

type HubInstanceListReq struct {
	Slugs []string `json:"slugs,omitempty"`
}

type HubInstance struct {
	ID      string  `json:"id"`
	AppSlug string  `json:"appSlug"`
	Name    string  `json:"name"`
	URL     *string `json:"url"`
	IsOwner bool    `json:"isOwner"`
}

type HubInstanceListResp struct {
	Instances []HubInstance `json:"instances"`
}

// ─── Envelope ──────────────────────────────────────────────────────────

// HubEnvelope is the top-level response shape for /api/mm and /api/v2.
// On success: { data: T }. On failure: { errors: [...] }.
type HubEnvelope[T any] struct {
	Data   T            `json:"data,omitempty"`
	Errors []HubErrItem `json:"errors,omitempty"`
}

type HubErrItem struct {
	Code   string `json:"code,omitempty"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail,omitempty"`
	Status string `json:"status,omitempty"`
}

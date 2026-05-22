package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	mmhttp "mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// NewDeskCmd builds the `mm desk` tree (`mm chat` kept as alias).
func NewDeskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "desk",
		Aliases: []string{"chat"},
		Short:   "Local agent threads (list/show/search/send/nodes/models)",
		RunE:    runChatList,
	}
	chatFlags(cmd, false)
	cmd.AddCommand(
		newChatListCmd(), newChatShowCmd(), newChatSearchCmd(),
		newChatProjectsCmd(), newChatSendCmd(), newChatNodesCmd(), newChatModelsCmd(),
	)
	return cmd
}

func chatFlags(c *cobra.Command, withSendOnly bool) {
	c.Flags().String("node", "", "Target a remote agent by name")
	if !withSendOnly {
		c.Flags().Int("limit", 0, "Max rows")
		c.Flags().String("project", "", "Project UUID or label")
	}
}

func newChatListCmd() *cobra.Command {
	c := &cobra.Command{Use: "list", Short: "Recent threads", Args: cobra.NoArgs, RunE: runChatList}
	chatFlags(c, false)
	return c
}

func newChatShowCmd() *cobra.Command {
	c := &cobra.Command{Use: "show [id]", Aliases: []string{"read"}, Short: "Print messages in a thread", Args: cobra.ExactArgs(1), RunE: runChatShow}
	chatFlags(c, false)
	return c
}

func newChatSearchCmd() *cobra.Command {
	c := &cobra.Command{Use: "search [query]", Aliases: []string{"find"}, Short: "Substring search across messages", Args: cobra.ExactArgs(1), RunE: runChatSearch}
	c.Flags().String("node", "", "Target a remote agent")
	c.Flags().Int("limit", 20, "Max results")
	return c
}

func newChatProjectsCmd() *cobra.Command {
	c := &cobra.Command{Use: "projects", Short: "List projects + thread counts", Args: cobra.NoArgs, RunE: runChatProjects}
	c.Flags().String("node", "", "Target a remote agent")
	return c
}

func newChatNodesCmd() *cobra.Command {
	return &cobra.Command{Use: "nodes", Short: "List registered agent nodes (from instance.list)", Args: cobra.NoArgs, RunE: runChatNodes}
}

func newChatModelsCmd() *cobra.Command {
	c := &cobra.Command{Use: "models", Short: "List models the agent has provider keys for", Args: cobra.NoArgs, RunE: runChatModels}
	c.Flags().String("node", "", "Target a remote agent")
	return c
}

// ─── list ──────────────────────────────────────────────────────────────

func runChatList(cmd *cobra.Command, _ []string) error {
	node, _ := cmd.Flags().GetString("node")
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 20
	}
	project, _ := cmd.Flags().GetString("project")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	if project != "" {
		q.Set("project_id", project)
	}
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), node, "/api/threads?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 && project != "" {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("project '%s' not found. %s", project, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /api/threads %d", resp.StatusCode)
	}
	var data wire.AgentThreadsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(data.Threads, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(data.Threads) == 0 {
		fmt.Println("(no threads)")
		return nil
	}
	for _, t := range data.Threads {
		id6 := t.ID
		if len(id6) > 6 {
			id6 = id6[:6]
		}
		proj := ""
		if t.ProjectID != nil && *t.ProjectID != "" {
			p := *t.ProjectID
			if len(p) > 6 {
				p = p[:6]
			}
			proj = " [" + p + "]"
		}
		model := ""
		if t.ModelID != nil && *t.ModelID != "" {
			parts := strings.Split(*t.ModelID, "/")
			model = " " + parts[len(parts)-1]
		}
		count := 0
		if t.MsgCount != nil {
			count = *t.MsgCount
		}
		title := truncString(strings.Join(strings.Fields(t.Title), " "), 60)
		fmt.Printf("%s  %s %3dmsg  %s%s%s\n",
			id6, padRight(relTimeMs(t.UpdatedAt), 8), count, title, proj, model)
	}
	return nil
}

// ─── show ──────────────────────────────────────────────────────────────

func runChatShow(cmd *cobra.Command, args []string) error {
	node, _ := cmd.Flags().GetString("node")
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 50
	}
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := mmhttp.New()

	id, err := resolveThreadID(cmd.Context(), client, node, args[0])
	if err != nil {
		return err
	}
	threadsResp, err := client.AgentFetch(cmd.Context(), node, "/api/threads?limit=1000", nil)
	if err != nil {
		return err
	}
	defer threadsResp.Body.Close()
	if threadsResp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /api/threads %d", threadsResp.StatusCode)
	}
	var tdata wire.AgentThreadsListResp
	if err := json.NewDecoder(threadsResp.Body).Decode(&tdata); err != nil {
		return err
	}
	var thread *wire.AgentThread
	for i := range tdata.Threads {
		if tdata.Threads[i].ID == id {
			thread = &tdata.Threads[i]
			break
		}
	}
	if thread == nil {
		return fmt.Errorf("thread not found: %s", id)
	}
	msgsResp, err := client.AgentFetch(cmd.Context(), node, "/api/threads/"+id+"/messages", nil)
	if err != nil {
		return err
	}
	defer msgsResp.Body.Close()
	var mdata wire.AgentThreadMessagesResp
	if err := json.NewDecoder(msgsResp.Body).Decode(&mdata); err != nil {
		return err
	}
	if len(mdata.Messages) > limit {
		mdata.Messages = mdata.Messages[:limit]
	}
	if wantJSON {
		out, _ := json.MarshalIndent(map[string]any{"thread": thread, "messages": mdata.Messages}, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("# %s\n", thread.Title)
	fmt.Printf("id: %s\n", thread.ID)
	fmt.Printf("updated: %s\n", fmtTimeMs(thread.UpdatedAt))
	if thread.ProjectID != nil && *thread.ProjectID != "" {
		fmt.Printf("project: %s\n", *thread.ProjectID)
	}
	if thread.ModelID != nil && *thread.ModelID != "" {
		prov := ""
		if thread.ModelProvider != nil {
			prov = *thread.ModelProvider + "/"
		}
		fmt.Printf("model: %s%s\n", prov, *thread.ModelID)
	}
	fmt.Println()
	for _, m := range mdata.Messages {
		fmt.Printf("── %s · %s ──\n", m.Role, fmtTimeMs(m.CreatedAt))
		fmt.Println(m.Content)
		fmt.Println()
	}
	if len(mdata.Messages) == limit {
		fmt.Printf("(showing first %d messages; use --limit to see more)\n", limit)
	}
	return nil
}

// ─── search ────────────────────────────────────────────────────────────

func runChatSearch(cmd *cobra.Command, args []string) error {
	query := args[0]
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 20
	}
	node, _ := cmd.Flags().GetString("node")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	q := url.Values{}
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), node, "/api/messages/search?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return fmt.Errorf("agent doesn't have /api/messages/search yet — needs the current meta-me-local-agent build")
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /api/messages/search %d", resp.StatusCode)
	}
	var data wire.AgentMessageSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(data.Matches, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(data.Matches) == 0 {
		fmt.Println("(no matches)")
		return nil
	}
	for _, r := range data.Matches {
		tid6 := r.ThreadID
		if len(tid6) > 6 {
			tid6 = tid6[:6]
		}
		fmt.Printf("%s  %s %s %s\n",
			tid6, padRight(relTimeMs(r.CreatedAt), 8), padRight(r.Role, 9), truncString(r.Title, 30))
		fmt.Printf("        %s\n", truncString(strings.Join(strings.Fields(r.Content), " "), 100))
	}
	return nil
}

// ─── projects ──────────────────────────────────────────────────────────

func runChatProjects(cmd *cobra.Command, _ []string) error {
	node, _ := cmd.Flags().GetString("node")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), node, "/api/projects", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /api/projects %d", resp.StatusCode)
	}
	var data wire.AgentProjectsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(data.Projects, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(data.Projects) == 0 {
		fmt.Println("(no projects)")
		return nil
	}
	for _, p := range data.Projects {
		id6 := p.ID
		if len(id6) > 6 {
			id6 = id6[:6]
		}
		count := 0
		if p.ThreadCount != nil {
			count = *p.ThreadCount
		}
		fmt.Printf("%s  %3dthr  %s  %s\n", id6, count, padRight(p.Label, 24), p.RootPath)
	}
	return nil
}

// ─── nodes (hub instance.list) ─────────────────────────────────────────

func runChatNodes(cmd *cobra.Command, _ []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := mmhttp.New()
	var data wire.HubInstanceListResp
	if err := client.Hub(cmd.Context(), "instance", "list", map[string]any{"slugs": []string{"chat", "agent", "desk"}}, &data); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(data.Instances, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(data.Instances) == 0 {
		fmt.Println("(no nodes registered)")
		fmt.Println("Register one with: mm v2 instance.create --slug=chat --label=<name> --url=<url>")
		return nil
	}
	for _, r := range data.Instances {
		owner := ""
		if !r.IsOwner {
			owner = " (shared)"
		}
		urlStr := "(no url)"
		if r.URL != nil {
			urlStr = *r.URL
		}
		fmt.Printf("%s %s %s%s\n", padRight(r.Name, 20), padRight(r.AppSlug, 8), urlStr, owner)
	}
	return nil
}

// ─── models ────────────────────────────────────────────────────────────

func runChatModels(cmd *cobra.Command, _ []string) error {
	node, _ := cmd.Flags().GetString("node")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), node, "/api/models", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /api/models %d", resp.StatusCode)
	}
	var data wire.AgentModelsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(data.Models, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(data.Models) == 0 {
		fmt.Println("(no models available — set a provider key via the SPA or ~/.pi/agent/auth.json)")
		return nil
	}
	for _, m := range data.Models {
		fullID := m.Provider + "/" + m.ID
		inputs := strings.Join(m.Input, ",")
		fmt.Printf("%s %s [%s]\n", padRight(m.Label, 6), padRight(fullID, 40), inputs)
	}
	return nil
}

// ─── send (WS) lives in chat_send.go ───────────────────────────────────

// ─── helpers ───────────────────────────────────────────────────────────

// resolveThreadID accepts a full UUID or a >=4-char prefix; for the prefix,
// fetch threads and match client-side.
func resolveThreadID(ctx context.Context, client *mmhttp.Client, node, prefix string) (string, error) {
	if uuidRe.MatchString(prefix) {
		return prefix, nil
	}
	if len(prefix) < 4 {
		return "", fmt.Errorf("thread prefix '%s' is too short (need ≥4 chars)", prefix)
	}
	resp, err := client.AgentFetch(ctx, node, "/api/threads?limit=1000", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET /api/threads %d", resp.StatusCode)
	}
	var data wire.AgentThreadsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	var matches []string
	for _, t := range data.Threads {
		if strings.HasPrefix(t.ID, strings.ToLower(prefix)) {
			matches = append(matches, t.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("thread not found: %s", prefix)
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("thread prefix '%s' is ambiguous (%d matches)", prefix, len(matches))
}

// Suppress unused-import warnings for http (will be used by send).
var _ = http.MethodGet

func fmtTimeMs(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
}

func relTimeMs(ms int64) string {
	diff := time.Since(time.UnixMilli(ms))
	s := int(diff.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds ago", s)
	}
	m := s / 60
	if m < 60 {
		return fmt.Sprintf("%dm ago", m)
	}
	h := m / 60
	if h < 24 {
		return fmt.Sprintf("%dh ago", h)
	}
	return fmt.Sprintf("%dd ago", h/24)
}

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	mmhttp "mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// NewProjectCmd builds the `mm project` tree.
func NewProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project",
		Aliases: []string{"projects"},
		Short:   "Local agent project index (overview/detail/add)",
		RunE:    runProjectList,
	}
	cmd.AddCommand(
		newProjectListCmd(), newProjectOverviewCmd(),
		newProjectDetailCmd(), newProjectAddCmd(), newProjectRebuildCmd(),
	)
	return cmd
}

func newProjectListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List registered projects", Args: cobra.NoArgs, RunE: runProjectList}
}

func newProjectOverviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "overview [name|path] [subpath]",
		Short: "Folder-level summaries with drift counts",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runProjectOverview,
	}
}

func newProjectDetailCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "detail [name|path] [subpath]",
		Short: "Per-file summaries under a folder",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runProjectDetail,
	}
	c.Flags().String("search", "", "Filter")
	c.Flags().Int("limit", 0, "Max rows")
	c.Flags().Bool("shallow", false, "Don't recurse")
	return c
}

func newProjectAddCmd() *cobra.Command {
	return &cobra.Command{Use: "add [path] [label]", Short: "Register a folder as a project", Args: cobra.RangeArgs(1, 2), RunE: runProjectAdd}
}

func newProjectRebuildCmd() *cobra.Command {
	return &cobra.Command{Use: "rebuild [name|path] [subpath]", Short: "Drop cached rows and re-summarise", Args: cobra.RangeArgs(1, 2), RunE: runProjectRebuild}
}

// ─── List ──────────────────────────────────────────────────────────────

func runProjectList(cmd *cobra.Command, _ []string) error {
	return listProjects(cmd, "")
}

// listProjects is the single implementation of the desk project catalogue —
// the registered projects with thread counts. Shared by `mm project list`,
// `mm desk projects`, and `mm overview desk` so the listing renders one way
// everywhere. node selects a remote agent ("" = local).
func listProjects(cmd *cobra.Command, node string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), node, "/api/projects", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /api/projects %d: %s", resp.StatusCode, truncString(string(body), 200))
	}
	if wantJSON {
		fmt.Println(string(body))
		return nil
	}
	var data wire.AgentProjectsListResp
	if err := json.Unmarshal(body, &data); err != nil {
		return err
	}
	fmt.Print(renderProjects(data))
	return nil
}

// renderProjects is the canonical markdown for the project catalogue, matching
// the `mm overview <app>` house style (count header + markdown bullets).
func renderProjects(data wire.AgentProjectsListResp) string {
	if len(data.Projects) == 0 {
		return "_No projects._\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Projects (%d)\n\n", len(data.Projects))
	for _, p := range data.Projects {
		id6 := p.ID
		if len(id6) > 6 {
			id6 = id6[:6]
		}
		count := 0
		if p.ThreadCount != nil {
			count = *p.ThreadCount
		}
		fmt.Fprintf(&b, "- **%s** (`%s`) — %d thread%s · `%s`\n", p.Label, id6, count, plural(count), p.RootPath)
	}
	return b.String()
}

// ─── Overview / Detail / Rebuild — pass-through to agent ───────────────

func runProjectOverview(cmd *cobra.Command, args []string) error {
	id, err := resolveProjectRef(cmd, args[0])
	if err != nil {
		return err
	}
	subpath := ""
	if len(args) == 2 {
		subpath = args[1]
	}
	return passthroughGet(cmd, fmt.Sprintf("/api/projects/%s/overview?path=%s", id, url.QueryEscape(subpath)))
}

func runProjectDetail(cmd *cobra.Command, args []string) error {
	id, err := resolveProjectRef(cmd, args[0])
	if err != nil {
		return err
	}
	subpath := ""
	if len(args) == 2 {
		subpath = args[1]
	}
	search, _ := cmd.Flags().GetString("search")
	limit, _ := cmd.Flags().GetInt("limit")
	shallow, _ := cmd.Flags().GetBool("shallow")
	q := url.Values{}
	q.Set("path", subpath)
	if search != "" {
		q.Set("search", search)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	deep := "1"
	if shallow {
		deep = "0"
	}
	q.Set("deep", deep)
	return passthroughGet(cmd, fmt.Sprintf("/api/projects/%s/index?%s", id, q.Encode()))
}

func runProjectRebuild(cmd *cobra.Command, args []string) error {
	id, err := resolveProjectRef(cmd, args[0])
	if err != nil {
		return err
	}
	subpath := ""
	if len(args) == 2 {
		subpath = args[1]
	}
	body := map[string]any{}
	if subpath != "" {
		body["path"] = subpath
	}
	bodyJSON, _ := json.Marshal(body)
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), "", fmt.Sprintf("/api/projects/%s/index/refresh", id), &mmhttp.AgentReq{
		Method:      "POST",
		Body:        bodyJSON,
		ContentType: "application/json",
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("rebuild failed (%d): %s", resp.StatusCode, truncString(string(b), 200))
	}
	fmt.Println(string(b))
	return nil
}

// ─── Add ───────────────────────────────────────────────────────────────

func runProjectAdd(cmd *cobra.Command, args []string) error {
	rootPath, err := filepath.Abs(expandTilde(args[0]))
	if err != nil {
		return err
	}
	label := ""
	if len(args) == 2 {
		label = args[1]
	}
	body := map[string]any{"root_path": rootPath}
	if label != "" {
		body["label"] = label
	}
	bodyJSON, _ := json.Marshal(body)
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), "", "/api/projects", &mmhttp.AgentReq{
		Method:      "POST",
		Body:        bodyJSON,
		ContentType: "application/json",
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /api/projects %d: %s", resp.StatusCode, truncString(string(b), 200))
	}
	b, _ := io.ReadAll(resp.Body)
	fmt.Println(string(b))
	return nil
}

// ─── helpers ───────────────────────────────────────────────────────────

// resolveProjectRef accepts either a UUID, a label, or a filesystem path
// and returns the project's UUID by hitting /api/projects.
func resolveProjectRef(cmd *cobra.Command, ref string) (string, error) {
	if uuidRe.MatchString(ref) {
		return ref, nil
	}
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), "", "/api/projects", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET /api/projects %d", resp.StatusCode)
	}
	var data wire.AgentProjectsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	abs, _ := filepath.Abs(expandTilde(ref))
	for _, p := range data.Projects {
		if strings.EqualFold(p.Label, ref) || p.RootPath == abs {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("project not found: %s", ref)
}

func passthroughGet(cmd *cobra.Command, path string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), "", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET %s %d: %s", path, resp.StatusCode, truncString(string(b), 200))
	}
	if wantJSON {
		fmt.Println(string(b))
		return nil
	}
	// Default rendering: pretty-print JSON. The TS side has bespoke per-mode
	// renderers; for parity we'll wire those in a follow-up but JSON output
	// is faithfully usable today.
	var v any
	if err := json.Unmarshal(b, &v); err == nil {
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Println(string(b))
	return nil
}

func expandTilde(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

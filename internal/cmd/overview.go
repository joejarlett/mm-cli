package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	mmhttp "mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// The `overview` / `surface` halves of the cards · overview · surface
// contract (specs/surface-overview-contract.md in the hub repo):
//
//	mm cards / mm manifest → what an app can DO
//	mm overview [app]      → what IS here   (stable catalogue)
//	mm surface  [app]      → what's HAPPENING now (decaying activity)
//
// With no app, both call the hub aggregators (overview.get / surface.get),
// which fan out across every app whose Agent Card declares the capability —
// one call, everything. With an app, they scope to that app.
//
// Desk is the asymmetric case: it's a browser-only local agent the hub can't
// reach, so the hub aggregator omits it. `mm overview desk` / `mm surface
// desk` are served locally (pull-mode), the same split as `desk.capture`.

// NewOverviewCmd builds `mm overview [app]`.
func NewOverviewCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "overview [app]",
		Short: "What's here — the stable catalogue across apps (or one)",
		Long: "What IS here — the stable content map of each app (notebooks, instances,\n" +
			"projects). The catalogue half of cards · overview · surface; for activity\n" +
			"use `mm surface`, for capabilities `mm cards`.\n\n" +
			"With no app, fans out across every app via the hub. With an app, scopes to\n" +
			"that one. Add --json for structured output.",
		Example: "  mm overview\n  mm overview kb\n  mm overview crm --json",
		Args:    cobra.MaximumNArgs(1),
		RunE:    runOverview,
	}
	c.Flags().String("node", "", "Target a remote agent by name (desk only)")
	return c
}

// NewSurfaceCmd builds `mm surface [app]`.
func NewSurfaceCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "surface [app]",
		Short: "What's happening now — recent activity across apps (or one)",
		Long: "What's HAPPENING now — decaying activity, most-relevant first (recent\n" +
			"touches, open loops, follow-ups due). The activity half of cards · overview\n" +
			"· surface; for the stable catalogue use `mm overview`.\n\n" +
			"With no app, fans out across every app via the hub. With an app, scopes to\n" +
			"that one. Add --json for structured output.",
		Example: "  mm surface\n  mm surface crm\n  mm surface desk",
		Args:    cobra.MaximumNArgs(1),
		RunE:    runSurface,
	}
	c.Flags().Int("limit", 0, "Max items per app")
	c.Flags().String("node", "", "Target a remote agent by name (desk only)")
	return c
}

// ─── overview ───────────────────────────────────────────────────────────

func runOverview(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	app := ""
	if len(args) == 1 {
		app = strings.ToLower(args[0])
	}
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	// Desk is pull-mode — the hub can't reach the local agent.
	if app == "desk" {
		return deskOverviewLocal(cmd)
	}

	payload := map[string]any{}
	if app != "" {
		payload["app"] = app
	}
	raw, err := hubRaw(ctx, "overview", "get", payload)
	if err != nil {
		return err
	}
	if wantJSON {
		printJSON(raw)
		return nil
	}
	var resp wire.OverviewResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	fmt.Print(renderOverview(resp, app != ""))
	return nil
}

// renderOverview turns the aggregator shape into markdown.
//
// scoped is true when the user named an app (`mm overview kb`): the header is
// redundant — they know what they asked for — so it's dropped. The bare
// aggregate form (`mm overview`) always shows per-app headers and a provenance
// footer, because its whole value is knowing *which* apps answered: a lone
// section with no header reads as "this is everything" when it really means
// "this is the one app wired up so far".
func renderOverview(resp wire.OverviewResp, scoped bool) string {
	var b strings.Builder
	slugs := make([]string, 0, len(resp.Apps))
	for k := range resp.Apps {
		slugs = append(slugs, k)
	}
	sort.Strings(slugs)
	if len(slugs) == 0 {
		return "_No apps expose an overview yet._\n"
	}
	showHeader := !scoped || len(slugs) > 1
	for _, slug := range slugs {
		ov := resp.Apps[slug]
		if showHeader {
			fmt.Fprintf(&b, "# %s\n\n", slug)
		}
		if len(ov.Sections) == 0 {
			b.WriteString("_(empty)_\n\n")
			continue
		}
		for _, sec := range ov.Sections {
			fmt.Fprintf(&b, "## %s", sec.Label)
			if n := len(sec.Items); n > 0 {
				fmt.Fprintf(&b, " (%d)", n)
			}
			b.WriteString("\n\n")
			if len(sec.Items) == 0 {
				b.WriteString("_None._\n\n")
				continue
			}
			for _, it := range sec.Items {
				b.WriteString("- " + overviewLine(it) + "\n")
			}
			b.WriteString("\n")
		}
	}
	if !scoped {
		b.WriteString(provenanceFooter(len(slugs), "overview"))
	}
	return b.String()
}

// provenanceFooter tells the aggregate form's reader how many apps actually
// answered, so a short list doesn't masquerade as the whole environment. Apps
// only appear once they declare the capability in their Agent Card.
func provenanceFooter(n int, verb string) string {
	return fmt.Sprintf("_%d app%s surfacing — others haven't adopted %s yet (`mm cards`)._\n",
		n, plural(n), verb)
}

func overviewLine(it wire.OverviewItem) string {
	parts := []string{strings.TrimSpace(it.Title)}
	if it.Subtitle != "" {
		parts = append(parts, "— "+it.Subtitle)
	}
	if it.Count != nil {
		parts = append(parts, fmt.Sprintf("(%d)", *it.Count))
	}
	line := strings.Join(parts, " ")
	if it.ID != "" {
		line += fmt.Sprintf(" `%s`", it.ID)
	}
	return line
}

// ─── surface ────────────────────────────────────────────────────────────

func runSurface(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	app := ""
	if len(args) == 1 {
		app = strings.ToLower(args[0])
	}
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	limit, _ := cmd.Flags().GetInt("limit")

	if app == "desk" {
		return deskSurfaceLocal(cmd)
	}

	payload := map[string]any{}
	if app != "" {
		payload["app"] = app
	}
	if limit > 0 {
		payload["limit"] = limit
	}
	raw, err := hubRaw(ctx, "surface", "get", payload)
	if err != nil {
		return err
	}
	if wantJSON {
		printJSON(raw)
		return nil
	}
	var resp wire.SurfaceResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	fmt.Print(renderSurface(resp, app != ""))
	return nil
}

func renderSurface(resp wire.SurfaceResp, scoped bool) string {
	var b strings.Builder
	slugs := make([]string, 0, len(resp.Apps))
	for k := range resp.Apps {
		slugs = append(slugs, k)
	}
	sort.Strings(slugs)
	if len(slugs) == 0 {
		return "_Nothing surfacing right now._\n"
	}
	showHeader := !scoped || len(slugs) > 1
	for _, slug := range slugs {
		sf := resp.Apps[slug]
		if showHeader {
			fmt.Fprintf(&b, "# %s (%d)\n\n", slug, len(sf.Items))
		}
		if len(sf.Items) == 0 {
			b.WriteString("_Nothing surfacing._\n\n")
			continue
		}
		for _, it := range sf.Items {
			b.WriteString("- " + surfaceLine(it) + "\n")
		}
		b.WriteString("\n")
	}
	if !scoped {
		b.WriteString(provenanceFooter(len(slugs), "surface"))
	}
	return b.String()
}

func surfaceLine(it wire.SurfaceItem) string {
	var b strings.Builder
	if it.Kind != "" {
		fmt.Fprintf(&b, "[%s] ", it.Kind)
	}
	b.WriteString(strings.TrimSpace(it.Title))
	if it.Subtitle != "" {
		b.WriteString(" — " + it.Subtitle)
	}
	meta := []string{}
	if d := fmtDate(it.At); d != "" {
		meta = append(meta, d)
	}
	if it.ID != "" {
		meta = append(meta, "`"+it.ID+"`")
	}
	if len(meta) > 0 {
		b.WriteString(" · " + strings.Join(meta, " · "))
	}
	return b.String()
}

// ─── desk pull-mode (local agent) ───────────────────────────────────────

// deskOverviewLocal serves `mm overview desk` from the local agent — the
// desk catalogue is its registered projects (the hub can't reach the agent).
func deskOverviewLocal(cmd *cobra.Command) error {
	node, _ := cmd.Flags().GetString("node")
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
	if len(data.Projects) == 0 {
		fmt.Println("_No projects._")
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Projects (%d)\n\n", len(data.Projects))
	for _, p := range data.Projects {
		count := 0
		if p.ThreadCount != nil {
			count = *p.ThreadCount
		}
		fmt.Fprintf(&b, "- **%s** — %d thread%s `%s`\n", p.Label, count, plural(count), p.RootPath)
	}
	fmt.Print(b.String())
	return nil
}

// deskSurfaceLocal serves `mm surface desk` from the local agent's event log
// (open loops + recent activity) — the same data as bare `mm desk`.
func deskSurfaceLocal(cmd *cobra.Command) error {
	node, _ := cmd.Flags().GetString("node")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := mmhttp.New()
	resp, err := client.AgentFetch(cmd.Context(), node, "/api/events/overview", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /api/events/overview %d: %s", resp.StatusCode, truncString(string(body), 200))
	}
	if wantJSON {
		fmt.Println(string(body))
		return nil
	}
	var data wire.AgentDeskOverview
	if err := json.Unmarshal(body, &data); err != nil {
		return err
	}
	renderDeskOverview(data)
	return nil
}

// ─── shared helpers ─────────────────────────────────────────────────────

// hubRaw calls a hub mm-RPC and returns the unwrapped `data` as raw JSON, so
// the caller can both pretty-print it (--json) and decode it into a typed
// shape without a second request.
func hubRaw(ctx context.Context, feature, action string, payload map[string]any) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := mmhttp.New().Hub(ctx, feature, action, payload, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func printJSON(raw json.RawMessage) {
	var v any
	if json.Unmarshal(raw, &v) == nil {
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(out))
		return
	}
	fmt.Println(string(raw))
}

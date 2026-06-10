package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/apps"
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
	app := ""
	if len(args) == 1 {
		app = strings.ToLower(args[0])
	}
	// Desk is pull-mode — the hub can't reach the local agent.
	if app == "desk" {
		return deskOverviewLocal(cmd)
	}
	if app != "" {
		return overviewScoped(cmd, app)
	}

	// Aggregate: fan out across every app the hub can reach, then best-effort
	// stitch desk (pull-mode — the hub can't reach the local agent).
	ctx := cmd.Context()
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	node, _ := cmd.Flags().GetString("node")
	raw, err := hubRaw(ctx, "overview", "get", map[string]any{})
	if err != nil {
		return err
	}
	var resp wire.OverviewResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	if deskApp, ok := stitchDeskOverview(ctx, node); ok {
		if resp.Apps == nil {
			resp.Apps = map[string]wire.OverviewApp{}
		}
		resp.Apps["desk"] = deskApp
	}
	if wantJSON {
		printJSON(mustJSON(resp))
		return nil
	}
	fmt.Print(renderOverview(resp, false))
	return nil
}

// overviewScoped renders a single app's overview via the hub — the shared
// path behind `mm overview <app>` and per-app aliases (`kb tree` once the
// notebook-count drift is reconciled).
//
// The response is filtered to the requested app CLI-side: the hub aggregator
// currently ignores the `app` param and returns every live app, so a scoped
// command would otherwise leak other apps' content. Filtering here keeps the
// CLI correct regardless of whether the hub honours the param.
func overviewScoped(cmd *cobra.Command, app string) error {
	ctx := cmd.Context()
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	raw, err := hubRaw(ctx, "overview", "get", map[string]any{"app": app})
	if err != nil {
		return err
	}
	var resp wire.OverviewResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	ov, ok := resp.Apps[app]
	if !ok {
		return fmt.Errorf("%s doesn't expose an overview (run `mm cards` to see which apps do)", app)
	}
	filtered := wire.OverviewResp{Apps: map[string]wire.OverviewApp{app: ov}}
	if wantJSON {
		printJSON(mustJSON(filtered))
		return nil
	}
	fmt.Print(renderOverview(filtered, true))
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
	// Header shows the app's tagline (from its card/registry) so the first
	// glance answers "what is this?" — the orientation an agent needs before
	// it picks where to drill. Dropped only when the user already named the
	// app (scoped) and it's the sole result.
	showHeader := !scoped || len(slugs) > 1
	for _, slug := range slugs {
		ov := resp.Apps[slug]
		if showHeader {
			if g := appGloss(slug); g != "" {
				fmt.Fprintf(&b, "# %s — %s\n\n", slug, g)
			} else {
				fmt.Fprintf(&b, "# %s\n\n", slug)
			}
		}
		if len(ov.Sections) == 0 {
			b.WriteString("_(empty)_\n\n")
		}
		for _, sec := range ov.Sections {
			fmt.Fprintf(&b, "## %s", sec.Label)
			// Auto-append the item count, unless the app's label already ends
			// in a parenthetical (it's stated its own count/scope — don't double up).
			if n := len(sec.Items); n > 0 && !strings.HasSuffix(sec.Label, ")") {
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
		// The road out: the command to go deeper. Names are the handle (they
		// resolve), so an agent never needs the raw id from the human view.
		if h := overviewDrillHint(slug); h != "" {
			b.WriteString(h + "\n\n")
		}
	}
	if !scoped {
		b.WriteString(provenanceFooter(len(slugs), "overview"))
	}
	return b.String()
}

// appGloss is the one-line "what is this app" tagline, taken from the registry
// description with its redundant "Name — " prefix stripped (the slug is
// already the header). Empty for apps the CLI doesn't know yet.
func appGloss(slug string) string {
	if slug == "desk" {
		return "what you're working on (projects + threads)"
	}
	e, ok := apps.Registry[slug]
	if !ok {
		return ""
	}
	if i := strings.Index(e.Description, " — "); i >= 0 {
		return e.Description[i+len(" — "):]
	}
	return e.Description
}

// overviewDrillHint is the road out of an app's overview — the command(s) to
// go deeper. The CLI already has bespoke wrappers for kb/crm and a desk
// command, so it can name the precise verb; everything else gets the
// always-valid universal verbs. Handles are names, not ids.
func overviewDrillHint(slug string) string {
	switch slug {
	case "kb":
		return "→ `mm kb peek \"<name>\"` (summary) · `mm kb tree \"<name>\"` (docs) · `mm kb find \"<q>\"`"
	case "crm":
		return "→ `mm crm context \"<name>\"` · `mm crm find \"<q>\"`"
	case "desk":
		return "→ `mm desk show` · `mm project detail <name>` (files)"
	default:
		return fmt.Sprintf("→ `mm %s find \"<q>\"` · `mm %s ask \"...\"`", slug, slug)
	}
}

// provenanceFooter tells the aggregate form's reader how many apps actually
// answered, so a short list doesn't masquerade as the whole environment. Apps
// only appear once they declare the capability in their Agent Card.
func provenanceFooter(n int, verb string) string {
	return fmt.Sprintf("%d app%s · not every app exposes %s yet, and desk shows only when its local agent is reachable (`mm cards`).\n",
		n, plural(n), verb)
}

// overviewLine renders one catalogue item: name, gloss, count. No markdown
// bold (token noise for an LLM; the bullet already delimits the name) and no
// raw id — the name is the nav handle, the machine id lives in `--json`.
// The bare count is "this item's magnitude"; its unit is implied by the
// section (Notebooks→docs, Active projects→threads). Apps whose count isn't
// self-evident should label it in the subtitle instead.
func overviewLine(it wire.OverviewItem) string {
	line := strings.TrimSpace(it.Title)
	if it.Subtitle != "" {
		line += " — " + it.Subtitle
	}
	if it.Value != "" {
		line += " · " + it.Value
	}
	if it.Count != nil {
		line += fmt.Sprintf(" (%d)", *it.Count)
	}
	return line
}

// ─── surface ────────────────────────────────────────────────────────────

func runSurface(cmd *cobra.Command, args []string) error {
	app := ""
	if len(args) == 1 {
		app = strings.ToLower(args[0])
	}
	limit, _ := cmd.Flags().GetInt("limit")

	if app == "desk" {
		return deskSurfaceLocal(cmd)
	}
	if app != "" {
		return surfaceScoped(cmd, app, limit)
	}

	// Aggregate: fan out across every app the hub can reach, then best-effort
	// stitch desk (pull-mode — the hub can't reach the local agent).
	ctx := cmd.Context()
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	node, _ := cmd.Flags().GetString("node")
	payload := map[string]any{}
	if limit > 0 {
		payload["limit"] = limit
	}
	raw, err := hubRaw(ctx, "surface", "get", payload)
	if err != nil {
		return err
	}
	var resp wire.SurfaceResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	if deskApp, ok := stitchDeskSurface(ctx, node); ok {
		if resp.Apps == nil {
			resp.Apps = map[string]wire.SurfaceApp{}
		}
		resp.Apps["desk"] = deskApp
	}
	if wantJSON {
		printJSON(mustJSON(resp))
		return nil
	}
	fmt.Print(renderSurface(resp, false))
	return nil
}

// surfaceScoped renders a single app's surface via the hub — the shared path
// behind `mm surface <app>` and per-app aliases like `mm crm surface`. As with
// overviewScoped, the response is filtered to the requested app CLI-side
// because the hub aggregator currently ignores the `app` param.
func surfaceScoped(cmd *cobra.Command, app string, limit int) error {
	ctx := cmd.Context()
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	payload := map[string]any{"app": app}
	if limit > 0 {
		payload["limit"] = limit
	}
	raw, err := hubRaw(ctx, "surface", "get", payload)
	if err != nil {
		return err
	}
	var resp wire.SurfaceResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	sf, ok := resp.Apps[app]
	if !ok {
		return fmt.Errorf("%s doesn't expose a surface (run `mm cards` to see which apps do)", app)
	}
	filtered := wire.SurfaceResp{Apps: map[string]wire.SurfaceApp{app: sf}}
	if wantJSON {
		printJSON(mustJSON(filtered))
		return nil
	}
	fmt.Print(renderSurface(filtered, true))
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
		return "Nothing surfacing right now.\n"
	}
	showHeader := !scoped || len(slugs) > 1
	for _, slug := range slugs {
		sf := resp.Apps[slug]
		if showHeader {
			if g := appGloss(slug); g != "" {
				fmt.Fprintf(&b, "# %s — %s (%d)\n\n", slug, g, len(sf.Items))
			} else {
				fmt.Fprintf(&b, "# %s (%d)\n\n", slug, len(sf.Items))
			}
		}
		if len(sf.Items) == 0 {
			b.WriteString("Nothing surfacing.\n\n")
		} else {
			for _, it := range sf.Items {
				b.WriteString("- " + surfaceLine(it) + "\n")
			}
			b.WriteString("\n")
		}
		if h := surfaceDrillHint(slug); h != "" {
			b.WriteString(h + "\n\n")
		}
	}
	if !scoped {
		b.WriteString(provenanceFooter(len(slugs), "surface"))
	}
	return b.String()
}

// surfaceLine renders one activity item: [kind] title — gloss · date. The
// kind tag and date are the load-bearing signal for decaying activity; the
// raw id is dropped (it lives in `--json`) — to act on an item the agent uses
// the name/title via the drill-in road, same as overview.
func surfaceLine(it wire.SurfaceItem) string {
	var b strings.Builder
	if it.Kind != "" {
		fmt.Fprintf(&b, "[%s] ", it.Kind)
	}
	b.WriteString(strings.TrimSpace(it.Title))
	if it.Subtitle != "" {
		b.WriteString(" — " + it.Subtitle)
	}
	if d := fmtDate(it.At); d != "" {
		b.WriteString(" · " + d)
	}
	return b.String()
}

// surfaceDrillHint is the road out of a surface item — how to act on what's
// surfacing (read it, open the thread, follow up), distinct from overview's
// catalogue-navigation roads.
func surfaceDrillHint(slug string) string {
	switch slug {
	case "crm":
		return "→ `mm crm context \"<name>\"` · `mm crm log \"...\"` (follow up)"
	case "kb":
		return "→ `mm kb read \"<title>\"` · `mm kb peek \"<title>\"`"
	case "desk":
		return "→ `mm desk` (threads) · `mm desk search \"<title>\"`"
	default:
		return fmt.Sprintf("→ `mm %s ask \"...\"`", slug)
	}
}

// ─── desk pull-mode (local agent) ───────────────────────────────────────

// deskOverviewLocal serves `mm overview desk` from the local agent — the desk
// catalogue is its registered projects (the hub can't reach the agent). Shares
// the single project-list renderer with `mm desk projects` / `mm project list`.
func deskOverviewLocal(cmd *cobra.Command) error {
	node, _ := cmd.Flags().GetString("node")
	return listProjects(cmd, node)
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

// ─── desk stitch (best-effort, for the aggregate views) ─────────────────

// deskStitchTimeout bounds the local-agent call when stitching desk into an
// aggregate overview/surface. Desk is pull-mode and its agent is only
// guaranteed up on the user's own always-on node (e.g. Joe's m4) — for anyone
// else it may be off, so a slow/absent agent must never stall the panorama.
// A connection-refused returns immediately; this caps the hang case.
const deskStitchTimeout = 2 * time.Second

// stitchDeskOverview fetches the local agent's projects as a desk OverviewApp.
// Best-effort: returns ok=false (and the aggregate simply omits desk) if the
// agent is unreachable, errors, or has nothing.
func stitchDeskOverview(ctx context.Context, node string) (wire.OverviewApp, bool) {
	ctx, cancel := context.WithTimeout(ctx, deskStitchTimeout)
	defer cancel()
	resp, err := mmhttp.New().AgentFetch(ctx, node, "/api/projects", nil)
	if err != nil {
		return wire.OverviewApp{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return wire.OverviewApp{}, false
	}
	var data wire.AgentProjectsListResp
	if json.NewDecoder(resp.Body).Decode(&data) != nil || len(data.Projects) == 0 {
		return wire.OverviewApp{}, false
	}
	// In the panorama, desk is "what you're working on" — so show only active
	// projects (threads > 0), most-active first, capped. The full registered
	// list (incl. dormant repos) stays in `mm overview desk`. No path here:
	// it's noise at orientation time; the name is the handle.
	active := make([]wire.AgentProject, 0, len(data.Projects))
	for _, p := range data.Projects {
		if p.ThreadCount != nil && *p.ThreadCount > 0 {
			active = append(active, p)
		}
	}
	sort.Slice(active, func(i, j int) bool { return *active[i].ThreadCount > *active[j].ThreadCount })
	if len(active) == 0 {
		return wire.OverviewApp{}, false
	}
	const deskCap = 8
	capped := active
	if len(capped) > deskCap {
		capped = capped[:deskCap]
	}
	items := make([]wire.OverviewItem, 0, len(capped))
	for _, p := range capped {
		c := *p.ThreadCount
		// Subtitle is the desk agent's project gloss once it ships one
		// (empty until then — the line just shows name + thread count).
		items = append(items, wire.OverviewItem{ID: p.ID, Title: p.Label, Subtitle: p.Description, Count: &c})
	}
	label := "Active projects"
	if len(active) > len(capped) {
		label = fmt.Sprintf("Active projects (top %d of %d — all: `mm overview desk`)", len(capped), len(active))
	}
	return wire.OverviewApp{
		Sections: []wire.OverviewSection{{Label: label, Items: items}},
		Meta:     map[string]any{"app": "desk", "pull_mode": true},
	}, true
}

// stitchDeskSurface maps the local agent's event log into a desk SurfaceApp:
// open loops lead (they're the attention items), then salient events, capped
// per project so one busy project can't flood the panorama. Best-effort.
func stitchDeskSurface(ctx context.Context, node string) (wire.SurfaceApp, bool) {
	ctx, cancel := context.WithTimeout(ctx, deskStitchTimeout)
	defer cancel()
	resp, err := mmhttp.New().AgentFetch(ctx, node, "/api/events/overview", nil)
	if err != nil {
		return wire.SurfaceApp{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return wire.SurfaceApp{}, false
	}
	var data wire.AgentDeskOverview
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		return wire.SurfaceApp{}, false
	}
	items := make([]wire.SurfaceItem, 0, len(data.OpenLoops))
	for _, e := range data.OpenLoops {
		items = append(items, deskEventToSurface(e, "open_loop"))
	}
	const perProject = 3
	for _, g := range data.Groups {
		n := 0
		for _, e := range g.Events {
			if n >= perProject {
				break
			}
			// Surface is "what's happening / needs attention" — a resolved
			// loop is done, not happening, so it's noise here. Open loops
			// (which lead, above) are never resolved by definition.
			if e.Kind == "resolved" {
				continue
			}
			items = append(items, deskEventToSurface(e, e.Kind))
			n++
		}
	}
	if len(items) == 0 {
		return wire.SurfaceApp{}, false
	}
	return wire.SurfaceApp{
		Items: items,
		Meta:  map[string]any{"app": "desk", "total": len(items), "pull_mode": true},
	}, true
}

func deskEventToSurface(e wire.AgentDeskEvent, kind string) wire.SurfaceItem {
	if kind == "" {
		kind = "event"
	}
	at := ""
	if e.TS > 0 {
		at = time.UnixMilli(e.TS).UTC().Format(time.RFC3339)
	}
	return wire.SurfaceItem{ID: e.ID, Title: e.Summary, Subtitle: e.ThreadTitle, Kind: kind, At: at}
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

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/apps"
	"mm-cli/internal/auth"
	"mm-cli/internal/config"
	"mm-cli/internal/db"
	mmhttp "mm-cli/internal/http"
	"mm-cli/internal/update"
	"mm-cli/internal/version"
)

// probeTimeout bounds every network/DB probe `mm status` makes. Status is a
// glance command — a slow hub or an unreachable DB degrades to "unreachable"
// rather than making the user wait.
const probeTimeout = 4 * time.Second

// ── report shapes (also the --json contract) ───────────────────────────

type statusUser struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	ID        string `json:"id"`
	Prefix    string `json:"tokenPrefix"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type statusCLI struct {
	Version         string `json:"version"`
	Commit          string `json:"commit,omitempty"`
	BuildDate       string `json:"buildDate,omitempty"`
	Binary          string `json:"binary,omitempty"`
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// statusEndpoint is one reachability probe: what we tried, whether it
// answered, and how long it took.
type statusEndpoint struct {
	Name    string `json:"name"`
	Target  string `json:"target"`
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Latency int64  `json:"latencyMs,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

type statusInstance struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"isDefault"`
}

type statusApp struct {
	Slug        string           `json:"slug"`
	Name        string           `json:"name,omitempty"`
	Description string           `json:"description,omitempty"`
	Entitled    bool             `json:"entitled"`
	Typed       bool             `json:"typedVerbs"`
	Instances   []statusInstance `json:"instances,omitempty"`
}

type statusNode struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type statusReport struct {
	Authenticated bool             `json:"authenticated"`
	User          *statusUser      `json:"user,omitempty"`
	CLI           statusCLI        `json:"cli"`
	Endpoints     []statusEndpoint `json:"endpoints"`
	Apps          []statusApp      `json:"apps,omitempty"`
	Nodes         []statusNode     `json:"nodes,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
}

// NewStatusCmd builds `mm status` — one screen answering "who am I, what can
// I reach, and what's on the other end". Everything on it is probed live;
// nothing is hardcoded.
func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Who you are, what's reachable, and which apps you can drive",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rep := collectStatus(cmd.Context())
			wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
			if wantJSON {
				out, err := json.MarshalIndent(rep, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal JSON: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			printStatus(cmd.OutOrStdout(), rep)
			return nil
		},
	}
}

// ── collection ─────────────────────────────────────────────────────────

func collectStatus(ctx context.Context) *statusReport {
	cfg := config.Load()
	state, _ := auth.Load()

	rep := &statusReport{
		Authenticated: state != nil,
		CLI: statusCLI{
			Version:   version.Version,
			Commit:    version.Commit,
			BuildDate: version.BuildDate,
			Binary:    binaryPath(),
		},
	}
	if state != nil {
		rep.User = &statusUser{
			Name: state.UserName, Email: state.UserEmail,
			ID: state.UserID, Prefix: state.Prefix, CreatedAt: state.CreatedAt,
		}
	}

	// Every probe is independent — run them together so status costs one
	// round-trip of wall clock, not five.
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		hubEP     statusEndpoint
		agentEP   statusEndpoint
		dbEP      statusEndpoint
		entitled  []hubApp
		instances []instanceItem
		instErr   error
	)
	addWarning := func(s string) {
		mu.Lock()
		rep.Warnings = append(rep.Warnings, s)
		mu.Unlock()
	}

	wg.Add(3)
	go func() {
		defer wg.Done()
		hubEP, entitled = probeHub(ctx, cfg, state)
	}()
	go func() {
		defer wg.Done()
		agentEP = probeLocalAgent(ctx, cfg)
	}()
	go func() {
		defer wg.Done()
		dbEP = probeDatabase(ctx, cfg)
	}()
	if state != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, probeTimeout)
			defer cancel()
			instances, instErr = listInstances(pctx, mmhttp.New(), "")
		}()
	}
	// The update check is best-effort — a missing dist endpoint must never
	// make `mm status` look broken.
	wg.Add(1)
	go func() {
		defer wg.Done()
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		if res, err := update.Check(pctx); err == nil {
			mu.Lock()
			rep.CLI.Latest = res.Latest
			rep.CLI.UpdateAvailable = res.Newer
			mu.Unlock()
		}
	}()
	wg.Wait()

	rep.Endpoints = []statusEndpoint{hubEP, agentEP, dbEP}
	if instErr != nil && state != nil {
		addWarning("instance list unavailable: " + statusFirstLine(instErr.Error()))
	}
	rep.Apps, rep.Nodes = buildAppView(entitled, instances)
	if state != nil && !hubEP.OK {
		addWarning("hub unreachable — app list below is the CLI's built-in registry, not your entitlements")
	}
	return rep
}

// hubApp is the slice of `apps.list` status cares about.
type hubApp struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// probeHub doubles as the reachability check and the entitlement fetch:
// `apps.list` is the cheapest authenticated call that answers both.
func probeHub(ctx context.Context, cfg *config.Config, state *auth.State) (statusEndpoint, []hubApp) {
	ep := statusEndpoint{Name: "hub", Target: cfg.HubURL}
	if state == nil {
		ep.Skipped = true
		ep.Detail = "not authenticated"
		ep.Hint = "mm login"
		return ep, nil
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var resp struct {
		Apps []hubApp `json:"apps"`
	}
	start := time.Now()
	err := mmhttp.New().Hub(pctx, "apps", "list", map[string]any{}, &resp)
	ep.Latency = time.Since(start).Milliseconds()
	if err != nil {
		ep.Detail = statusFirstLine(err.Error())
		return ep, nil
	}
	ep.OK = true
	ep.Detail = fmt.Sprintf("%d apps entitled", len(resp.Apps))
	return ep, resp.Apps
}

// probeLocalAgent hits the local agent's /api/health — the surface behind
// `mm desk`, `mm project` and `mm run`.
func probeLocalAgent(ctx context.Context, cfg *config.Config) statusEndpoint {
	ep := statusEndpoint{Name: "local agent", Target: cfg.LocalAgentURL, Hint: "mm desk · mm project · mm run"}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	start := time.Now()
	resp, err := mmhttp.New().AgentFetch(pctx, "", "/api/health", nil)
	ep.Latency = time.Since(start).Milliseconds()
	if err != nil {
		ep.Detail = "not running"
		return ep
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		ep.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return ep
	}
	var health struct {
		Version  string `json:"version"`
		Sessions int    `json:"sessions"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&health)
	ep.OK = true
	parts := []string{}
	if health.Version != "" {
		parts = append(parts, "v"+health.Version)
	}
	if health.Sessions > 0 {
		parts = append(parts, fmt.Sprintf("%d live session(s)", health.Sessions))
	}
	ep.Detail = strings.Join(parts, ", ")
	return ep
}

// probeDatabase reports whether the direct-Postgres admin surface is usable
// from this box. Unset is a normal state, not a failure — most machines run
// user verbs only.
func probeDatabase(ctx context.Context, cfg *config.Config) statusEndpoint {
	ep := statusEndpoint{Name: "admin db", Hint: "mm admin …"}
	if cfg.DatabaseURL == "" {
		ep.Skipped = true
		ep.Target = "not configured"
		ep.Detail = "set MM_DATABASE_URL in ~/.mm/.env for admin verbs"
		return ep
	}
	ep.Target = redactDSN(cfg.DatabaseURL)
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	start := time.Now()
	pool, err := db.Pool(pctx)
	if err == nil {
		err = pool.Ping(pctx)
	}
	ep.Latency = time.Since(start).Milliseconds()
	if err != nil {
		ep.Detail = statusFirstLine(err.Error())
		return ep
	}
	ep.OK = true
	return ep
}

// buildAppView merges three sources into one list: the hub's entitlements
// (what this token may touch), the CLI's registry (what has typed verbs
// here), and instance.list (which instance a bare command lands on). Node
// instances are split out — they're machines, not apps.
func buildAppView(entitled []hubApp, instances []instanceItem) ([]statusApp, []statusNode) {
	byInstance := map[string][]statusInstance{}
	var nodes []statusNode
	for _, it := range instances {
		if it.AppSlug == "desk" || it.AppSlug == "agent" {
			nodes = append(nodes, statusNode{Name: it.Name, URL: it.URL})
			continue
		}
		byInstance[it.AppSlug] = append(byInstance[it.AppSlug],
			statusInstance{ID: it.ID, Name: it.Name, Default: it.IsPrimary})
	}
	for slug := range byInstance {
		rows := byInstance[slug]
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Default && !rows[j].Default })
		byInstance[slug] = rows
	}
	sort.Slice(nodes, func(i, j int) bool {
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})

	seen := map[string]*statusApp{}
	var out []statusApp
	add := func(a statusApp) {
		out = append(out, a)
		seen[a.Slug] = &out[len(out)-1]
	}
	for _, a := range entitled {
		if a.Slug == "desk" || a.Slug == "agent" {
			continue
		}
		app := statusApp{Slug: a.Slug, Name: a.Name, Entitled: true, Instances: byInstance[a.Slug]}
		if reg, ok := apps.Registry[a.Slug]; ok {
			app.Typed = true
			app.Description = reg.Description
		}
		add(app)
	}
	// Registry apps the hub didn't return: either the hub was unreachable or
	// the token genuinely lacks the grant. Either way, show them — with the
	// entitlement flag off — so the list never silently shrinks.
	regSlugs := make([]string, 0, len(apps.Registry))
	for slug := range apps.Registry {
		regSlugs = append(regSlugs, slug)
	}
	sort.Strings(regSlugs)
	for _, slug := range regSlugs {
		if _, ok := seen[slug]; ok {
			continue
		}
		add(statusApp{Slug: slug, Typed: true, Description: apps.Registry[slug].Description,
			Instances: byInstance[slug]})
	}
	// Typed-verb apps first (they're what you can actually drive), then the
	// generic-dispatch remainder, each alphabetically.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Typed != out[j].Typed {
			return out[i].Typed
		}
		return out[i].Slug < out[j].Slug
	})
	return out, nodes
}

// ── rendering ──────────────────────────────────────────────────────────

func printStatus(w interface{ Write([]byte) (int, error) }, rep *statusReport) {
	p := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }

	head := "mm " + rep.CLI.Version
	if rep.CLI.Commit != "" && rep.CLI.Commit != "unknown" {
		head += " (" + rep.CLI.Commit + ")"
	}
	switch {
	case rep.CLI.Version == "dev":
		head += "  ·  dev build (go run)"
	case rep.CLI.UpdateAvailable:
		head += fmt.Sprintf("  ·  update available: %s → run `mm update`", rep.CLI.Latest)
	case rep.CLI.Latest != "":
		head += "  ·  up to date"
	}
	p("%s", head)
	p("")

	if !rep.Authenticated {
		p("Not authenticated — run `mm login` to link this machine.")
		p("")
		printEndpoints(p, rep)
		p("")
		p("Once logged in, `mm status` lists the apps and instances your token reaches.")
		return
	}

	u := rep.User
	p("Account")
	p("  %s <%s>", u.Name, u.Email)
	created := u.CreatedAt
	if len(created) >= 10 {
		created = created[:10]
	}
	line := fmt.Sprintf("  token %s…", u.Prefix)
	if created != "" {
		line += fmt.Sprintf(" (created %s)", created)
	}
	if u.ID != "" {
		line += fmt.Sprintf("  ·  user %s", u.ID)
	}
	p("%s", line)
	p("")

	printEndpoints(p, rep)
	p("")

	// Apps — typed-verb apps get a line each with their instances; the rest
	// collapse to one line, since all you can do with them is `mm v2`.
	// "Reachable" counts grants only: a registry app the token can't touch is
	// still listed (so the absence is visible) but must never inflate the
	// count of what you can actually drive.
	typed, reachable, ungranted := 0, 0, 0
	generic := []string{}
	for _, a := range rep.Apps {
		switch {
		case !a.Entitled:
			ungranted++
		case a.Typed:
			typed++
			reachable++
		default:
			reachable++
		}
		if !a.Typed {
			generic = append(generic, a.Slug)
		}
	}
	appsHead := fmt.Sprintf("Apps (%d reachable · %d with typed verbs", reachable, typed)
	if ungranted > 0 {
		appsHead += fmt.Sprintf(" · %d not granted", ungranted)
	}
	p("%s)", appsHead)
	width := 0
	for _, a := range rep.Apps {
		if a.Typed && len(a.Slug) > width {
			width = len(a.Slug)
		}
	}
	for _, a := range rep.Apps {
		if !a.Typed {
			continue
		}
		marker := "  "
		suffix := ""
		if !a.Entitled {
			marker = "! "
			suffix = "   (no grant on this token)"
		}
		p("%s%s  %s%s", marker, padRight(a.Slug, width), a.Description, suffix)
		if len(a.Instances) == 0 {
			continue
		}
		names := make([]string, 0, len(a.Instances))
		for _, in := range a.Instances {
			n := in.Name
			if in.Default && len(a.Instances) > 1 {
				n += " ●"
			}
			names = append(names, n)
		}
		p("  %s  └ %s", padRight("", width), strings.Join(names, " · "))
	}
	if len(generic) > 0 {
		p("")
		p("  Also granted: %s", strings.Join(generic, ", "))
		p("  → mm v2 <app> <feature.action>  ·  mm cards <app> for the surface")
	}

	if len(rep.Nodes) > 0 {
		p("")
		p("Agent nodes (%d)", len(rep.Nodes))
		nw := 0
		for _, n := range rep.Nodes {
			if len(n.Name) > nw {
				nw = len(n.Name)
			}
		}
		for _, n := range rep.Nodes {
			p("  %s  %s", padRight(n.Name, nw), n.URL)
		}
		p("  → mm desk send \"…\" --node <name>")
	}

	if len(rep.Warnings) > 0 {
		fmt.Fprintln(os.Stderr)
		for _, warn := range rep.Warnings {
			fmt.Fprintf(os.Stderr, "! %s\n", warn)
		}
	}
}

func printEndpoints(p func(string, ...any), rep *statusReport) {
	p("Reach")
	width := 0
	for _, e := range rep.Endpoints {
		if len(e.Name) > width {
			width = len(e.Name)
		}
	}
	tw := 0
	for _, e := range rep.Endpoints {
		if len(e.Target) > tw {
			tw = len(e.Target)
		}
	}
	for _, e := range rep.Endpoints {
		mark := "✗"
		switch {
		case e.OK:
			mark = "✓"
		case e.Skipped:
			mark = "–"
		}
		row := fmt.Sprintf("  %s %s  %s", mark, padRight(e.Name, width), padRight(e.Target, tw))
		bits := []string{}
		if e.OK && e.Latency > 0 {
			bits = append(bits, fmt.Sprintf("%dms", e.Latency))
		}
		if e.Detail != "" {
			bits = append(bits, e.Detail)
		}
		if len(bits) > 0 {
			row += "  " + strings.Join(bits, " · ")
		}
		p("%s", strings.TrimRight(row, " "))
	}
}

// ── helpers ────────────────────────────────────────────────────────────

// redactDSN renders a Postgres URL as user@host/db — never the password.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		return "(configured)"
	}
	name := strings.TrimPrefix(u.Path, "/")
	out := u.Host
	if user := u.User.Username(); user != "" {
		out = user + "@" + out
	}
	if name != "" {
		out += "/" + name
	}
	return out
}

func statusFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func binaryPath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}

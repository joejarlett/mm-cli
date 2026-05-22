// Package admin holds the hub-DB-backed admin subcommands.
// Lives under `mm admin <verb>` (renamed from the TS top-level slots per
// specs/go-port/06-improvements.md item #8).
package admin

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"mm-cli/internal/db"
)

// NewAdminCmd builds the `mm admin` tree.
func NewAdminCmd() *cobra.Command {
	c := &cobra.Command{Use: "admin", Short: "Hub admin verbs (sql / apps / health / errors)"}
	c.AddCommand(newSqlCmd(), newAppsCmd(), newAppCmd(), newHealthCmd(), newErrorsCmd(), newErrorCmd())
	return c
}

func newSqlCmd() *cobra.Command {
	return &cobra.Command{Use: "sql [query]", Short: "Run arbitrary SQL", Args: cobra.ExactArgs(1), RunE: runSql}
}

func newAppsCmd() *cobra.Command {
	return &cobra.Command{Use: "apps", Short: "List all apps registered with the hub", RunE: runApps}
}

func newAppCmd() *cobra.Command {
	return &cobra.Command{Use: "app [slug] [enable|disable]", Short: "Inspect or toggle an app", Args: cobra.RangeArgs(1, 2), RunE: runApp}
}

func newHealthCmd() *cobra.Command {
	return &cobra.Command{Use: "health", Short: "Quick hub stats", RunE: runHealth}
}

func newErrorsCmd() *cobra.Command {
	c := &cobra.Command{Use: "errors", Short: "List recent errors", RunE: runErrors}
	c.Flags().String("since", "", "Window (e.g. 24h, 7d). Default: 7d")
	c.Flags().Int("limit", 50, "Max rows")
	c.Flags().String("status", "", "Filter: new|triaged|resolved|wontfix|ignored")
	c.Flags().String("app", "", "Filter by app slug")
	c.Flags().String("level", "error", "Filter by level (default error)")
	c.Flags().String("priority", "", "Only high-priority")
	return c
}

func newErrorCmd() *cobra.Command {
	c := &cobra.Command{Use: "error [fingerprint] [<status>]", Short: "Inspect / triage one error", Args: cobra.RangeArgs(1, 2), RunE: runError}
	c.Flags().String("note", "", "Append a triage note")
	c.Flags().String("priority", "", "high|normal")
	c.Flags().String("log-quality", "", "ok|missing-stack|…")
	return c
}

// ─── helpers ───────────────────────────────────────────────────────────

func runSql(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	rows, err := pool.Query(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := rowFields(rows)
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return err
		}
		row := map[string]any{}
		for i, c := range cols {
			row[c] = vals[i]
		}
		out = append(out, row)
	}
	if wantJSON {
		j, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	renderMD(cols, out)
	return nil
}

func runApps(cmd *cobra.Command, _ []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	rows, err := pool.Query(cmd.Context(), `
		SELECT a.slug, a.name, a.enabled, a.listed, a.sort_order,
			COALESCE((SELECT array_agg(al.label_slug) FROM app_label al WHERE al.app_slug = a.slug), ARRAY[]::text[]) AS labels,
			a.features
		FROM app a
		ORDER BY a.sort_order, a.slug`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := rowFields(rows)
	data := []map[string]any{}
	for rows.Next() {
		vals, _ := rows.Values()
		row := map[string]any{}
		for i, c := range cols {
			row[c] = vals[i]
		}
		data = append(data, row)
	}
	if wantJSON {
		j, _ := json.MarshalIndent(data, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	fmt.Printf("# Apps (%d)\n\n", len(data))
	renderMD([]string{"slug", "name", "enabled", "listed", "sort_order", "labels", "features"}, data)
	return nil
}

func runApp(cmd *cobra.Command, args []string) error {
	slug := args[0]
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	if len(args) == 2 && (args[1] == "enable" || args[1] == "disable") {
		next := args[1] == "enable"
		_, err := pool.Exec(cmd.Context(), `UPDATE app SET enabled = $1 WHERE slug = $2`, next, slug)
		if err != nil {
			return err
		}
		fmt.Printf("✓ %s → enabled=%v\n", slug, next)
		return nil
	}
	row := pool.QueryRow(cmd.Context(), `SELECT slug, name, enabled, listed, url, caption, agent_description, features
		FROM app WHERE slug = $1`, slug)
	var a struct {
		Slug             string
		Name             string
		Enabled          bool
		Listed           bool
		URL              string
		Caption          *string
		AgentDescription *string
		Features         []string
	}
	if err := row.Scan(&a.Slug, &a.Name, &a.Enabled, &a.Listed, &a.URL, &a.Caption, &a.AgentDescription, &a.Features); err != nil {
		return fmt.Errorf("App not found: %s", slug)
	}
	if wantJSON {
		j, _ := json.MarshalIndent(a, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	fmt.Printf("# %s (%s)\n\n", a.Name, a.Slug)
	fmt.Printf("- enabled: %v · listed: %v\n", a.Enabled, a.Listed)
	fmt.Printf("- url: %s\n", a.URL)
	if a.Caption != nil && *a.Caption != "" {
		fmt.Printf("- caption: %s\n", *a.Caption)
	}
	if len(a.Features) > 0 {
		fmt.Printf("- features: %s\n", strings.Join(a.Features, ", "))
	}
	if a.AgentDescription != nil && *a.AgentDescription != "" {
		fmt.Printf("\n## Agent description\n\n%s\n", *a.AgentDescription)
	}
	return nil
}

func runHealth(cmd *cobra.Command, _ []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	type kv struct{ Metric, Value string }
	queries := []struct{ metric, sql string }{
		{"users", `SELECT COUNT(*)::int FROM "user"`},
		{"apps", `SELECT COUNT(*)::int FROM app`},
		{"errors (24h)", `SELECT COUNT(*)::int FROM error WHERE last_seen > NOW() - INTERVAL '24 hours'`},
		{"feedback (new)", `SELECT COUNT(*)::int FROM feedback WHERE status = 'new'`},
		{"digest (24h)", `SELECT COUNT(*)::int FROM digest WHERE occurred_at > NOW() - INTERVAL '24 hours'`},
	}
	out := []kv{}
	for _, q := range queries {
		var n int
		if err := pool.QueryRow(cmd.Context(), q.sql).Scan(&n); err != nil {
			return err
		}
		out = append(out, kv{q.metric, fmt.Sprint(n)})
	}
	var lastDigest *time.Time
	_ = pool.QueryRow(cmd.Context(), `SELECT MAX(occurred_at) FROM digest`).Scan(&lastDigest)
	last := "(none)"
	if lastDigest != nil {
		last = lastDigest.Format("2006-01-02 15:04")
	}
	out = append(out, kv{"last digest", last})
	if wantJSON {
		j, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	fmt.Println("# Hub health\n")
	rows := []map[string]any{}
	for _, r := range out {
		rows = append(rows, map[string]any{"metric": r.Metric, "value": r.Value})
	}
	renderMD([]string{"metric", "value"}, rows)
	return nil
}

var sinceRe = regexp.MustCompile(`^(\d+)([smhd])$`)

func runErrors(cmd *cobra.Command, _ []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	since := parseSince(getString(cmd, "since"))
	if since.IsZero() {
		since = time.Now().Add(-7 * 24 * time.Hour)
	}
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 50
	}
	level := getString(cmd, "level")
	if level == "" {
		level = "error"
	}
	app := getString(cmd, "app")
	status := getString(cmd, "status")
	priority := getString(cmd, "priority")

	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	conds := []string{"last_seen > $1", "level = $2"}
	args := []any{since, level}
	i := 3
	if status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", i))
		args = append(args, status)
		i++
	}
	if app != "" {
		conds = append(conds, fmt.Sprintf("app_slug = $%d", i))
		args = append(args, app)
		i++
	}
	if priority != "" {
		conds = append(conds, fmt.Sprintf("priority = $%d", i))
		args = append(args, priority)
		i++
	}
	query := `SELECT fingerprint, app_slug, surface, level, status, priority, count, last_seen, message
		FROM error
		WHERE ` + strings.Join(conds, " AND ") + `
		ORDER BY (priority = 'high') DESC, last_seen DESC
		LIMIT $` + strconv.Itoa(i)
	args = append(args, limit)

	rows, err := pool.Query(cmd.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := rowFields(rows)
	out := []map[string]any{}
	for rows.Next() {
		vals, _ := rows.Values()
		row := map[string]any{}
		for j, c := range cols {
			row[c] = vals[j]
		}
		out = append(out, row)
	}
	if wantJSON {
		j, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	fmt.Printf("# Errors (showing %d, since %s)\n\n", len(out), since.Format("2006-01-02 15:04"))
	renderMD(cols, out)
	if len(out) > 0 {
		fmt.Println("\n→ `mm admin error <fingerprint>` for full detail")
	}
	return nil
}

func runError(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	fp := args[0]
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	if len(args) == 2 {
		newStatus := args[1]
		valid := map[string]bool{"new": true, "triaged": true, "resolved": true, "wontfix": true, "ignored": true}
		if !valid[newStatus] {
			return fmt.Errorf("Invalid status. One of: new, triaged, resolved, wontfix, ignored")
		}
		note, _ := cmd.Flags().GetString("note")
		priority := getString(cmd, "priority")
		logQuality := getString(cmd, "log-quality")
		setParts := []string{"status = $1"}
		argsList := []any{newStatus}
		i := 2
		if priority != "" {
			setParts = append(setParts, fmt.Sprintf("priority = $%d", i))
			argsList = append(argsList, priority)
			i++
		}
		if logQuality != "" {
			setParts = append(setParts, fmt.Sprintf("log_quality = $%d", i))
			argsList = append(argsList, logQuality)
			i++
		}
		if note != "" {
			noteLine := fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02"), note)
			setParts = append(setParts, fmt.Sprintf("notes = COALESCE(notes || E'\\n', '') || $%d", i))
			argsList = append(argsList, noteLine)
			i++
		}
		q := `UPDATE error SET ` + strings.Join(setParts, ", ") + ` WHERE fingerprint LIKE $` + strconv.Itoa(i) + ` RETURNING fingerprint, status, message`
		argsList = append(argsList, fp+"%")
		rows, err := pool.Query(cmd.Context(), q, argsList...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var hits int
		for rows.Next() {
			var f, st, msg string
			_ = rows.Scan(&f, &st, &msg)
			fmt.Printf("✓ %s → %s  (%s)\n", f, st, trunc(msg, 60))
			hits++
		}
		if hits == 0 {
			return fmt.Errorf("No error matching fingerprint: %s", fp)
		}
		return nil
	}

	rows, err := pool.Query(cmd.Context(), `SELECT * FROM error WHERE fingerprint LIKE $1 ORDER BY last_seen DESC`, fp+"%")
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := rowFields(rows)
	out := []map[string]any{}
	for rows.Next() {
		vals, _ := rows.Values()
		row := map[string]any{}
		for j, c := range cols {
			row[c] = vals[j]
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		return fmt.Errorf("No error matching fingerprint: %s", fp)
	}
	if wantJSON {
		j, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	if len(out) > 1 {
		fmt.Printf("# %d matches for %q:\n", len(out), fp)
		for _, r := range out {
			fmt.Printf("- %s  %s  %s\n", r["fingerprint"], r["app_slug"], trunc(fmt.Sprint(r["message"]), 60))
		}
		return nil
	}
	e := out[0]
	fmt.Printf("# Error %s  (%s %s)\n\n", e["fingerprint"], e["app_slug"], e["surface"])
	for _, k := range []string{"status", "priority", "count", "level", "first_seen", "last_seen", "message"} {
		if v, ok := e[k]; ok && v != nil {
			fmt.Printf("- %s: %v\n", k, v)
		}
	}
	if e["stack"] != nil && fmt.Sprint(e["stack"]) != "" {
		fmt.Printf("\n## Stack\n\n```\n%s\n```\n", e["stack"])
	}
	if e["notes"] != nil && fmt.Sprint(e["notes"]) != "" {
		fmt.Printf("\n## Notes\n\n%s\n", e["notes"])
	}
	return nil
}

// ─── shared rendering ──────────────────────────────────────────────────

func rowFields(rows pgx.Rows) []string {
	fds := rows.FieldDescriptions()
	out := make([]string, len(fds))
	for i, f := range fds {
		out[i] = string(f.Name)
	}
	return out
}

func renderMD(cols []string, rows []map[string]any) {
	if len(rows) == 0 {
		fmt.Println("_(no rows)_")
		return
	}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c)
	}
	cells := make([][]string, len(rows))
	for ri, r := range rows {
		cells[ri] = make([]string, len(cols))
		for ci, c := range cols {
			s := fmtCell(r[c])
			if len(s) > 60 {
				s = s[:59] + "…"
			}
			cells[ri][ci] = s
			if len(s) > widths[ci] {
				widths[ci] = len(s)
			}
		}
	}
	fmtRow := func(vals []string) string {
		out := "| "
		for i, v := range vals {
			out += padRight(v, widths[i])
			if i < len(vals)-1 {
				out += " | "
			}
		}
		return out + " |"
	}
	sep := "|"
	for _, w := range widths {
		sep += strings.Repeat("-", w+2) + "|"
	}
	fmt.Println(fmtRow(cols))
	fmt.Println(sep)
	for _, c := range cells {
		fmt.Println(fmtRow(c))
	}
	fmt.Printf("\n_(%d rows)_\n", len(rows))
}

func fmtCell(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case time.Time:
		return t.Format("2006-01-02 15:04")
	case []any:
		strs := make([]string, len(t))
		for i, x := range t {
			strs[i] = fmt.Sprint(x)
		}
		return strings.Join(strs, ",")
	case []string:
		return strings.Join(t, ",")
	}
	j, err := json.Marshal(v)
	if err == nil {
		s := string(j)
		if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
			s = s[1 : len(s)-1]
		}
		return s
	}
	return fmt.Sprint(v)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func parseSince(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	m := sinceRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "s":
		return time.Now().Add(-time.Duration(n) * time.Second)
	case "m":
		return time.Now().Add(-time.Duration(n) * time.Minute)
	case "h":
		return time.Now().Add(-time.Duration(n) * time.Hour)
	case "d":
		return time.Now().AddDate(0, 0, -n)
	}
	return time.Time{}
}

func getString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/apps"
	mmhttp "mm-cli/internal/http"
)

// NewKbCmd builds `mm kb …` — the Knowledge Base surface over /api/rpc.
//
// Two layers: intent verbs that operate on meaning (names resolve to UUIDs
// anywhere) and a raw `kb <feature> <action> [k=v…]` passthrough. Read verbs
// render markdown by default; --json returns the structured payload.
func NewKbCmd() *cobra.Command {
	c := &cobra.Command{Use: "kb", Short: "Knowledge Base"}
	c.AddCommand(
		// Navigation
		newKbFindCmd(), newKbTreeCmd(), newKbPeekCmd(), newKbReadCmd(),
		newKbRelatedCmd(), newKbTaggedCmd(), newKbMentionsCmd(),
		newKbDigestCmd(), newKbSurfaceCmd(),
		// Mutation
		newKbRenameCmd(), newKbMoveCmd(), newKbAddCmd(), newKbRmCmd(),
		newKbTagCmd(), newKbUntagCmd(), newKbLabelCmd(), newKbDescribeCmd(),
		// Misc
		newKbCollectionsCmd(), newKbResearchCmd(), newKbDownloadCmd(),
		newKbActionsCmd(), newKbStatusCmd(),
	)
	// Default: kb <feature> <action> [k=v…] pass-through.
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 {
			return cmd.Help()
		}
		return kbDispatch(cmd.Context(), args[0], args[1], parseKV(args[2:]))
	}
	c.Args = cobra.ArbitraryArgs
	return c
}

// ─── RPC + envelope ────────────────────────────────────────────────────

type apiResource struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Attributes map[string]any `json:"attributes"`
}
type apiError struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}
type apiList struct {
	Data   []apiResource `json:"data"`
	Errors []apiError    `json:"errors"`
}
type apiSingle struct {
	Data   apiResource `json:"data"`
	Errors []apiError  `json:"errors"`
}

// kbCall posts to kb's /api/rpc and surfaces a JSON:API `errors` envelope
// (returned with HTTP 200) as a Go error.
func kbCall(ctx context.Context, feature, action string, payload map[string]any) (json.RawMessage, error) {
	app, err := apps.Resolve("kb")
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := mmhttp.New().Rpc(ctx, app.URL, feature, action, payload, &raw); err != nil {
		return nil, err
	}
	var probe struct {
		Errors []apiError `json:"errors"`
	}
	if json.Unmarshal(raw, &probe) == nil && len(probe.Errors) > 0 {
		msg := probe.Errors[0].Detail
		if msg == "" {
			msg = probe.Errors[0].Title
		}
		return raw, fmt.Errorf("%s.%s: %s", feature, action, msg)
	}
	return raw, nil
}

func kbList(ctx context.Context, feature, action string, payload map[string]any) (apiList, json.RawMessage, error) {
	raw, err := kbCall(ctx, feature, action, payload)
	if err != nil {
		return apiList{}, raw, err
	}
	var l apiList
	_ = json.Unmarshal(raw, &l)
	return l, raw, nil
}

func kbSingle(ctx context.Context, feature, action string, payload map[string]any) (apiSingle, json.RawMessage, error) {
	raw, err := kbCall(ctx, feature, action, payload)
	if err != nil {
		return apiSingle{}, raw, err
	}
	var s apiSingle
	_ = json.Unmarshal(raw, &s)
	return s, raw, nil
}

// ─── output helpers ────────────────────────────────────────────────────

func wantJSON(cmd *cobra.Command) bool {
	b, _ := cmd.Root().PersistentFlags().GetBool("json")
	return b
}

func emit(cmd *cobra.Command, raw json.RawMessage, markdown string) error {
	if wantJSON(cmd) {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			out, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		fmt.Println(string(raw))
		return nil
	}
	fmt.Println(markdown)
	return nil
}

// splitArgs separates positional args from key=value tokens.
func splitArgs(args []string) ([]string, map[string]any) {
	var pos []string
	kv := map[string]any{}
	for _, a := range args {
		if eq := strings.IndexByte(a, '='); eq > 0 && !strings.HasPrefix(a, "--") {
			kv[a[:eq]] = coerce(a[eq+1:])
			continue
		}
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
		}
	}
	return pos, kv
}

func kvStr(kv map[string]any, key string) string {
	if v, ok := kv[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func kvInt(kv map[string]any, key string, def int) int {
	if v, ok := kv[key]; ok {
		switch n := v.(type) {
		case int64:
			return int(n)
		case string:
			if i, err := strconv.Atoi(n); err == nil {
				return i
			}
		}
	}
	return def
}

func kvFloat(kv map[string]any, key string) (float64, bool) {
	if v, ok := kv[key]; ok {
		switch n := v.(type) {
		case int64:
			return float64(n), true
		case string:
			if f, err := strconv.ParseFloat(n, 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

var (
	uuidRE      = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	shortUUIDRE = regexp.MustCompile(`(?i)^[0-9a-f]{8,35}$`)
)

func isID(s string) bool { return uuidRE.MatchString(s) || shortUUIDRE.MatchString(s) }

func attrStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func attrFloat(m map[string]any, key string) (float64, bool) {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f, true
		}
	}
	return 0, false
}

func attrLabels(m map[string]any) string {
	v, ok := m["labels"]
	if !ok {
		return ""
	}
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	var names []string
	for _, l := range arr {
		switch x := l.(type) {
		case string:
			names = append(names, x)
		case map[string]any:
			if n := attrStr(x, "name", "slug"); n != "" {
				names = append(names, n)
			}
		}
	}
	return strings.Join(names, ", ")
}

func fmtDate(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format("2006-01-02")
	}
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func fmtScore(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimRight(s[:n], " ") + "…"
}

// ─── name → UUID resolution ────────────────────────────────────────────

type collRef struct{ ID, Name, Description string }
type docRef struct{ ID, Title, CollectionID, UpdatedAt string }

var collCache []collRef

func listCollections(ctx context.Context) ([]collRef, error) {
	if collCache != nil {
		return collCache, nil
	}
	l, _, err := kbList(ctx, "collections", "list", nil)
	if err != nil {
		return nil, err
	}
	for _, r := range l.Data {
		collCache = append(collCache, collRef{r.ID, attrStr(r.Attributes, "name"), attrStr(r.Attributes, "description")})
	}
	return collCache, nil
}

func resolveCollection(ctx context.Context, input string) (collRef, error) {
	all, err := listCollections(ctx)
	if err != nil {
		return collRef{}, err
	}
	if isID(input) {
		for _, c := range all {
			if c.ID == input || strings.HasPrefix(c.ID, strings.ToLower(input)) {
				return c, nil
			}
		}
		return collRef{ID: input}, nil
	}
	lower := strings.ToLower(input)
	var matches []collRef
	for _, c := range all {
		if c.Name == input {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		for _, c := range all {
			if strings.ToLower(c.Name) == lower {
				matches = append(matches, c)
			}
		}
	}
	if len(matches) == 0 {
		for _, c := range all {
			if strings.Contains(strings.ToLower(c.Name), lower) {
				matches = append(matches, c)
			}
		}
	}
	if len(matches) == 0 {
		return collRef{}, fmt.Errorf("no collection matching %q", input)
	}
	if len(matches) > 1 {
		var names []string
		for _, m := range matches {
			names = append(names, fmt.Sprintf("%q", m.Name))
		}
		return collRef{}, fmt.Errorf("ambiguous %q. Matches: %s", input, strings.Join(names, ", "))
	}
	return matches[0], nil
}

func listDocuments(ctx context.Context, collectionID string) ([]docRef, error) {
	l, _, err := kbList(ctx, "documents", "list", map[string]any{"collectionId": collectionID})
	if err != nil {
		return nil, err
	}
	var docs []docRef
	for _, r := range l.Data {
		docs = append(docs, docRef{
			ID:        r.ID,
			Title:     attrStr(r.Attributes, "title"),
			UpdatedAt: attrStr(r.Attributes, "updatedAt", "createdAt"),
		})
	}
	return docs, nil
}

func resolveDocument(ctx context.Context, input, scope string) (docRef, error) {
	if isID(input) {
		s, _, err := kbSingle(ctx, "documents", "get", map[string]any{"id": input})
		if err != nil {
			return docRef{}, err
		}
		return docRef{ID: s.Data.ID, Title: attrStr(s.Data.Attributes, "title"), CollectionID: attrStr(s.Data.Attributes, "collectionId")}, nil
	}
	if scope == "" {
		return docRef{}, fmt.Errorf("document name lookup needs scope — add in=<notebook>")
	}
	coll, err := resolveCollection(ctx, scope)
	if err != nil {
		return docRef{}, err
	}
	docs, err := listDocuments(ctx, coll.ID)
	if err != nil {
		return docRef{}, err
	}
	lower := strings.ToLower(input)
	var matches []docRef
	for _, d := range docs {
		if d.Title == input {
			matches = append(matches, d)
		}
	}
	if len(matches) == 0 {
		for _, d := range docs {
			if strings.ToLower(d.Title) == lower {
				matches = append(matches, d)
			}
		}
	}
	if len(matches) == 0 {
		for _, d := range docs {
			if strings.Contains(strings.ToLower(d.Title), lower) {
				matches = append(matches, d)
			}
		}
	}
	if len(matches) == 0 {
		return docRef{}, fmt.Errorf("no document matching %q in %q", input, coll.Name)
	}
	if len(matches) > 1 {
		return docRef{}, fmt.Errorf("ambiguous %q in %q (%d matches)", input, coll.Name, len(matches))
	}
	matches[0].CollectionID = coll.ID
	return matches[0], nil
}

// A label target may be a notebook or a document. Notebook unless scope forces doc.
type itemRef struct {
	Type  string // "collection" | "document"
	ID    string
	Label string
}

func resolveItem(ctx context.Context, target, scope string) (itemRef, error) {
	if scope == "" {
		if coll, err := resolveCollection(ctx, target); err == nil {
			return itemRef{"collection", coll.ID, coll.Name}, nil
		}
	}
	doc, err := resolveDocument(ctx, target, scope)
	if err != nil {
		return itemRef{}, err
	}
	return itemRef{"document", doc.ID, doc.Title}, nil
}

func resolveSince(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if m := regexp.MustCompile(`(?i)^(\d+)\s*([dhm])$`).FindStringSubmatch(raw); m != nil {
		n, _ := strconv.Atoi(m[1])
		var d time.Duration
		switch strings.ToLower(m[2]) {
		case "d":
			d = time.Duration(n) * 24 * time.Hour
		case "h":
			d = time.Duration(n) * time.Hour
		default:
			d = time.Duration(n) * time.Minute
		}
		return time.Now().Add(-d).UTC().Format(time.RFC3339), true
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(time.RFC3339), true
		}
	}
	return "", false
}

// ─── Navigation verbs ──────────────────────────────────────────────────

func newKbFindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "find <query> [in=<nb>] [limit=10] [minScore=0.45] [since=7d] [full=true]",
		Short: "Semantic search across notebooks (or one)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pos, kv := splitArgs(args)
			if len(pos) == 0 {
				return fmt.Errorf("usage: mm kb find <query> [in=<nb>] [limit] [minScore] [since] [full=true]")
			}
			query := strings.Join(pos, " ")
			scope := kvStr(kv, "in")
			limit := kvInt(kv, "limit", 10)
			full := kv["full"] == true
			payload := map[string]any{"query": query, "limit": limit}
			if ms, ok := kvFloat(kv, "minScore"); ok {
				payload["minScore"] = ms
			}
			if s := kvStr(kv, "since"); s != "" {
				iso, ok := resolveSince(s)
				if !ok {
					return fmt.Errorf("could not parse since=%q (use ISO date or 7d/24h/30m)", s)
				}
				payload["since"] = iso
			}
			ctx := cmd.Context()
			feature, action := "documents", "searchCorpus"
			if scope != "" {
				coll, err := resolveCollection(ctx, scope)
				if err != nil {
					return err
				}
				payload["collectionId"] = coll.ID
				action = "search"
			}
			l, raw, err := kbList(ctx, feature, action, payload)
			if err != nil {
				return err
			}
			snip := 200
			if full {
				snip = 1 << 30
			}
			var b strings.Builder
			scopeLabel := "all-notebooks"
			if scope != "" {
				scopeLabel = scope
			}
			fmt.Fprintf(&b, "# Search: %q — %d result%s\n_scope: %s_\n\n", query, len(l.Data), plural(len(l.Data)), scopeLabel)
			if len(l.Data) == 0 {
				b.WriteString("_No results._")
			}
			for i, r := range l.Data {
				a := r.Attributes
				level := "chunk"
				if lv, ok := attrFloat(a, "level"); ok && lv == 1 {
					level = "doc-summary"
				}
				heading := attrStr(a, "heading")
				if heading == "" {
					heading = fmt.Sprintf("(no heading, %s)", level)
				}
				docID := attrStr(a, "documentId", "document_id")
				docTitle := attrStr(a, "documentTitle", "title")
				fmt.Fprintf(&b, "## %d. %s\n", i+1, heading)
				meta := []string{}
				if docTitle != "" {
					meta = append(meta, "_"+docTitle+"_")
				}
				if sc, ok := attrFloat(a, "score"); ok {
					meta = append(meta, "score "+fmtScore(sc))
				}
				if level != "chunk" {
					meta = append(meta, "_"+level+"_")
				}
				fmt.Fprintf(&b, "%s\n`%s`\n\n", strings.Join(meta, " · "), docID)
				chunk := attrStr(a, "chunk")
				if chunk != "" {
					block := clip(chunk, snip)
					b.WriteString("> " + strings.ReplaceAll(block, "\n", "\n> ") + "\n\n")
				}
			}
			return emit(cmd, raw, strings.TrimRight(b.String(), "\n"))
		},
	}
}

func newKbTreeCmd() *cobra.Command {
	var label string
	var byLabel bool
	c := &cobra.Command{
		Use:   "tree [notebook]",
		Short: "List notebooks, or docs in one ([--label=slug] [--by-label])",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, _ := splitArgs(args)
			var target string
			if len(pos) > 0 {
				target = pos[0]
			}
			if target == "" {
				colls, err := listCollections(ctx)
				if err != nil {
					return err
				}
				if byLabel {
					type grp struct {
						Name string   `json:"name"`
						Slug string   `json:"slug"`
						Docs []string `json:"docs"`
					}
					groups := map[string]*grp{}
					for _, c := range colls {
						l, _, _ := kbList(ctx, "documents", "list", map[string]any{"collectionId": c.ID})
						for _, r := range l.Data {
							if arr, ok := r.Attributes["labels"].([]any); ok {
								for _, lab := range arr {
									if lm, ok := lab.(map[string]any); ok {
										slug := attrStr(lm, "slug")
										g := groups[slug]
										if g == nil {
											g = &grp{Name: attrStr(lm, "name"), Slug: slug}
											groups[slug] = g
										}
										g.Docs = append(g.Docs, fmt.Sprintf("- %s _(in %s)_", attrStr(r.Attributes, "title"), c.Name))
									}
								}
							}
						}
					}
					ordered := make([]*grp, 0, len(groups))
					for _, g := range groups {
						ordered = append(ordered, g)
					}
					sort.Slice(ordered, func(i, j int) bool { return len(ordered[i].Docs) > len(ordered[j].Docs) })
					var b strings.Builder
					fmt.Fprintf(&b, "# Notebooks grouped by label (%d labels)\n\n", len(ordered))
					for _, g := range ordered {
						fmt.Fprintf(&b, "## %s (%d)\n%s\n\n", g.Name, len(g.Docs), strings.Join(g.Docs, "\n"))
					}
					return emit(cmd, mustJSON(ordered), strings.TrimRight(b.String(), "\n"))
				}
				type nb struct {
					id, name string
					count    int
				}
				var rows []nb
				for _, c := range colls {
					docs, _ := listDocuments(ctx, c.ID)
					rows = append(rows, nb{c.ID, c.Name, len(docs)})
				}
				sort.Slice(rows, func(i, j int) bool { return rows[i].count > rows[j].count })
				var b strings.Builder
				fmt.Fprintf(&b, "# Notebooks (%d)\n\n", len(rows))
				for _, r := range rows {
					fmt.Fprintf(&b, "- **%s** — %d docs `%s`\n", r.name, r.count, r.id)
				}
				return emit(cmd, mustJSON(rows), strings.TrimRight(b.String(), "\n"))
			}
			coll, err := resolveCollection(ctx, target)
			if err != nil {
				return err
			}
			payload := map[string]any{"collectionId": coll.ID}
			if label != "" {
				payload["labels"] = []string{label}
			}
			l, raw, err := kbList(ctx, "documents", "list", payload)
			if err != nil {
				return err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "# %s (%d docs)\n`%s`\n", coll.Name, len(l.Data), coll.ID)
			if label != "" {
				fmt.Fprintf(&b, "_filter: label=%s_\n", label)
			}
			b.WriteString("\n")
			if len(l.Data) == 0 {
				b.WriteString("_No documents._")
			}
			for _, r := range l.Data {
				a := r.Attributes
				title := attrStr(a, "title")
				if title == "" {
					title = "(untitled)"
				}
				labels := attrLabels(a)
				labelStr := ""
				if labels != "" {
					labelStr = " _(" + labels + ")_"
				}
				date := ""
				if d := fmtDate(attrStr(a, "updatedAt")); d != "" {
					date = " _" + d + "_"
				}
				fmt.Fprintf(&b, "- %s%s%s `%s`\n", title, date, labelStr, r.ID)
			}
			return emit(cmd, raw, strings.TrimRight(b.String(), "\n"))
		},
	}
	c.Flags().StringVar(&label, "label", "", "filter docs by label slug")
	c.Flags().BoolVar(&byLabel, "by-label", false, "group docs by label (notebook list only)")
	return c
}

func newKbPeekCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "peek <name-or-id>",
		Short: "Summary of a notebook or document (no full body)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			target := args[0]

			peekColl := func(id, fallback string) (string, json.RawMessage, bool) {
				s, raw, err := kbSingle(ctx, "collections", "get", map[string]any{"id": id})
				if err != nil || s.Data.ID == "" {
					return "", raw, false
				}
				a := s.Data.Attributes
				docs, _ := listDocuments(ctx, id)
				rl, _, _ := kbList(ctx, "research", "list", map[string]any{"collectionId": id})
				name := attrStr(a, "name")
				if name == "" {
					name = fallback
				}
				var b strings.Builder
				fmt.Fprintf(&b, "# %s\n\n", name)
				meta := []string{"id: `" + id + "`", fmt.Sprintf("%d docs", len(docs))}
				if labels := attrLabels(a); labels != "" {
					meta = append(meta, "labels: "+labels)
				}
				b.WriteString(strings.Join(meta, " · ") + "\n\n")
				if d := attrStr(a, "description"); d != "" {
					b.WriteString(d + "\n\n")
				}
				if len(docs) > 0 {
					b.WriteString("## Recent documents\n\n")
					for i, d := range docs {
						if i >= 5 {
							break
						}
						fmt.Fprintf(&b, "- %s `%s` _(%s)_\n", d.Title, d.ID, fmtDate(d.UpdatedAt))
					}
					b.WriteString("\n")
				}
				if len(rl.Data) > 0 {
					fmt.Fprintf(&b, "## Research runs (%d)\n\n", len(rl.Data))
					for i, r := range rl.Data {
						if i >= 5 {
							break
						}
						st := attrStr(r.Attributes, "status")
						if st != "" {
							st = " _[" + st + "]_"
						}
						fmt.Fprintf(&b, "- `%s`%s _(%s)_\n", r.ID, st, fmtDate(attrStr(r.Attributes, "createdAt")))
					}
				}
				return strings.TrimRight(b.String(), "\n"), raw, true
			}

			peekDoc := func(id string) (string, json.RawMessage, bool) {
				s, raw, err := kbSingle(ctx, "documents", "get", map[string]any{"id": id})
				if err != nil || s.Data.ID == "" {
					return "", raw, false
				}
				a := s.Data.Attributes
				content := attrStr(a, "content")
				title := attrStr(a, "shortTitle", "title")
				var b strings.Builder
				fmt.Fprintf(&b, "# %s\n", title)
				if st := attrStr(a, "shortTitle"); st != "" && attrStr(a, "title") != "" && st != attrStr(a, "title") {
					fmt.Fprintf(&b, "_%s_\n", attrStr(a, "title"))
				}
				b.WriteString("\n")
				meta := []string{"id: `" + id + "`"}
				if cid := attrStr(a, "collectionId"); cid != "" {
					meta = append(meta, "collection: `"+cid+"`")
				}
				if d := fmtDate(attrStr(a, "createdAt")); d != "" {
					meta = append(meta, "created: "+d)
				}
				meta = append(meta, fmt.Sprintf("%d chars", len(content)))
				b.WriteString(strings.Join(meta, " · ") + "\n\n")
				if labels := attrLabels(a); labels != "" {
					fmt.Fprintf(&b, "**Labels:** %s\n\n", labels)
				}
				if sum := attrStr(a, "summary"); sum != "" {
					b.WriteString("## Summary\n\n" + sum + "\n\n")
				}
				if outline, ok := a["outline"].([]any); ok && len(outline) > 0 {
					fmt.Fprintf(&b, "## Outline (%d)\n\n", len(outline))
					for _, o := range outline {
						if om, ok := o.(map[string]any); ok {
							idx := 0
							if ci, ok := attrFloat(om, "chunkIndex"); ok {
								idx = int(ci)
							}
							fmt.Fprintf(&b, "%d. %s  `chunkIndex=%d`\n", idx+1, attrStr(om, "heading"), idx)
						}
					}
					b.WriteString("\n")
				}
				if content != "" {
					b.WriteString("## First 300 chars\n\n```\n" + clip(content, 300) + "\n```")
				}
				return strings.TrimRight(b.String(), "\n"), raw, true
			}

			if isID(target) {
				if md, raw, ok := peekColl(target, ""); ok {
					return emit(cmd, raw, md)
				}
				if md, raw, ok := peekDoc(target); ok {
					return emit(cmd, raw, md)
				}
				return fmt.Errorf("%s not found as collection or document", target)
			}
			coll, err := resolveCollection(ctx, target)
			if err != nil {
				return err
			}
			md, raw, ok := peekColl(coll.ID, coll.Name)
			if !ok {
				return fmt.Errorf("collection %q exists but could not load", target)
			}
			return emit(cmd, raw, md)
		},
	}
}

func newKbReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read <doc> [path=…] [inline=true] [in=<nb>]",
		Short: "Write doc body to /tmp (or inline if small)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, kv := splitArgs(args)
			if len(pos) == 0 {
				return fmt.Errorf("usage: mm kb read <doc> [path=…] [inline=true] [in=<nb>]")
			}
			target := pos[0]
			docID, title := target, ""
			if !isID(target) {
				d, err := resolveDocument(ctx, target, kvStr(kv, "in"))
				if err != nil {
					return err
				}
				docID, title = d.ID, d.Title
			}
			s, raw, err := kbSingle(ctx, "documents", "get", map[string]any{"id": docID})
			if err != nil {
				return err
			}
			a := s.Data.Attributes
			content := attrStr(a, "content")
			if content == "" {
				return fmt.Errorf("document has no content")
			}
			if title == "" {
				title = attrStr(a, "title")
			}
			const inlineMax = 8192
			path := kvStr(kv, "path")
			if path == "" {
				path = filepath.Join(os.TempDir(), "kb-"+docID+".md")
			}
			if kv["inline"] == true && len(content) <= inlineMax {
				return emit(cmd, raw, content)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return err
			}
			note := fmt.Sprintf("Wrote %q → `%s` (%d chars)", title, path, len(content))
			if kv["inline"] == true {
				note = fmt.Sprintf("Doc is %d chars (inline limit %d). Wrote %q → `%s` instead.", len(content), inlineMax, title, path)
			}
			return emit(cmd, mustJSON(map[string]any{"written": true, "path": path, "id": docID, "title": title, "size": len(content)}), note)
		},
	}
}

func newKbRelatedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "related <doc> [in=<nb>] [scope=<nb>] [limit=10] [minScore=0.5]",
		Short: "Vector-neighbour docs by summary embedding",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, kv := splitArgs(args)
			if len(pos) == 0 {
				return fmt.Errorf("usage: mm kb related <doc> [in=<nb>] [scope=<nb>] [limit] [minScore]")
			}
			target := pos[0]
			docID := target
			if !isID(target) {
				d, err := resolveDocument(ctx, target, kvStr(kv, "in"))
				if err != nil {
					return err
				}
				docID = d.ID
			}
			payload := map[string]any{"id": docID, "limit": kvInt(kv, "limit", 10)}
			if ms, ok := kvFloat(kv, "minScore"); ok {
				payload["minScore"] = ms
			}
			if sc := kvStr(kv, "scope"); sc != "" {
				coll, err := resolveCollection(ctx, sc)
				if err != nil {
					return err
				}
				payload["collectionId"] = coll.ID
			}
			l, raw, err := kbList(ctx, "documents", "related", payload)
			if err != nil {
				return err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "# Related to `%s` — %d result%s\n\n", docID, len(l.Data), plural(len(l.Data)))
			if len(l.Data) == 0 {
				b.WriteString("_No related docs above threshold._")
			}
			for i, r := range l.Data {
				a := r.Attributes
				sc, _ := attrFloat(a, "score")
				fmt.Fprintf(&b, "## %d. %s\nscore %s · `%s`\n\n", i+1, attrStr(a, "shortTitle", "title"), fmtScore(sc), attrStr(a, "id"))
				if sum := attrStr(a, "summary"); sum != "" {
					b.WriteString(sum + "\n\n")
				}
			}
			return emit(cmd, raw, strings.TrimRight(b.String(), "\n"))
		},
	}
}

func newKbTaggedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tagged <label-slug> [in=<nb>] [limit=50]",
		Short: "Docs carrying a label slug",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, kv := splitArgs(args)
			if len(pos) == 0 {
				return fmt.Errorf("usage: mm kb tagged <label-slug> [in=<nb>] [limit]")
			}
			slug := pos[0]
			payload := map[string]any{"labels": []string{slug}, "labelMode": "any", "limit": kvInt(kv, "limit", 50)}
			if in := kvStr(kv, "in"); in != "" {
				coll, err := resolveCollection(ctx, in)
				if err != nil {
					return err
				}
				payload["collectionId"] = coll.ID
			}
			l, raw, err := kbList(ctx, "documents", "list", payload)
			if err != nil {
				return err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "# Tagged %q — %d doc%s\n\n", slug, len(l.Data), plural(len(l.Data)))
			if len(l.Data) == 0 {
				b.WriteString("_No documents._")
			}
			for _, r := range l.Data {
				a := r.Attributes
				date := ""
				if d := fmtDate(attrStr(a, "updatedAt", "createdAt")); d != "" {
					date = " _(" + d + ")_"
				}
				fmt.Fprintf(&b, "- %s%s `%s`\n", attrStr(a, "shortTitle", "title"), date, r.ID)
			}
			return emit(cmd, raw, strings.TrimRight(b.String(), "\n"))
		},
	}
}

func newKbMentionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mentions <entity> [in=<nb>] [limit=25]",
		Short: "Backlinks: entity matches + text matches",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, kv := splitArgs(args)
			if len(pos) == 0 {
				return fmt.Errorf("usage: mm kb mentions <entity> [in=<nb>] [limit]")
			}
			entity := strings.Join(pos, " ")
			payload := map[string]any{"entity": entity, "limit": kvInt(kv, "limit", 25)}
			if in := kvStr(kv, "in"); in != "" {
				coll, err := resolveCollection(ctx, in)
				if err != nil {
					return err
				}
				payload["collectionId"] = coll.ID
			}
			l, raw, err := kbList(ctx, "documents", "mentions", payload)
			if err != nil {
				return err
			}
			var entityM, textM []apiResource
			for _, r := range l.Data {
				if attrStr(r.Attributes, "matchType") == "entity" {
					entityM = append(entityM, r)
				} else {
					textM = append(textM, r)
				}
			}
			var b strings.Builder
			fmt.Fprintf(&b, "# Mentions of %q — %d hit%s\n\n", entity, len(l.Data), plural(len(l.Data)))
			if len(entityM) > 0 {
				fmt.Fprintf(&b, "## Entity matches (%d)\n\n", len(entityM))
				for _, r := range entityM {
					a := r.Attributes
					fmt.Fprintf(&b, "- **%s** `%s`\n", attrStr(a, "shortTitle", "documentTitle"), attrStr(a, "documentId"))
				}
				b.WriteString("\n")
			}
			if len(textM) > 0 {
				fmt.Fprintf(&b, "## Text matches (%d)\n\n", len(textM))
				for _, r := range textM {
					a := r.Attributes
					heading := attrStr(a, "heading")
					if heading == "" {
						heading = "(no heading)"
					}
					fmt.Fprintf(&b, "### %s\n_%s_ · `%s`\n", heading, attrStr(a, "shortTitle", "documentTitle"), attrStr(a, "documentId"))
					if sn := attrStr(a, "snippet"); sn != "" {
						b.WriteString("\n> " + strings.ReplaceAll(sn, "\n", "\n> ") + "\n")
					}
					b.WriteString("\n")
				}
			}
			if len(l.Data) == 0 {
				b.WriteString("_No mentions._")
			}
			return emit(cmd, raw, strings.TrimRight(b.String(), "\n"))
		},
	}
}

func newKbDigestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "digest <notebook> [force=true]",
		Short: "~400-token narrative overview of a notebook (cached)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, kv := splitArgs(args)
			if len(pos) == 0 {
				return fmt.Errorf("usage: mm kb digest <notebook> [force=true]")
			}
			coll, err := resolveCollection(ctx, pos[0])
			if err != nil {
				return err
			}
			payload := map[string]any{"id": coll.ID}
			if kv["force"] == true {
				payload["force"] = true
			}
			s, raw, err := kbSingle(ctx, "collections", "digest", payload)
			if err != nil {
				return err
			}
			a := s.Data.Attributes
			cache := "_(fresh)_"
			if a["fromCache"] == true {
				cache = "_(cached)_"
			}
			docCount := 0
			if dc, ok := attrFloat(a, "docCount"); ok {
				docCount = int(dc)
			}
			digest := attrStr(a, "digest")
			if digest == "" {
				digest = "_No digest yet — add docs to the notebook to generate one._"
			}
			md := fmt.Sprintf("# %s — digest\n%d docs · generated %s · %s\n\n%s",
				coll.Name, docCount, fmtDate(attrStr(a, "generatedAt")), cache, digest)
			return emit(cmd, raw, md)
		},
	}
}

func newKbSurfaceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "surface [notebook]",
		Short: "Recently-added + open questions + contradictions",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, _ := splitArgs(args)
			payload := map[string]any{}
			scope := "all notebooks"
			if len(pos) > 0 {
				coll, err := resolveCollection(ctx, pos[0])
				if err != nil {
					return err
				}
				payload["id"] = coll.ID
				scope = pos[0]
			}
			s, raw, err := kbSingle(ctx, "collections", "surface", payload)
			if err != nil {
				return err
			}
			a := s.Data.Attributes
			recent, _ := a["recentlyAddedHighImportance"].([]any)
			questions, _ := a["openQuestions"].([]any)
			contradictions, _ := a["contradictions"].([]any)
			var b strings.Builder
			fmt.Fprintf(&b, "# Surface — %s\n\n## Recently added (%d)\n\n", scope, len(recent))
			if len(recent) == 0 {
				b.WriteString("_None._\n")
			}
			for _, item := range recent {
				m, _ := item.(map[string]any)
				fmt.Fprintf(&b, "### %s\nid: `%s` · added: %s\n\n", attrStr(m, "shortTitle", "title"), attrStr(m, "documentId"), fmtDate(attrStr(m, "createdAt")))
				if sum := attrStr(m, "summary"); sum != "" {
					b.WriteString(sum + "\n\n")
				}
			}
			fmt.Fprintf(&b, "## Open questions (%d)\n\n", len(questions))
			if len(questions) == 0 {
				b.WriteString("_None._\n")
			}
			for _, item := range questions {
				m, _ := item.(map[string]any)
				resolved := ""
				if attrStr(m, "resolvedByDocId") != "" {
					resolved = " _(resolved)_"
				}
				fmt.Fprintf(&b, "- %s%s\n  · raised %s by `%s`\n", attrStr(m, "question"), resolved, fmtDate(attrStr(m, "raisedAt")), attrStr(m, "raisedByDocId"))
			}
			fmt.Fprintf(&b, "\n## Contradictions (%d)\n\n", len(contradictions))
			if len(contradictions) == 0 {
				b.WriteString("_None._")
			}
			for _, item := range contradictions {
				m, _ := item.(map[string]any)
				conf, _ := attrFloat(m, "confidence")
				fmt.Fprintf(&b, "### %q ⇄ %q\nconfidence: %s · %s\n\n%s\n\n",
					attrStr(m, "sourceTitle"), attrStr(m, "targetTitle"), fmtScore(conf), fmtDate(attrStr(m, "observedAt")), attrStr(m, "reason"))
				if ev := attrStr(m, "evidenceQuote"); ev != "" {
					b.WriteString("> " + ev + "\n\n")
				}
			}
			return emit(cmd, raw, strings.TrimRight(b.String(), "\n"))
		},
	}
}

// ─── Mutation verbs ────────────────────────────────────────────────────

func newKbRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <notebook-or-doc> <new name> [in=<nb>]",
		Short: "Rename a notebook (name) or document (title)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, kv := splitArgs(args)
			if len(pos) < 2 {
				return fmt.Errorf("usage: mm kb rename <notebook-or-doc> <new name> [in=<nb>]")
			}
			target := pos[0]
			newName := strings.Join(pos[1:], " ")
			scope := kvStr(kv, "in")

			// Notebook unless scope (in=) forces document resolution.
			if scope == "" && !isID(target) {
				coll, err := resolveCollection(ctx, target)
				if err != nil {
					return err
				}
				_, raw, err := kbSingle(ctx, "collections", "update", map[string]any{"id": coll.ID, "name": newName})
				if err != nil {
					return err
				}
				return emit(cmd, raw, fmt.Sprintf("Renamed notebook → %q", newName))
			}
			if isID(target) && scope == "" {
				// Ambiguous id — try collection, fall back to document.
				if _, raw, err := kbSingle(ctx, "collections", "update", map[string]any{"id": target, "name": newName}); err == nil {
					return emit(cmd, raw, fmt.Sprintf("Renamed notebook → %q", newName))
				}
				_, raw, err := kbSingle(ctx, "documents", "update", map[string]any{"id": target, "title": newName})
				if err != nil {
					return err
				}
				return emit(cmd, raw, fmt.Sprintf("Renamed document → %q", newName))
			}
			doc, err := resolveDocument(ctx, target, scope)
			if err != nil {
				return err
			}
			_, raw, err := kbSingle(ctx, "documents", "update", map[string]any{"id": doc.ID, "title": newName})
			if err != nil {
				return err
			}
			return emit(cmd, raw, fmt.Sprintf("Renamed document → %q", newName))
		},
	}
}

func newKbMoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "move <doc> to <notebook> [in=<nb>]",
		Short: "Move a document to another notebook",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, kv := splitArgs(args)
			// Accept `move <doc> to <notebook>` or `move <doc> <notebook>` or to=<nb>.
			dest := kvStr(kv, "to")
			var docArg string
			if len(pos) > 0 {
				docArg = pos[0]
			}
			if dest == "" {
				for i, p := range pos {
					if p == "to" && i+1 < len(pos) {
						dest = pos[i+1]
						break
					}
				}
			}
			if dest == "" && len(pos) >= 2 {
				dest = pos[len(pos)-1]
			}
			if docArg == "" || dest == "" {
				return fmt.Errorf("usage: mm kb move <doc> to <notebook> [in=<nb>]")
			}
			doc, err := resolveDocument(ctx, docArg, kvStr(kv, "in"))
			if err != nil {
				return err
			}
			coll, err := resolveCollection(ctx, dest)
			if err != nil {
				return err
			}
			_, raw, err := kbSingle(ctx, "documents", "move", map[string]any{"id": doc.ID, "toCollectionId": coll.ID})
			if err != nil {
				return err
			}
			return emit(cmd, raw, fmt.Sprintf("Moved %q → %s", doc.Title, coll.Name))
		},
	}
}

func newKbAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <notebook> url=<url> | content=<text> [title=<title>]",
		Short: "Add a document to a notebook",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, kv := splitArgs(args)
			if len(pos) == 0 {
				return fmt.Errorf("usage: mm kb add <notebook> url=<url> | content=<text> [title=…]")
			}
			if kvStr(kv, "url") == "" && kvStr(kv, "content") == "" {
				return fmt.Errorf("provide url= or content=")
			}
			coll, err := resolveCollection(ctx, pos[0])
			if err != nil {
				return err
			}
			payload := map[string]any{"collectionId": coll.ID}
			for k, v := range kv {
				payload[k] = v
			}
			s, raw, err := kbSingle(ctx, "documents", "create", payload)
			if err != nil {
				return err
			}
			title := attrStr(s.Data.Attributes, "title")
			if title == "" {
				title = kvStr(kv, "title")
			}
			if title == "" {
				title = kvStr(kv, "url")
			}
			return emit(cmd, raw, fmt.Sprintf("Added %q → %s `%s`", title, coll.Name, s.Data.ID))
		},
	}
}

func newKbRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <doc> [in=<nb>]",
		Aliases: []string{"remove"},
		Short:   "Remove a document",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, kv := splitArgs(args)
			if len(pos) == 0 {
				return fmt.Errorf("usage: mm kb rm <doc> [in=<nb>]")
			}
			docID := pos[0]
			if !isID(docID) {
				d, err := resolveDocument(ctx, pos[0], kvStr(kv, "in"))
				if err != nil {
					return err
				}
				docID = d.ID
			}
			_, raw, err := kbSingle(ctx, "documents", "remove", map[string]any{"id": docID})
			if err != nil {
				return err
			}
			return emit(cmd, raw, fmt.Sprintf("Removed `%s`", docID))
		},
	}
}

func newKbTagCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag <target> <label> [<label> …] [in=<nb>]",
		Short: "Attach labels to a notebook or document",
		Args:  cobra.MinimumNArgs(2),
		RunE:  func(cmd *cobra.Command, args []string) error { return kbLabelOp(cmd, "add", args) },
	}
}

func newKbUntagCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "untag <target> <label> [in=<nb>]",
		Short: "Detach a label",
		Args:  cobra.MinimumNArgs(2),
		RunE:  func(cmd *cobra.Command, args []string) error { return kbLabelOp(cmd, "rm", args) },
	}
}

// kbLabelOp handles tag/untag/set against a notebook or document.
func kbLabelOp(cmd *cobra.Command, op string, args []string) error {
	ctx := cmd.Context()
	pos, kv := splitArgs(args)
	if len(pos) < 2 {
		return fmt.Errorf("usage: mm kb %s <target> <label> [in=<nb>]", op)
	}
	target, labels := pos[0], pos[1:]
	item, err := resolveItem(ctx, target, kvStr(kv, "in"))
	if err != nil {
		return err
	}
	switch op {
	case "set":
		_, raw, err := kbList(ctx, "labels", "replaceFor", map[string]any{"itemType": item.Type, "itemId": item.ID, "labels": labels})
		if err != nil {
			return err
		}
		return emit(cmd, raw, fmt.Sprintf("Set labels on %s %q: %s", item.Type, item.Label, strings.Join(labels, ", ")))
	case "add":
		var raw json.RawMessage
		for _, l := range labels {
			_, r, err := kbList(ctx, "labels", "attach", map[string]any{"itemType": item.Type, "itemId": item.ID, "label": l})
			if err != nil {
				return err
			}
			raw = r
		}
		return emit(cmd, raw, fmt.Sprintf("Tagged %s %q with: %s", item.Type, item.Label, strings.Join(labels, ", ")))
	case "rm":
		labelInput := labels[0]
		labelID := labelInput
		if !isID(labelInput) {
			s, _, err := kbSingle(ctx, "labels", "get", map[string]any{"slug": labelInput})
			if err != nil || s.Data.ID == "" {
				return fmt.Errorf("label not found: %s", labelInput)
			}
			labelID = s.Data.ID
		}
		_, raw, err := kbList(ctx, "labels", "detach", map[string]any{"itemType": item.Type, "itemId": item.ID, "labelId": labelID})
		if err != nil {
			return err
		}
		return emit(cmd, raw, fmt.Sprintf("Removed %q from %s %q", labelInput, item.Type, item.Label))
	}
	return fmt.Errorf("unknown label op %q", op)
}

func newKbLabelCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "label <set|suggest|rename|merge|list> …",
		Aliases: []string{"labels"},
		Short:   "Label management",
	}
	c.AddCommand(
		&cobra.Command{
			Use: "set <target> <label> [<label> …] [in=<nb>]", Short: "Replace a target's label set",
			Args: cobra.MinimumNArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error { return kbLabelOp(cmd, "set", args) },
		},
		&cobra.Command{
			Use: "add <target> <label> [in=<nb>]", Short: "Attach labels",
			Args: cobra.MinimumNArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error { return kbLabelOp(cmd, "add", args) },
		},
		&cobra.Command{
			Use: "rm <target> <label> [in=<nb>]", Short: "Detach a label",
			Args: cobra.MinimumNArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error { return kbLabelOp(cmd, "rm", args) },
		},
		newKbLabelSuggestCmd(),
		&cobra.Command{
			Use: "rename slug=<slug>|id=<uuid> name=<name>", Short: "Rename a label",
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := cmd.Context()
				_, kv := splitArgs(args)
				name := kvStr(kv, "name")
				if name == "" {
					return fmt.Errorf("usage: mm kb label rename slug=<slug>|id=<uuid> name=<name>")
				}
				labelID := kvStr(kv, "id")
				if labelID == "" {
					if slug := kvStr(kv, "slug"); slug != "" {
						s, _, err := kbSingle(ctx, "labels", "get", map[string]any{"slug": slug})
						if err != nil || s.Data.ID == "" {
							return fmt.Errorf("label not found: %s", slug)
						}
						labelID = s.Data.ID
					}
				}
				if labelID == "" {
					return fmt.Errorf("provide id= or slug=")
				}
				_, raw, err := kbSingle(ctx, "labels", "rename", map[string]any{"id": labelID, "name": name})
				if err != nil {
					return err
				}
				return emit(cmd, raw, fmt.Sprintf("Renamed label → %q", name))
			},
		},
		&cobra.Command{
			Use: "merge from=<id-or-slug> to=<id-or-slug>", Short: "Merge one label into another",
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := cmd.Context()
				_, kv := splitArgs(args)
				from, to := kvStr(kv, "from"), kvStr(kv, "to")
				if from == "" || to == "" {
					return fmt.Errorf("usage: mm kb label merge from=<id-or-slug> to=<id-or-slug>")
				}
				resolve := func(in string) (string, error) {
					if isID(in) {
						return in, nil
					}
					s, _, err := kbSingle(ctx, "labels", "get", map[string]any{"slug": in})
					if err != nil || s.Data.ID == "" {
						return "", fmt.Errorf("label not found: %s", in)
					}
					return s.Data.ID, nil
				}
				fromID, err := resolve(from)
				if err != nil {
					return err
				}
				toID, err := resolve(to)
				if err != nil {
					return err
				}
				_, raw, err := kbSingle(ctx, "labels", "merge", map[string]any{"fromId": fromID, "toId": toID})
				if err != nil {
					return err
				}
				return emit(cmd, raw, fmt.Sprintf("Merged %s → %s", from, to))
			},
		},
		&cobra.Command{
			Use: "list", Short: "List all labels with usage counts", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				l, raw, err := kbList(cmd.Context(), "labels", "list", nil)
				if err != nil {
					return err
				}
				var b strings.Builder
				fmt.Fprintf(&b, "# Labels (%d)\n\n", len(l.Data))
				for _, r := range l.Data {
					a := r.Attributes
					slug := attrStr(a, "slug")
					if slug == "" {
						slug = r.ID
					}
					count := ""
					if c, ok := attrFloat(a, "usageCount"); ok {
						count = fmt.Sprintf(" _(%d)_", int(c))
					}
					fmt.Fprintf(&b, "- %s `%s`%s\n", attrStr(a, "name"), slug, count)
				}
				return emit(cmd, raw, strings.TrimRight(b.String(), "\n"))
			},
		},
	)
	return c
}

func newKbLabelSuggestCmd() *cobra.Command {
	var apply, force bool
	c := &cobra.Command{
		Use:   "suggest <notebook>",
		Short: "Propose labels for a notebook (LLM, cached 24h)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, _ := splitArgs(args)
			if len(pos) == 0 {
				return fmt.Errorf("usage: mm kb label suggest <notebook> [--apply] [--force]")
			}
			coll, err := resolveCollection(ctx, pos[0])
			if err != nil {
				return err
			}
			s, raw, err := kbSingle(ctx, "labels", "suggestForCollection", map[string]any{"collectionId": coll.ID, "force": force})
			if err != nil {
				return err
			}
			candidates, _ := s.Data.Attributes["candidates"].([]any)
			if !apply {
				return emit(cmd, raw, fmt.Sprintf("%d label candidates for %s (use --apply to apply)", len(candidates), coll.Name))
			}
			if len(candidates) == 0 {
				return emit(cmd, raw, "No candidates to apply.")
			}
			as, araw, err := kbSingle(ctx, "labels", "applySuggestion", map[string]any{"collectionId": coll.ID, "candidates": candidates})
			if err != nil {
				return err
			}
			applied, _ := as.Data.Attributes["applied"].([]any)
			return emit(cmd, araw, fmt.Sprintf("Applied %d labels to %s", len(applied), coll.Name))
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "apply the proposed labels")
	c.Flags().BoolVar(&force, "force", false, "bypass the 24h cache")
	return c
}

func newKbDescribeCmd() *cobra.Command {
	var dryRun, placeholders bool
	c := &cobra.Command{
		Use:   "describe [notebook]",
		Short: "Auto-synthesise description + labels for a notebook",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pos, _ := splitArgs(args)
			var targets []collRef
			if placeholders {
				colls, err := listCollections(ctx)
				if err != nil {
					return err
				}
				for _, c := range colls {
					d := c.Description
					if d == "" || d == "A new notebook collection" || strings.HasPrefix(d, "Automatically imported from") || len(strings.TrimSpace(d)) < 10 {
						targets = append(targets, c)
					}
				}
				if len(targets) == 0 {
					fmt.Println("No placeholder descriptions found.")
					return nil
				}
			} else {
				if len(pos) == 0 {
					return fmt.Errorf("usage: mm kb describe <notebook> [--dry-run] | describe --placeholders")
				}
				coll, err := resolveCollection(ctx, pos[0])
				if err != nil {
					return err
				}
				targets = append(targets, coll)
			}
			var b strings.Builder
			var lastRaw json.RawMessage
			for _, t := range targets {
				s, raw, err := kbSingle(ctx, "labels", "synthesiseForCollection", map[string]any{"collectionId": t.ID, "dryRun": dryRun})
				lastRaw = raw
				suffix := ""
				if dryRun {
					suffix = " (dry-run)"
				}
				fmt.Fprintf(&b, "# %s%s\n", t.Name, suffix)
				if err != nil {
					fmt.Fprintf(&b, "_error: %s_\n\n", err)
					continue
				}
				desc := attrStr(s.Data.Attributes, "description")
				if desc == "" {
					desc = "_no change_"
				}
				b.WriteString(desc + "\n\n")
			}
			return emit(cmd, lastRaw, strings.TrimRight(b.String(), "\n"))
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "propose without applying")
	c.Flags().BoolVar(&placeholders, "placeholders", false, "run against all notebooks with placeholder descriptions")
	return c
}

// ─── Misc ──────────────────────────────────────────────────────────────

func newKbCollectionsCmd() *cobra.Command {
	// `kb collections` lists; `kb collections <action> [k=v…]` passes through.
	// Validate against known actions so stray positionals (e.g. `help`) error,
	// keeping the leaf-command convention while still allowing create/remove.
	actions := map[string]bool{
		"list": true, "create": true, "get": true, "update": true,
		"remove": true, "surface": true, "digest": true,
	}
	return &cobra.Command{
		Use:     "collections [action] [k=v…]",
		Aliases: []string{"col", "notebooks"},
		Short:   "List notebooks (or collections.<action>)",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && !actions[args[0]] {
				return fmt.Errorf("unknown action %q for kb collections", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "list" {
				return newKbTreeCmd().RunE(cmd, nil)
			}
			return kbDispatch(cmd.Context(), "collections", args[0], parseKV(args[1:]))
		},
	}
}

func newKbResearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "research <list|create|get|execute|…> …",
		Short: "Research runs",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if len(args) >= 1 && args[0] == "list" {
				pos, kv := splitArgs(args[1:])
				collectionID := kvStr(kv, "collectionId")
				if collectionID == "" && len(pos) > 0 {
					coll, err := resolveCollection(ctx, pos[0])
					if err != nil {
						return err
					}
					collectionID = coll.ID
				}
				if collectionID == "" {
					return fmt.Errorf("usage: mm kb research list <notebook>|collectionId=<uuid>")
				}
				l, raw, err := kbList(ctx, "research", "list", map[string]any{"collectionId": collectionID})
				if err != nil {
					return err
				}
				var b strings.Builder
				fmt.Fprintf(&b, "# Research runs (%d)\n\n", len(l.Data))
				if len(l.Data) == 0 {
					b.WriteString("_No research runs in this collection._")
				}
				for _, r := range l.Data {
					a := r.Attributes
					st := attrStr(a, "status")
					if st != "" {
						st = " _[" + st + "]_"
					}
					fmt.Fprintf(&b, "- `%s`%s _(%s)_\n", r.ID, st, fmtDate(attrStr(a, "createdAt")))
					if p := attrStr(a, "prompt"); p != "" {
						fmt.Fprintf(&b, "  · %s\n", clip(strings.Join(strings.Fields(p), " "), 200))
					}
				}
				return emit(cmd, raw, strings.TrimRight(b.String(), "\n"))
			}
			if len(args) < 2 {
				return fmt.Errorf("usage: mm kb research <action> [k=v…]")
			}
			return kbDispatch(ctx, "research", args[0], parseKV(args[1:]))
		},
	}
}

func newKbDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "download <doc-id> path=<file>",
		Short: "Write a document's content to a path",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pos, kv := splitArgs(args)
			if len(pos) == 0 || kvStr(kv, "path") == "" {
				return fmt.Errorf("usage: mm kb download <doc-id> path=/path/to/file")
			}
			s, raw, err := kbSingle(cmd.Context(), "documents", "get", map[string]any{"id": pos[0]})
			if err != nil {
				return err
			}
			content := attrStr(s.Data.Attributes, "content")
			if content == "" {
				return fmt.Errorf("document has no content")
			}
			path := kvStr(kv, "path")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return err
			}
			return emit(cmd, raw, fmt.Sprintf("Wrote %q → %s (%d chars)", attrStr(s.Data.Attributes, "title"), path, len(content)))
		},
	}
}

func newKbActionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "actions",
		Aliases: []string{"introspect"},
		Short:   "List the full RPC surface (self-discovery)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := kbCall(cmd.Context(), "meta", "actions", nil)
			if err != nil {
				return err
			}
			var resp struct {
				Data struct {
					Features []struct {
						Feature string   `json:"feature"`
						Type    string   `json:"type"`
						Actions []string `json:"actions"`
					} `json:"features"`
				} `json:"data"`
			}
			_ = json.Unmarshal(raw, &resp)
			var b strings.Builder
			b.WriteString("# KB RPC surface\n\n")
			for _, f := range resp.Data.Features {
				quoted := make([]string, len(f.Actions))
				for i, a := range f.Actions {
					quoted[i] = "`" + a + "`"
				}
				fmt.Fprintf(&b, "## %s _(%s)_\n\n%s\n\n", f.Feature, f.Type, strings.Join(quoted, " · "))
			}
			return emit(cmd, raw, strings.TrimRight(b.String(), "\n"))
		},
	}
}

func newKbStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "KB health + auth check",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := kbCall(cmd.Context(), "status", "get", nil)
			if err != nil {
				return err
			}
			return emit(cmd, raw, "KB: "+string(raw))
		},
	}
}

// ─── passthrough ───────────────────────────────────────────────────────

func kbDispatch(ctx context.Context, feature, action string, payload map[string]any) error {
	return doRpcAndRender(ctx, "kb", feature, action, payload)
}

// doRpcAndRender is the generic render path shared by kb + crm.
func doRpcAndRender(ctx context.Context, slug, feature, action string, payload map[string]any) error {
	app, err := apps.Resolve(slug)
	if err != nil {
		return err
	}
	client := mmhttp.New()
	var raw json.RawMessage
	if err := client.Rpc(ctx, app.URL, feature, action, payload, &raw); err != nil {
		return err
	}
	var probe struct {
		Data []struct {
			ID         string                 `json:"id"`
			Type       string                 `json:"type"`
			Attributes map[string]interface{} `json:"attributes"`
		} `json:"data"`
		Meta map[string]interface{} `json:"meta"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil {
		if len(probe.Data) > 0 {
			for _, h := range probe.Data {
				title := stringFirst(h.Attributes, "title", "name", "shortTitle", "label")
				summary := stringFirst(h.Attributes, "summary")
				idShort := h.ID
				if len(idShort) > 8 {
					idShort = h.ID[:8]
				}
				if title == "" {
					title = "(untitled)"
				}
				fmt.Printf("- **`%s`** — %s\n", idShort, title)
				if summary != "" {
					if len(summary) > 120 {
						summary = summary[:120]
					}
					fmt.Printf("  > %s\n", summary)
				}
			}
			return nil
		}
	}
	var any2 interface{}
	if err := json.Unmarshal(raw, &any2); err == nil {
		out, _ := json.MarshalIndent(any2, "", "  ")
		fmt.Printf("```json\n%s\n```\n", string(out))
		return nil
	}
	fmt.Println(string(raw))
	return nil
}

func stringFirst(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

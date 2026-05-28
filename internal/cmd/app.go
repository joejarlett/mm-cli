package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mm-cli/internal/apps"
	"mm-cli/internal/card"
	mmhttp "mm-cli/internal/http"
)

// NewAppCommands builds `mm <slug>` for every registered app slug.
// Each command exposes: `mm <slug>` (render card), `mm <slug> ask <q>`,
// `mm <slug> find <q>`, `mm <slug> do <tool> [k=v…]`, and the raw
// `mm <slug> <feature> <action>` escape hatch. Mirrors src/commands/app.ts.
//
// Returns a list of top-level commands to add. The caller is responsible
// for guarding against name collisions with built-in commands (kb / crm
// have their own typed wrappers; finances / gn / analytics use this path).
func NewAppCommands() []*cobra.Command {
	out := make([]*cobra.Command, 0, len(apps.Registry))
	// Skip slugs that have explicit, bespoke wrappers.
	skip := map[string]bool{"kb": true, "crm": true}
	for slug := range apps.Registry {
		if skip[slug] {
			continue
		}
		s := slug
		out = append(out, newAppCmd(s))
	}
	return out
}

func newAppCmd(slug string) *cobra.Command {
	root := &cobra.Command{
		Use:   slug + " [verb] [args...]",
		Short: "Talk to the " + slug + " app (ask / find / do / <feature> <action>)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppDispatch(cmd, slug, args)
		},
	}
	root.Flags().String("instance", "", "X-Hub-Instance-Id header")
	root.Flags().Bool("no-validate", false, "Skip manifest pre-validation")
	root.Flags().Bool("refresh", false, "Refresh the Agent Card cache")
	return root
}

func runAppDispatch(cmd *cobra.Command, slug string, args []string) error {
	ctx := cmd.Context()
	instance, _ := cmd.Flags().GetString("instance")
	refresh, _ := cmd.Flags().GetBool("refresh")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	if len(args) == 0 || args[0] == "help" {
		// Render card.
		c, err := card.Load(ctx, slug, refresh)
		if err != nil {
			return err
		}
		if wantJSON {
			out, _ := json.MarshalIndent(c, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		fmt.Printf("# %s\n\n", c.Name)
		if c.Description != "" {
			fmt.Println(c.Description)
			fmt.Println()
		}
		if len(c.Capabilities) > 0 {
			fmt.Printf("Capabilities: %s\n", strings.Join(c.Capabilities, ", "))
		}
		if len(c.Tools) > 0 {
			fmt.Println("Tools:")
			for _, t := range c.Tools {
				fmt.Printf("  - %s\n", t.Name)
			}
		}
		return nil
	}

	verb := args[0]
	rest := args[1:]
	app, err := apps.Resolve(slug)
	if err != nil {
		return err
	}
	client := mmhttp.New()

	dispatch := func(featureAction string, payload map[string]any) error {
		// Resolve the target instance when the caller didn't pass --instance.
		// Instance-scoped apps (kb/finances/gn/crm) otherwise 422 with
		// "X-Hub-Instance-Id required". Resolution honours the user's pinned
		// default (mm <app> use); ambiguity surfaces a helpful error.
		inst := instance
		if inst == "" {
			resolved, rerr := resolveDefaultInstance(ctx, client, slug)
			if rerr != nil {
				return rerr
			}
			inst = resolved
		}
		res, err := client.V2(ctx, app.URL, featureAction, payload, mmhttp.V2Opts{InstanceID: inst})
		if err != nil {
			return err
		}
		if wantJSON {
			fmt.Println(string(res.Body))
		} else {
			var v any
			if json.Unmarshal(res.Body, &v) == nil {
				out, _ := json.MarshalIndent(v, "", "  ")
				fmt.Println(string(out))
			} else {
				fmt.Println(string(res.Body))
			}
		}
		if !res.OK {
			return fmt.Errorf("HTTP %d", res.Status)
		}
		return nil
	}

	switch verb {
	case "use":
		// `mm <app> use <instance-name-or-id>` — pin the default instance.
		if len(rest) == 0 {
			return fmt.Errorf("Usage: mm %s use <instance-name-or-id>", slug)
		}
		target := strings.Join(rest, " ")
		items, err := listInstances(ctx, client, slug)
		if err != nil {
			return err
		}
		match, err := matchInstance(items, target)
		if err != nil {
			return err
		}
		if err := client.Hub(ctx, "instance", "setDefault",
			map[string]any{"slug": slug, "instanceId": match.ID}, &struct{}{}); err != nil {
			return err
		}
		fmt.Printf("Default %s instance → %q `%s`\n", slug, match.Name, match.ID)
		return nil
	case "ask":
		q := strings.Join(rest, " ")
		if q == "" {
			return fmt.Errorf("Usage: mm %s ask \"<question>\"", slug)
		}
		return dispatch("agent.chat", map[string]any{"question": q})
	case "find":
		q := strings.Join(rest, " ")
		if q == "" {
			return fmt.Errorf("Usage: mm %s find \"<query>\"", slug)
		}
		c, err := card.Load(ctx, slug, false)
		if err == nil && !c.HasCapability("search") {
			return fmt.Errorf("%s doesn't advertise the 'search' capability; try `mm %s ask`", slug, slug)
		}
		return dispatch("agent.search", map[string]any{"query": q})
	case "do":
		if len(rest) == 0 {
			return fmt.Errorf("Usage: mm %s do <tool> [k=v…]", slug)
		}
		toolName := rest[0]
		payload := parseKV(rest[1:])
		// Strip "<slug>." prefix if the user typed the fully-qualified name.
		bare := strings.TrimPrefix(toolName, slug+".")
		return dispatch(bare, payload)
	default:
		// Raw <feature> <action> [k=v…]
		if len(rest) == 0 {
			return fmt.Errorf("Usage: mm %s <feature> <action> [k=v…]", slug)
		}
		feature := verb
		action := rest[0]
		payload := parseKV(rest[1:])
		return dispatch(feature+"."+action, payload)
	}
}

// ── Instance resolution ─────────────────────────────────────────────────

type instanceItem struct {
	ID        string `json:"id"`
	AppSlug   string `json:"appSlug"`
	Name      string `json:"name"`
	IsOwner   bool   `json:"isOwner"`
	IsPrimary bool   `json:"isPrimary"`
}

func listInstances(ctx context.Context, client *mmhttp.Client, slug string) ([]instanceItem, error) {
	var resp struct {
		Instances []instanceItem `json:"instances"`
	}
	if err := client.Hub(ctx, "instance", "list", map[string]any{"slug": slug}, &resp); err != nil {
		return nil, err
	}
	return resp.Instances, nil
}

// resolveDefaultInstance picks the instance for an instance-scoped app when
// no --instance was given: the sole instance, or the pinned default. Returns
// "" for apps the user has no instance of (unscoped apps ignore the header).
// Errors only when it's genuinely ambiguous and no default is pinned.
func resolveDefaultInstance(ctx context.Context, client *mmhttp.Client, slug string) (string, error) {
	items, err := listInstances(ctx, client, slug)
	if err != nil {
		// Soft-fail: don't block a call on a resolution hiccup. If the app
		// truly needs an instance it'll return its own 422.
		return "", nil
	}
	switch len(items) {
	case 0:
		return "", nil
	case 1:
		return items[0].ID, nil
	}
	for _, it := range items {
		if it.IsPrimary {
			return it.ID, nil
		}
	}
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, fmt.Sprintf("%q", it.Name))
	}
	return "", fmt.Errorf(
		"multiple %s instances and no default set.\nPin one:  mm %s use \"<name>\"   (or pass --instance <id>)\nInstances: %s",
		slug, slug, strings.Join(names, ", "),
	)
}

// matchInstance resolves a name-or-id against the user's instances, with the
// same exact→case-insensitive→substring ladder used elsewhere; ambiguity is
// an error listing the candidates.
func matchInstance(items []instanceItem, target string) (instanceItem, error) {
	for _, it := range items {
		if it.ID == target {
			return it, nil
		}
	}
	lower := strings.ToLower(target)
	var matches []instanceItem
	for _, it := range items {
		if it.Name == target {
			matches = append(matches, it)
		}
	}
	if len(matches) == 0 {
		for _, it := range items {
			if strings.ToLower(it.Name) == lower {
				matches = append(matches, it)
			}
		}
	}
	if len(matches) == 0 {
		for _, it := range items {
			if strings.Contains(strings.ToLower(it.Name), lower) {
				matches = append(matches, it)
			}
		}
	}
	if len(matches) == 0 {
		return instanceItem{}, fmt.Errorf("no instance matching %q", target)
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = fmt.Sprintf("%q", m.Name)
		}
		return instanceItem{}, fmt.Errorf("ambiguous %q — matches: %s", target, strings.Join(names, ", "))
	}
	return matches[0], nil
}

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
	// Short is the registry's domain gloss (what the app is about). The verb
	// hint lives in the `mm --help` group header, not here, so the line stays
	// scannable and parallel with the other apps.
	short := slug
	if e, ok := apps.Registry[slug]; ok && e.Description != "" {
		short = e.Description
	}
	root := &cobra.Command{
		Use:   slug + " [verb] [args...]",
		Short: short,
		Long: short + "\n\n" +
			"Verbs:\n" +
			"  mm " + slug + "                          Show the app's Agent Card (capabilities + tools)\n" +
			"  mm " + slug + " ask \"<question>\"         Ask the app's agent (agent.chat) — prose answer\n" +
			"  mm " + slug + " find \"<query>\"           Search the app (agent.search, where supported)\n" +
			"  mm " + slug + " do <tool> [k=v…]         Invoke a Card-declared tool\n" +
			"  mm " + slug + " use [<name|id>]          Show instances, or pin the default one\n" +
			"  mm " + slug + " <feature> <action> [k=v…]   Raw call (the escape hatch)\n\n" +
			"Instance-scoped apps resolve the target automatically (sole instance,\n" +
			"else the pinned default from `use`); override with --instance.\n" +
			"Add --json for structured output.",
		Example: "  mm " + slug + " ask \"summarise what's here\"\n" +
			"  mm " + slug + " find \"<query>\"\n" +
			"  mm " + slug + " use",
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

	// dispatch sends feature.action to the app's /api/v2 with instance
	// resolution + rendering shared with the kb/crm wrappers (see runV2).
	dispatch := func(featureAction string, payload map[string]any) error {
		return runV2(cmd, slug, featureAction, payload, instance)
	}

	switch verb {
	case "use":
		// `mm <app> use` — show instances + current default.
		// `mm <app> use <name-or-id>` — pin the default.
		if len(rest) == 0 {
			return showInstances(cmd, slug)
		}
		return pinDefaultInstance(cmd, slug, strings.Join(rest, " "))
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

// runV2 dispatches feature.action to an app's /api/v2 with instance
// resolution and human/JSON rendering — the shared path behind the
// universal verbs (app.go) and the kb/crm wrappers. When instanceFlag is
// empty it resolves the user's instance (sole → pinned default → helpful
// ambiguity error). Renders agent.chat's markdown_snapshot when present.
func runV2(cmd *cobra.Command, slug, featureAction string, payload map[string]any, instanceFlag string) error {
	ctx := cmd.Context()
	app, err := apps.Resolve(slug)
	if err != nil {
		return err
	}
	client := mmhttp.New()
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	inst := instanceFlag
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
	// On failure, surface the structured error (the SDK's did-you-mean
	// message) cleanly instead of dumping the envelope. --json still gets
	// the raw body for scripting.
	if !res.OK {
		if wantJSON {
			fmt.Println(string(res.Body))
		}
		if msg := extractError(res.Body); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("HTTP %d", res.Status)
	}
	if wantJSON {
		fmt.Println(string(res.Body))
	} else if md := extractMarkdownSnapshot(res.Body); md != "" {
		fmt.Println(md)
	} else {
		var v any
		if json.Unmarshal(res.Body, &v) == nil {
			out, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println(string(out))
		} else {
			fmt.Println(string(res.Body))
		}
	}
	return nil
}

// extractError pulls a human message from a JSON:API-ish error envelope
// ({errors:[{message|detail|title}]}), or "" if the body isn't one.
func extractError(body []byte) string {
	var e struct {
		Errors []struct {
			Message string `json:"message"`
			Detail  string `json:"detail"`
			Title   string `json:"title"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 {
		switch {
		case e.Errors[0].Message != "":
			return e.Errors[0].Message
		case e.Errors[0].Detail != "":
			return e.Errors[0].Detail
		default:
			return e.Errors[0].Title
		}
	}
	return ""
}

// showInstances lists an app's instances, marking the pinned default — the
// CLI's read view of default-instance state (companion to `use <name>`).
func showInstances(cmd *cobra.Command, slug string) error {
	ctx := cmd.Context()
	client := mmhttp.New()
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	items, err := listInstances(ctx, client, slug)
	if err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(items) == 0 {
		fmt.Printf("No %s instances.\n", slug)
		return nil
	}
	fmt.Printf("# %s instances (%d)\n\n", slug, len(items))
	for _, it := range items {
		marker := "  "
		suffix := ""
		if it.IsPrimary {
			marker = "● "
			suffix = "  _(default)_"
		}
		fmt.Printf("%s%s `%s`%s\n", marker, it.Name, it.ID, suffix)
	}
	fmt.Printf("\nSet default: mm %s use \"<name>\"\n", slug)
	return nil
}

// pinDefaultInstance resolves a name-or-id against the user's instances for
// an app and writes it as the default (hub instance.setDefault). Shared by
// `mm <app> use` across the universal path and the kb/crm wrappers.
func pinDefaultInstance(cmd *cobra.Command, slug, target string) error {
	ctx := cmd.Context()
	client := mmhttp.New()
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
}

// extractMarkdownSnapshot pulls the `markdown_snapshot` field out of an
// agent.chat response, or "" if absent. Lets `ask` render the readable
// answer instead of the raw {intent, entities, writes, …} envelope.
func extractMarkdownSnapshot(body []byte) string {
	var probe struct {
		MarkdownSnapshot string `json:"markdown_snapshot"`
	}
	if json.Unmarshal(body, &probe) == nil {
		return strings.TrimSpace(probe.MarkdownSnapshot)
	}
	return ""
}

// ── Instance resolution ─────────────────────────────────────────────────

type instanceItem struct {
	ID        string `json:"id"`
	AppSlug   string `json:"appSlug"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	IsOwner   bool   `json:"isOwner"`
	IsPrimary bool   `json:"isPrimary"`
}

func listInstances(ctx context.Context, client *mmhttp.Client, slug string) ([]instanceItem, error) {
	var resp struct {
		Instances []instanceItem `json:"instances"`
	}
	// An empty slug means "every app" — the hub filters on the key's
	// presence, so send the payload without it rather than with "".
	payload := map[string]any{}
	if slug != "" {
		payload["slug"] = slug
	}
	if err := client.Hub(ctx, "instance", "list", payload, &resp); err != nil {
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

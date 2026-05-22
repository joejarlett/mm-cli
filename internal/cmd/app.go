package cmd

import (
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

	if len(args) == 0 {
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
		res, err := client.V2(ctx, app.URL, featureAction, payload, mmhttp.V2Opts{InstanceID: instance})
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

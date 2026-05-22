package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"mm-cli/internal/apps"
	"mm-cli/internal/manifest"
)

func runManifest(cmd *cobra.Command, args []string) error {
	if len(args) > 0 && args[0] == "help" {
		return cmd.Help()
	}
	refresh, _ := cmd.Flags().GetBool("refresh")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	ctx := cmd.Context()

	if len(args) == 0 {
		// All apps, summary.
		type row struct {
			Slug    string `json:"slug"`
			Version string `json:"version,omitempty"`
			Actions int    `json:"actions"`
			Err     string `json:"err,omitempty"`
		}
		rows := make([]row, 0, len(apps.Registry))
		for slug := range apps.Registry {
			r := row{Slug: slug}
			m, err := manifest.Load(ctx, slug, refresh)
			if err != nil {
				r.Err = err.Error()
			} else {
				r.Version = m.Version
				r.Actions = m.ActionCount()
			}
			rows = append(rows, r)
		}
		if wantJSON {
			out, _ := json.MarshalIndent(rows, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		fmt.Println("# Manifests")
		for _, r := range rows {
			if r.Err != "" {
				fmt.Printf("  %s  (fetch failed: %s)\n", padRight(r.Slug, 12), r.Err)
				continue
			}
			fmt.Printf("  %s  v%s  %d actions\n", padRight(r.Slug, 12), r.Version, r.Actions)
		}
		return nil
	}

	m, err := manifest.Load(ctx, args[0], refresh)
	if err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(m, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("# %s v%s\n\n", m.AppSlug, m.Version)
	for feature, actions := range m.Features {
		fmt.Printf("## %s (%d)\n", feature, len(actions))
		for action, def := range actions {
			auth := def.Auth
			if auth == "" {
				auth = "?"
			}
			desc := ""
			if def.Description != "" {
				desc = "  — " + def.Description
			}
			fmt.Printf("  %s.%s  [auth: %s]%s\n", feature, action, auth, desc)
		}
		fmt.Println()
	}
	return nil
}

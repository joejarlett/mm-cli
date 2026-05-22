package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mm-cli/internal/apps"
	"mm-cli/internal/card"
)

// NewCardsCmd builds `mm cards [<app>] [--refresh]`.
func NewCardsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "cards [app]",
		Aliases: []string{"card"},
		Short:   "Capability matrix / per-app Agent Card",
		Args:    cobra.MaximumNArgs(1),
		RunE:    runCards,
	}
	c.Flags().Bool("refresh", false, "Bypass the 24h cache")
	return c
}

func NewManifestCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "manifest [app]",
		Short: "Wire-level manifest (deeper than the Card)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runManifest,
	}
	c.Flags().Bool("refresh", false, "Bypass the 24h cache")
	return c
}

func runCards(cmd *cobra.Command, args []string) error {
	if len(args) > 0 && args[0] == "help" {
		return cmd.Help()
	}
	refresh, _ := cmd.Flags().GetBool("refresh")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	ctx := cmd.Context()

	if len(args) == 0 {
		// Capability matrix.
		type row struct {
			slug         string
			name         string
			capabilities []string
			tools        int
			err          error
		}
		rows := make([]row, 0, len(apps.Registry))
		for slug := range apps.Registry {
			c, err := card.Load(ctx, slug, refresh)
			r := row{slug: slug}
			if err != nil {
				r.err = err
			} else {
				r.name = c.Name
				r.capabilities = c.Capabilities
				r.tools = len(c.Tools)
			}
			rows = append(rows, r)
		}
		if wantJSON {
			out, _ := json.MarshalIndent(rows, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		fmt.Println("# Apps")
		fmt.Println()
		for _, r := range rows {
			if r.err != nil {
				fmt.Printf("  %s  (card fetch failed: %s)\n", padRight(r.slug, 12), r.err)
				continue
			}
			caps := strings.Join(r.capabilities, ",")
			if caps == "" {
				caps = "—"
			}
			fmt.Printf("  %s  %s  caps=%-30s tools=%d\n",
				padRight(r.slug, 12), padRight(r.name, 22), caps, r.tools)
		}
		return nil
	}

	slug := args[0]
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
	if c.Version != "" {
		fmt.Printf("Version: %s\n", c.Version)
	}
	if len(c.Capabilities) > 0 {
		fmt.Printf("Capabilities: %s\n", strings.Join(c.Capabilities, ", "))
	}
	if c.MCPURL != nil && *c.MCPURL != "" {
		fmt.Printf("MCP: %s\n", *c.MCPURL)
	}
	if len(c.Tools) > 0 {
		fmt.Println()
		fmt.Println("## Tools")
		for _, t := range c.Tools {
			ann := []string{}
			if t.ReadOnlyHint != nil && *t.ReadOnlyHint {
				ann = append(ann, "read-only")
			}
			if t.DestructiveHint != nil && *t.DestructiveHint {
				ann = append(ann, "destructive")
			}
			tagPart := ""
			if len(ann) > 0 {
				tagPart = "  [" + strings.Join(ann, ", ") + "]"
			}
			fmt.Printf("  - %s%s\n", t.Name, tagPart)
			if t.Description != "" {
				fmt.Printf("    %s\n", t.Description)
			}
		}
	}
	if len(c.Aliases) > 0 {
		fmt.Println()
		fmt.Println("## Aliases")
		for verb, a := range c.Aliases {
			fmt.Printf("  %s → %s.%s\n", verb, a.Feature, a.Action)
		}
	}
	return nil
}

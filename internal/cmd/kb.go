package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mm-cli/internal/apps"
	mmhttp "mm-cli/internal/http"
)

// NewKbCmd builds `mm kb …` — legacy /api/rpc wrapper.
func NewKbCmd() *cobra.Command {
	c := &cobra.Command{Use: "kb", Short: "Knowledge Base"}
	c.AddCommand(
		newKbFindCmd(), newKbTreeCmd(), newKbPeekCmd(),
		newKbReadCmd(), newKbCollectionsCmd(), newKbStatusCmd(),
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

func newKbFindCmd() *cobra.Command {
	return &cobra.Command{Use: "find [query]", Short: "Search documents", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return kbDispatch(cmd.Context(), "documents", "searchCorpus", map[string]any{"query": strings.Join(args, " ")})
		}}
}
func newKbTreeCmd() *cobra.Command {
	return &cobra.Command{Use: "tree [notebook]", Short: "List notebooks (or one notebook's docs)", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return kbDispatch(cmd.Context(), "collections", "get", map[string]any{"name": args[0]})
			}
			return kbDispatch(cmd.Context(), "collections", "list", nil)
		}}
}
func newKbPeekCmd() *cobra.Command {
	return &cobra.Command{Use: "peek [id]", Short: "Preview a document", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return kbDispatch(cmd.Context(), "documents", "get", map[string]any{"id": args[0]})
		}}
}
func newKbReadCmd() *cobra.Command {
	return &cobra.Command{Use: "read [id]", Short: "Read full document body", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return kbDispatch(cmd.Context(), "documents", "get", map[string]any{"id": args[0], "includeContent": "true"})
		}}
}
func newKbCollectionsCmd() *cobra.Command {
	return &cobra.Command{Use: "collections", Aliases: []string{"col", "notebooks"}, Short: "List collections", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return kbDispatch(cmd.Context(), "collections", "list", nil)
		}}
}
func newKbStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "KB health + auth check", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return kbDispatch(cmd.Context(), "status", "get", nil)
		}}
}

func kbDispatch(ctx context.Context, feature, action string, payload map[string]any) error {
	return doRpcAndRender(ctx, "kb", feature, action, payload)
}

// ─── shared rpc render path used by kb + crm ───────────────────────────

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
	// Try data array → list of hits.
	var probe struct {
		Data []struct {
			ID         string                 `json:"id"`
			Type       string                 `json:"type"`
			Attributes map[string]interface{} `json:"attributes"`
		} `json:"data"`
		Meta map[string]interface{} `json:"meta"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil {
		// Render lightly: titles + ids.
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
				fmt.Printf("  %s  %s\n", idShort, title)
				if summary != "" {
					if len(summary) > 120 {
						summary = summary[:120]
					}
					fmt.Printf("        %s\n", summary)
				}
				fmt.Println()
			}
			return nil
		}
	}
	// Fallback: pretty-print raw.
	var any2 interface{}
	if err := json.Unmarshal(raw, &any2); err == nil {
		out, _ := json.MarshalIndent(any2, "", "  ")
		fmt.Println(string(out))
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

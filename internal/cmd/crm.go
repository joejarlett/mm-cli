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

// NewCrmCmd builds `mm crm …`. Bespoke verbs (surface/contacts/log/…) ride
// crm's legacy /api/rpc; the universal `ask`/`use` route to /api/v2 via the
// shared runV2/pinDefaultInstance path (so default-instance + markdown
// rendering work uniformly). CRM is multi-instance, so `mm crm use <name>`
// pins which one `ask` targets.
func NewCrmCmd() *cobra.Command {
	c := &cobra.Command{Use: "crm", Short: apps.Registry["crm"].Description}
	c.AddCommand(
		newCrmSurfaceCmd(), newCrmContactsCmd(), newCrmProjectsCmd(),
		newCrmLogCmd(), newCrmContextCmd(), newCrmPeekCmd(),
		newCrmReadCmd(), newCrmFindCmd(), newCrmAskCmd(), newCrmUseCmd(),
	)
	c.Args = cobra.ArbitraryArgs
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 {
			return cmd.Help()
		}
		return crmDispatch(cmd.Context(), args[0], args[1], parseKV(args[2:]))
	}
	return c
}

func newCrmAskCmd() *cobra.Command {
	return &cobra.Command{Use: "ask [question]", Short: "Ask the CRM agent (agent.chat)", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runV2(cmd, "crm", "agent.chat", map[string]any{"question": strings.Join(args, " ")}, "")
		}}
}

func newCrmUseCmd() *cobra.Command {
	return &cobra.Command{Use: "use [instance-name-or-id]", Short: "Show CRM instances, or pin the default", Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return showInstances(cmd, "crm")
			}
			return pinDefaultInstance(cmd, "crm", strings.Join(args, " "))
		}}
}

// newCrmSurfaceCmd is an alias of the universal `mm surface crm` — one
// implementation of the surface axis (the normalised agent.surface, which
// renders commitments with due dates / overdue framing), not a second bespoke
// path. The richer salience view stays reachable via `mm crm ask`.
func newCrmSurfaceCmd() *cobra.Command {
	return &cobra.Command{Use: "surface", Short: "What's surfacing now (alias of `mm surface crm`)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return surfaceScoped(cmd, "crm", 0)
		}}
}
func newCrmContactsCmd() *cobra.Command {
	c := &cobra.Command{Use: "contacts [find <q>]", Short: "List or search contacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) >= 1 && args[0] == "find" {
				// By-NAME lookup over contact nodes — `contact.search`, NOT
				// `find.search` (which is a semantic search over INTERACTIONS
				// and so returns interaction rows, not people). `--all` opts
				// untriaged prospects back in.
				includeProspects := false
				terms := make([]string, 0, len(args)-1)
				for _, a := range args[1:] {
					if a == "--all" {
						includeProspects = true
					} else {
						terms = append(terms, a)
					}
				}
				if len(terms) == 0 {
					return fmt.Errorf("usage: mm crm contacts find <query> [--all]")
				}
				return crmDispatch(cmd.Context(), "contact", "search", map[string]any{
					"query":            strings.Join(terms, " "),
					"includeProspects": includeProspects,
				})
			}
			return renderCrmTree(cmd)
		}}
	c.Args = cobra.ArbitraryArgs
	return c
}

// renderCrmTree renders `mm crm contacts` (tree.show). The RPC puts the real
// totals in meta.counts and only a capped *preview* in meta.contacts — so the
// generic JSON dump made the 10-row preview read as "10 contacts total" when
// the instance actually has many more. Lead with the count header, then the
// preview, then signpost that it's a preview.
func renderCrmTree(cmd *cobra.Command) error {
	app, err := apps.Resolve("crm")
	if err != nil {
		return err
	}
	client := mmhttp.New()
	var raw json.RawMessage
	if err := client.Rpc(cmd.Context(), app.URL, "tree", "show", nil, &raw); err != nil {
		return err
	}
	if wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json"); wantJSON {
		var pretty interface{}
		_ = json.Unmarshal(raw, &pretty)
		out, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Printf("```json\n%s\n```\n", string(out))
		return nil
	}
	var resp struct {
		Meta struct {
			Counts   map[string]int `json:"counts"`
			Contacts []struct {
				ID                  string `json:"id"`
				Title               string `json:"title"`
				InteractionsCount   int    `json:"interactionsCount"`
				LastMeaningfulTouch string `json:"lastMeaningfulTouch"`
			} `json:"contacts"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	counts := resp.Meta.Counts
	active, prospects := counts["contact_active"], counts["contact_prospect"]
	fmt.Printf("**%d active contact%s**", active, plural(active))
	if prospects > 0 {
		fmt.Printf(" · %d untriaged prospect%s in /review", prospects, plural(prospects))
	}
	fmt.Print("\n\n")
	for _, c := range resp.Meta.Contacts {
		id := c.ID
		if len(id) > 8 {
			id = id[:8]
		}
		touch := ""
		if len(c.LastMeaningfulTouch) >= 10 {
			touch = " · last " + c.LastMeaningfulTouch[:10]
		}
		fmt.Printf("- **`%s`** — %s (%d interaction%s)%s\n", id, c.Title, c.InteractionsCount, plural(c.InteractionsCount), touch)
	}
	if shown := len(resp.Meta.Contacts); active > shown {
		fmt.Printf("\n_Showing the %d most-recently-touched of %d. Use `mm crm contacts find <name>` to look someone up._\n", shown, active)
	}
	return nil
}
func newCrmProjectsCmd() *cobra.Command {
	return &cobra.Command{Use: "projects", Short: "List CRM projects", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return crmDispatch(cmd.Context(), "project", "list", nil)
		}}
}
func newCrmLogCmd() *cobra.Command {
	return &cobra.Command{Use: "log [text]", Short: "Log an interaction", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return crmDispatch(cmd.Context(), "interaction", "log", map[string]any{"text": strings.Join(args, " ")})
		}}
}
func newCrmContextCmd() *cobra.Command {
	return &cobra.Command{Use: "context [person]", Short: "Person context", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return crmDispatch(cmd.Context(), "contact", "context", map[string]any{"target": args[0]})
		}}
}
func newCrmPeekCmd() *cobra.Command {
	return &cobra.Command{Use: "peek [id]", Short: "Preview anything", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return crmDispatch(cmd.Context(), "peek", "show", map[string]any{"target": args[0]})
		}}
}
func newCrmReadCmd() *cobra.Command {
	return &cobra.Command{Use: "read [id]", Short: "Full content", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return crmDispatch(cmd.Context(), "read", "show", map[string]any{"target": args[0]})
		}}
}
func newCrmFindCmd() *cobra.Command {
	return &cobra.Command{Use: "find [query]", Short: "Search the CRM", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return crmDispatch(cmd.Context(), "find", "search", map[string]any{"query": strings.Join(args, " ")})
		}}
}

func crmDispatch(ctx context.Context, feature, action string, payload map[string]any) error {
	return doRpcAndRender(ctx, "crm", feature, action, payload)
}

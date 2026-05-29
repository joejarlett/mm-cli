package cmd

import (
	"context"
	"strings"

	"github.com/spf13/cobra"
)

// NewCrmCmd builds `mm crm …`. Bespoke verbs (surface/contacts/log/…) ride
// crm's legacy /api/rpc; the universal `ask`/`use` route to /api/v2 via the
// shared runV2/pinDefaultInstance path (so default-instance + markdown
// rendering work uniformly). CRM is multi-instance, so `mm crm use <name>`
// pins which one `ask` targets.
func NewCrmCmd() *cobra.Command {
	c := &cobra.Command{Use: "crm", Short: "CRM"}
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
	return &cobra.Command{Use: "use <instance-name-or-id>", Short: "Pin the default CRM instance", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return pinDefaultInstance(cmd, "crm", strings.Join(args, " "))
		}}
}

func newCrmSurfaceCmd() *cobra.Command {
	return &cobra.Command{Use: "surface", Short: "Today's priorities", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return crmDispatch(cmd.Context(), "surface", "list", nil)
		}}
}
func newCrmContactsCmd() *cobra.Command {
	c := &cobra.Command{Use: "contacts [find <q>]", Short: "List or search contacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) >= 2 && args[0] == "find" {
				return crmDispatch(cmd.Context(), "find", "search", map[string]any{"query": strings.Join(args[1:], " ")})
			}
			return crmDispatch(cmd.Context(), "tree", "show", nil)
		}}
	c.Args = cobra.ArbitraryArgs
	return c
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

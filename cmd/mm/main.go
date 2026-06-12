// Command mm — Meta-Me CLI (Go port).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"mm-cli/internal/cmd"
	"mm-cli/internal/cmd/admin"
	"mm-cli/internal/db"
	"mm-cli/internal/version"
)

func main() {
	root := newRootCmd()

	// Cancel root context on Ctrl-C so in-flight HTTP/WS requests can clean up.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	preprocessed, err := cmd.PreprocessArgs(ctx, os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s\n", err.Error())
		os.Exit(1)
	}
	root.SetArgs(preprocessed)

	err = root.ExecuteContext(ctx)
	db.Close()
	if err != nil {
		// Cobra prints its own usage on Args errors; for our RunE errors we
		// want the TS-style "❌ msg" prefix on stderr + exit 1.
		fmt.Fprintf(os.Stderr, "❌ %s\n", err.Error())
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mm",
		Short: "Meta-Me CLI — one terminal for every Meta-Me app",
		Long: `mm — one terminal for every Meta-Me app.

Two ways to work with an app, by how precise your intent is:

  • Typed verbs (precise, composable, cheap) — when you know what you want:
      mm kb find "..."   mm kb peek <doc>   mm finances accounts list
  • ask (fuzzy, conversational) — hand an open question to the app's agent:
      mm finances ask "how's my spending trending?"

Prefer typed verbs when chaining steps; reach for ask when the question is
open-ended. Names work anywhere an id is taken; add --json for structured
output. Start with 'mm cards' to see every app, or 'mm <app> --help'.`,
		Version:       version.String(),
		SilenceUsage:  true, // don't dump usage on every RunE error
		SilenceErrors: true, // we handle stderr ourselves in main()
	}
	// `mm --version` → "<version.String()>\n" (drops Cobra's "mm version " prefix).
	root.SetVersionTemplate("{{.Version}}\n")

	root.PersistentFlags().Bool("json", false, "Output as JSON")

	// Help is grouped by intent, not by build phase — an agent/user scans for
	// "what do I want to do", not implementation history. Ungrouped commands
	// (help, completion) fall under Cobra's "Additional Commands".
	const (
		grpApps    = "apps"
		grpGoogle  = "google"
		grpAgents  = "agents"
		grpDiscing = "discover"
		grpAccount = "account"
		grpSystem  = "system"
	)
	root.AddGroup(
		&cobra.Group{ID: grpApps, Title: "Apps (ask / find / do / <feature> <action>):"},
		&cobra.Group{ID: grpGoogle, Title: "Google Workspace:"},
		&cobra.Group{ID: grpAgents, Title: "Agents & automation:"},
		&cobra.Group{ID: grpDiscing, Title: "Discovery:"},
		&cobra.Group{ID: grpAccount, Title: "Account:"},
		&cobra.Group{ID: grpSystem, Title: "CLI & admin:"},
	)

	// add registers a command under a help group.
	add := func(group string, c *cobra.Command) {
		c.GroupID = group
		root.AddCommand(c)
	}

	// Apps — the Meta-Me apps reachable over the v2 contract. kb/crm have
	// bespoke verb wrappers; analytics/finances/gn use the universal verbs.
	add(grpApps, cmd.NewKbCmd())
	add(grpApps, cmd.NewCrmCmd())
	for _, c := range cmd.NewAppCommands() {
		add(grpApps, c)
	}

	// Google Workspace — operate on the user's linked Google accounts.
	add(grpGoogle, cmd.NewEmailCmd())
	add(grpGoogle, cmd.NewCalendarCmd())
	add(grpGoogle, cmd.NewDriveCmd())
	add(grpGoogle, cmd.NewTasksCmd())

	// Agents & automation — delegate work / drive the local agent.
	add(grpAgents, cmd.NewRunCmd())
	add(grpAgents, cmd.NewDeskCmd())
	add(grpAgents, cmd.NewHubCmd())
	add(grpAgents, cmd.NewCaptureCmd())
	add(grpAgents, cmd.NewProjectCmd())

	// Discovery — find what apps and surfaces exist, and what's in them.
	//   cards/manifest → what apps can DO · overview → what IS here ·
	//   surface → what's HAPPENING now.
	add(grpDiscing, cmd.NewCardsCmd())
	add(grpDiscing, cmd.NewManifestCmd())
	add(grpDiscing, cmd.NewOverviewCmd())
	add(grpDiscing, cmd.NewSurfaceCmd())

	// Account — who am I, what can I reach.
	add(grpAccount, cmd.NewLoginCmd())
	add(grpAccount, cmd.NewLogoutCmd())
	add(grpAccount, cmd.NewWhoamiCmd())
	add(grpAccount, cmd.NewStatusCmd())

	// CLI & admin — manage the tool itself + privileged hub ops.
	add(grpSystem, admin.NewAdminCmd())
	add(grpSystem, cmd.NewHostCmd())
	add(grpSystem, cmd.NewFeedbackCmd())
	add(grpSystem, cmd.NewSttCmd())
	add(grpSystem, cmd.NewTtsCmd())
	add(grpSystem, cmd.NewConvertCmd())
	add(grpSystem, cmd.NewUpdateCmd())
	add(grpSystem, cmd.NewVersionCmd())

	return root
}

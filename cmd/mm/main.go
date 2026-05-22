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
	"mm-cli/internal/version"
)

func main() {
	root := newRootCmd()

	// Cancel root context on Ctrl-C so in-flight HTTP/WS requests can clean up.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		// Cobra prints its own usage on Args errors; for our RunE errors we
		// want the TS-style "❌ msg" prefix on stderr + exit 1.
		fmt.Fprintf(os.Stderr, "❌ %s\n", err.Error())
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "mm",
		Short:         "Meta-Me CLI",
		Long:          "mm — interact with Meta-Me apps from the terminal.",
		Version:       version.String(),
		SilenceUsage:  true, // don't dump usage on every RunE error
		SilenceErrors: true, // we handle stderr ourselves in main()
	}
	// `mm --version` → "<version.String()>\n" (drops Cobra's "mm version " prefix).
	root.SetVersionTemplate("{{.Version}}\n")

	root.PersistentFlags().Bool("json", false, "Output as JSON")

	// Phase 1 subcommands.
	root.AddCommand(cmd.NewLoginCmd())
	root.AddCommand(cmd.NewLogoutCmd())
	root.AddCommand(cmd.NewWhoamiCmd())

	// Phase 2 subcommands.
	root.AddCommand(cmd.NewCalendarCmd())
	root.AddCommand(cmd.NewTasksCmd())
	root.AddCommand(cmd.NewDriveCmd())
	root.AddCommand(cmd.NewEmailCmd())
	root.AddCommand(cmd.NewSttCmd())
	root.AddCommand(cmd.NewTtsCmd())

	return root
}

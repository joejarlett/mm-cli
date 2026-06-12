package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"mm-cli/internal/host"
)

// NewHostCmd builds `mm host` — this machine's own telemetry surface. The
// `serve` subcommand is the cross-platform Go successor to infra-api.mjs,
// powering the home /server dashboard.
func NewHostCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "host",
		Short: "This machine's telemetry (memory, Docker, services)",
		Long: `mm host — serve this node's own telemetry for the /server dashboard.

The cross-platform (macOS + Linux) successor to infra/scripts/infra-api.mjs:
one Go binary per node instead of a bun runtime + script, so the same surface
works on the Macs and on Linux boxes (jj-server, fedora).`,
	}
	c.AddCommand(newHostServeCmd(), newHostWakeCmd())
	return c
}

func newHostWakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wake <mac>",
		Short: "Send a Wake-on-LAN magic packet (target must be on this LAN)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := host.Wake(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "magic packet sent to %s\n", args[0])
			return nil
		},
	}
}

func newHostServeCmd() *cobra.Command {
	var port int
	var token, peer, wake string
	c := &cobra.Command{
		Use:   "serve",
		Short: "Serve host telemetry over a token-gated HTTP API (127.0.0.1)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = os.Getenv("API_TOKEN")
			}
			if token == "" {
				return fmt.Errorf("no API token — pass --token or set API_TOKEN")
			}
			if peer == "" {
				peer = os.Getenv("PEER_HOST")
			}
			if wake == "" {
				wake = os.Getenv("WAKE_TARGETS")
			}
			return host.Serve(cmd.Context(), port, token, peer, host.ParseWakeTargets(wake))
		},
	}
	c.Flags().IntVar(&port, "port", 8889, "port to listen on (bound to 127.0.0.1)")
	c.Flags().StringVar(&token, "token", "", "API token clients must send as X-API-Token (default: $API_TOKEN)")
	c.Flags().StringVar(&peer, "peer", "", "ssh host whose agent backs /peer/* relay routes (default: $PEER_HOST)")
	c.Flags().StringVar(&wake, "wake", "", `WoL targets "name=mac@probeAddr,..." (default: $WAKE_TARGETS)`)
	return c
}

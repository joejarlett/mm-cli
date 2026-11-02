package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// NewAccountsCmd builds `mm accounts`.
func NewAccountsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accounts",
		Short: "List linked Google accounts, for --account",
		Long: "List the Google accounts linked to you, for the --account flag on\n" +
			"`mm email`, `calendar`, `drive` and `tasks`. It takes the slug or the email.\n\n" +
			"\"needs reconnecting\" means the platform holds no working token for that\n" +
			"account. Its absence doesn't prove the login works: a token Google has\n" +
			"revoked only shows up when a Google call fails.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp wire.HubGoogleAccountsResp
			if err := http.New().Hub(cmd.Context(), "integrations", "googleAccounts", nil, &resp); err != nil {
				return err
			}
			if wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json"); wantJSON {
				out, err := json.MarshalIndent(resp, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal JSON: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			renderAccounts(cmd.OutOrStdout(), resp)
			return nil
		},
	}
}

func renderAccounts(w io.Writer, resp wire.HubGoogleAccountsResp) {
	if len(resp.Accounts) == 0 {
		fmt.Fprintf(w, "No Google accounts linked. Connect one at %s\n", resp.ManageURL)
		return
	}
	relink := false
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, a := range resp.Accounts {
		var notes []string
		if a.IsDefault {
			notes = append(notes, "default")
		}
		if a.NeedsRelink {
			notes = append(notes, "needs reconnecting")
			relink = true
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", strOrDash(a.Email), strOrDash(a.Slug), strings.Join(notes, ", "))
	}
	_ = tw.Flush()
	if relink {
		fmt.Fprintf(w, "\nReconnect at %s\n", resp.ManageURL)
	}
}

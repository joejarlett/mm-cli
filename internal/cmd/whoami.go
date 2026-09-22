package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/auth"
	"mm-cli/internal/config"
)

// NewWhoamiCmd builds `mm whoami`.
func NewWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := auth.Load()
			if err != nil {
				return err
			}
			if s == nil {
				wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
				if wantJSON {
					outMap := map[string]interface{}{
						"authenticated": false,
						"error":         "Not authenticated. Run `mm login` first.",
					}
					out, _ := json.MarshalIndent(outMap, "", "  ")
					fmt.Fprintln(cmd.OutOrStdout(), string(out))
					os.Exit(1)
				}
				fmt.Println("Not authenticated. Run `mm login` first.")
				os.Exit(1)
			}

			wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
			if wantJSON {
				outMap := map[string]interface{}{
					"authenticated": true,
					"userName":      s.UserName,
					"userEmail":     s.UserEmail,
					"userId":        s.UserID,
					"prefix":        s.Prefix,
					"createdAt":     s.CreatedAt,
				}
				out, err := json.MarshalIndent(outMap, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal JSON: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}

			fmt.Printf("User:  %s (%s)\n", s.UserName, s.UserEmail)
			fmt.Printf("ID:    %s\n", s.UserID)
			// Match TS: prefix is the saved 8-char prefix; createdAt sliced to YYYY-MM-DD.
			date := s.CreatedAt
			if len(date) >= 10 {
				date = date[:10]
			}
			fmt.Printf("Token: %s... (created %s)\n", s.Prefix, date)
			return nil
		},
	}
}

// NewLogoutCmd builds `mm logout`.
func NewLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke this machine's key and clear stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, _ := auth.Load()
			if s == nil {
				fmt.Fprintln(os.Stderr, "Not authenticated.")
				os.Exit(1)
			}
			// Revoke first, while the token still exists to present. Logging
			// out used to delete the file and leave the key live on the
			// server. Best effort: offline, the local file still goes and the
			// user is told the key may outlive it.
			revoked := revokeOwnToken(s.Token)
			if err := auth.Clear(); err != nil {
				return err
			}
			if revoked {
				fmt.Printf("Logged out and revoked this key. (Was %s)\n", s.UserName)
			} else {
				fmt.Printf("Logged out. (Was %s)\n", s.UserName)
				fmt.Fprintln(os.Stderr, "Could not reach auth to revoke the key; revoke it at https://meta-me.uk/settings/api.")
			}
			return nil
		},
	}
}

// revokeOwnToken asks auth to revoke the token presented, and nothing else.
// A 401 means it was already dead, which is the outcome logout wants.
func revokeOwnToken(token string) bool {
	req, err := http.NewRequest(http.MethodPost, config.Load().AuthURL+"/api/cli/revoke", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusOK || res.StatusCode == http.StatusUnauthorized
}

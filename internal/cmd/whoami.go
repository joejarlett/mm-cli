package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"mm-cli/internal/auth"
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
					cmd.Println(string(out))
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
				cmd.Println(string(out))
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
		Short: "Clear stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, _ := auth.Load()
			if s == nil {
				fmt.Fprintln(os.Stderr, "Not authenticated.")
				os.Exit(1)
			}
			if err := auth.Clear(); err != nil {
				return err
			}
			fmt.Printf("Logged out. (Was %s)\n", s.UserName)
			return nil
		},
	}
}

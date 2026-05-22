package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"mm-cli/internal/auth"
)

// NewStatusCmd builds `mm status`. Mirrors src/commands/status.ts.
func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show auth status and available apps",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, _ := auth.Load()
			wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
			if wantJSON {
				var outMap map[string]interface{}
				if s == nil {
					outMap = map[string]interface{}{
						"authenticated": false,
						"apps": []map[string]string{
							{"slug": "kb", "name": "Knowledge Base", "description": "search, read, manage documents"},
							{"slug": "crm", "name": "CRM", "description": "contacts, projects, interactions"},
						},
					}
				} else {
					outMap = map[string]interface{}{
						"authenticated": true,
						"userName":      s.UserName,
						"userEmail":     s.UserEmail,
						"prefix":        s.Prefix,
						"apps": []map[string]string{
							{"slug": "kb", "name": "Knowledge Base", "description": "search, read, manage documents"},
							{"slug": "crm", "name": "CRM", "description": "contacts, projects, interactions"},
						},
					}
				}
				out, err := json.MarshalIndent(outMap, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal JSON: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}

			if s == nil {
				fmt.Println("Not authenticated. Run `mm login` first.")
				fmt.Println()
				fmt.Println("Available apps:")
				fmt.Println("  kb     Knowledge Base — search, read, manage documents")
				fmt.Println("  crm    CRM — contacts, projects, interactions")
				fmt.Println()
				fmt.Println("Run `mm login` to get started.")
				return nil
			}
			fmt.Printf("Authenticated as: %s (%s)\n", s.UserName, s.UserEmail)
			fmt.Printf("Token: %s...\n", s.Prefix)
			fmt.Println()
			fmt.Println("Available apps:")
			fmt.Println("  kb     Knowledge Base — search, read, manage documents")
			fmt.Println("  crm    CRM — contacts, projects, interactions")
			fmt.Println()
			fmt.Println("Try:")
			fmt.Println("  mm kb find \"machine learning\"")
			fmt.Println("  mm crm contacts")
			return nil
		},
	}
}

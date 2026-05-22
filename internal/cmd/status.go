package cmd

import (
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

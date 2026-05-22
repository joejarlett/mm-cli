package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"mm-cli/internal/update"
	"mm-cli/internal/version"
)

// NewUpdateCmd builds `mm update [--check] [--version X.Y.Z]`.
func NewUpdateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "update",
		Short: "Fetch + replace the running binary (or --check for a one-liner status)",
		RunE:  runUpdate,
	}
	c.Flags().Bool("check", false, "Only check if a newer version is available")
	c.Flags().String("version", "", "Force a specific version tag")
	return c
}

// NewVersionCmd builds `mm version` (alongside --version).
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Println(version.String())
		},
	}
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")
	tag, _ := cmd.Flags().GetString("version")

	if checkOnly {
		res, err := update.Check(cmd.Context())
		if err != nil {
			return err
		}
		if res.Newer {
			fmt.Printf("mm update available: v%s → %s  (run: mm update)\n", res.Current, res.Latest)
		} else {
			fmt.Printf("mm is up to date (%s)\n", res.Current)
		}
		return nil
	}
	return update.Apply(cmd.Context(), tag)
}

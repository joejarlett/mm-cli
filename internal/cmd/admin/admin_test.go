package admin

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func TestAdminHelpAndArgumentValidation(t *testing.T) {
	runCmd := func(cmd *cobra.Command, args ...string) (string, error) {
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return buf.String(), err
	}

	t.Run("admin apps help rejection", func(t *testing.T) {
		c := newAppsCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("admin health help rejection", func(t *testing.T) {
		c := newHealthCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("admin errors help rejection", func(t *testing.T) {
		c := newErrorsCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})
}

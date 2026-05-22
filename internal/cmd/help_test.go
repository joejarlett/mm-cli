package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestHelpAndArgumentValidation(t *testing.T) {
	// A helper to execute a cobra command with specific args and capture output/error
	runCmd := func(cmd *cobra.Command, args ...string) (string, error) {
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return buf.String(), err
	}

	t.Run("tts help", func(t *testing.T) {
		c := NewTtsCmd()
		out, err := runCmd(c, "help")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Stream WAV to stdout") {
			t.Errorf("expected tts help menu, got: %s", out)
		}
	})

	t.Run("stt help", func(t *testing.T) {
		c := NewSttCmd()
		out, err := runCmd(c, "help")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Pass a file path") {
			t.Errorf("expected stt help menu, got: %s", out)
		}
	})

	t.Run("cards help", func(t *testing.T) {
		c := NewCardsCmd()
		out, err := runCmd(c, "help")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Capability matrix") {
			t.Errorf("expected cards help menu, got: %s", out)
		}
	})

	t.Run("manifest help", func(t *testing.T) {
		c := NewManifestCmd()
		out, err := runCmd(c, "help")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Wire-level manifest") {
			t.Errorf("expected manifest help menu, got: %s", out)
		}
	})

	t.Run("whoami help rejection", func(t *testing.T) {
		c := NewWhoamiCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("status help rejection", func(t *testing.T) {
		c := NewStatusCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("logout help rejection", func(t *testing.T) {
		c := NewLogoutCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("update help rejection", func(t *testing.T) {
		c := NewUpdateCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("version help rejection", func(t *testing.T) {
		c := NewVersionCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("calendar list help rejection", func(t *testing.T) {
		c := newCalendarListCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("tasks list help rejection", func(t *testing.T) {
		c := newTasksListCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("crm surface help rejection", func(t *testing.T) {
		c := newCrmSurfaceCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("crm projects help rejection", func(t *testing.T) {
		c := newCrmProjectsCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("drive ls help rejection", func(t *testing.T) {
		c := newDriveListCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("kb collections help rejection", func(t *testing.T) {
		c := newKbCollectionsCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("kb status help rejection", func(t *testing.T) {
		c := newKbStatusCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("email list help rejection", func(t *testing.T) {
		c := newEmailListCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("email send help rejection", func(t *testing.T) {
		c := newEmailSendCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("email draft help rejection", func(t *testing.T) {
		c := newEmailDraftCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("chat list help rejection", func(t *testing.T) {
		c := newChatListCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("chat projects help rejection", func(t *testing.T) {
		c := newChatProjectsCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("chat nodes help rejection", func(t *testing.T) {
		c := newChatNodesCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("chat models help rejection", func(t *testing.T) {
		c := newChatModelsCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})

	t.Run("project list help rejection", func(t *testing.T) {
		c := newProjectListCmd()
		_, err := runCmd(c, "help")
		if err == nil {
			t.Error("expected error due to cobra.NoArgs, but got nil")
		}
	})
}

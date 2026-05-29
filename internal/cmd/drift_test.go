package cmd_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/spf13/cobra"

	"mm-cli/internal/cmd"
	"mm-cli/internal/cmd/admin"
)

func dummyRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "mm"}
	root.AddCommand(cmd.NewLoginCmd())
	root.AddCommand(cmd.NewLogoutCmd())
	root.AddCommand(cmd.NewWhoamiCmd())
	root.AddCommand(cmd.NewStatusCmd())
	root.AddCommand(cmd.NewCalendarCmd())
	root.AddCommand(cmd.NewTasksCmd())
	root.AddCommand(cmd.NewDriveCmd())
	root.AddCommand(cmd.NewEmailCmd())
	root.AddCommand(cmd.NewSttCmd())
	root.AddCommand(cmd.NewTtsCmd())
	root.AddCommand(cmd.NewDeskCmd())
	root.AddCommand(cmd.NewHubCmd())
	root.AddCommand(cmd.NewProjectCmd())
	root.AddCommand(cmd.NewCardsCmd())
	root.AddCommand(cmd.NewManifestCmd())
	root.AddCommand(cmd.NewKbCmd())
	root.AddCommand(cmd.NewCrmCmd())
	root.AddCommand(cmd.NewUpdateCmd())
	root.AddCommand(cmd.NewVersionCmd())
	root.AddCommand(cmd.NewFeedbackCmd())
	root.AddCommand(cmd.NewCaptureCmd())
	root.AddCommand(cmd.NewRunCmd())
	root.AddCommand(admin.NewAdminCmd())
	return root
}

func TestCliDrift(t *testing.T) {
	// 1. Locate and read src/index.ts
	path := filepath.Join("..", "..", "src", "index.ts")
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read src/index.ts: %v", err)
	}
	content := string(contentBytes)

	// 2. Extract cases using regex
	// Matches `case 'something':`
	re := regexp.MustCompile(`case\s+'([^']+)'\s*:`)
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		t.Fatalf("No command cases found in src/index.ts")
	}

	tsCommands := make(map[string]bool)
	for _, m := range matches {
		tsCommands[m[1]] = true
	}

	// 3. Build Go command set
	root := dummyRootCmd()
	goCommands := make(map[string]bool)

	// Collect top-level Go commands and their aliases
	for _, c := range root.Commands() {
		goCommands[c.Name()] = true
		for _, alias := range c.Aliases {
			goCommands[alias] = true
		}

		// If it is the admin command, collect its subcommands
		if c.Name() == "admin" {
			for _, sub := range c.Commands() {
				goCommands[sub.Name()] = true
				for _, alias := range sub.Aliases {
					goCommands[alias] = true
				}
			}
		}
	}

	// 4. Assert TS commands exist in Go
	for tsCmd := range tsCommands {
		// Exceptions:
		// - 'v2' is a deprecated alias in TS, dropped in Go port
		// - 'chat' renamed to 'desk' in Go port, alias intentionally removed
		if tsCmd == "v2" || tsCmd == "chat" {
			continue
		}

		if !goCommands[tsCmd] {
			t.Errorf("Drift detected: TS command %q is missing from Go CLI", tsCmd)
		}
	}

	// 5. Assert Go commands exist in TS (or are documented exceptions)
	// New commands introduced in Go or structural helper commands
	newGoExceptions := map[string]bool{
		"admin":    true, // Namespace command standardizing the admin verbs
		"desk":     true, // Renamed from 'chat' in Go port
		"hub":      true, // Hub meta-agent conversations (new in Go port)
		"feedback": true, // Friction/bug reporting command
		"capture":  true, // Inbox capture command
		"update":   true, // Self-updater command
		"version":  true, // Print version command
	}

	for goCmd := range goCommands {
		if newGoExceptions[goCmd] {
			continue
		}
		// If it's a sub-command of admin (like sql, apps, app, health, errors, error),
		// we expect it to be a top-level command in TS.
		if !tsCommands[goCmd] {
			t.Errorf("Drift detected: Go command/alias %q is missing from TS CLI", goCmd)
		}
	}
}

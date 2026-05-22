package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/http"
	"mm-cli/internal/nldate"
	"mm-cli/internal/wire"
)

// NewTasksCmd builds the `mm tasks` tree.
func NewTasksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "Google Tasks — list, add, complete",
		Args:  cobra.NoArgs,
		RunE:  runTasksList,
	}
	cmd.Flags().Int("days", 7, "Window in days")
	cmd.Flags().Bool("all", false, "Include all lists")
	cmd.Flags().String("account", "", "Pick a linked Google account")
	cmd.AddCommand(newTasksListCmd(), newTasksAddCmd(), newTasksDoneCmd())
	return cmd
}

func newTasksListCmd() *cobra.Command {
	c := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List tasks", Args: cobra.NoArgs, RunE: runTasksList}
	c.Flags().Int("days", 7, "Window in days")
	c.Flags().Bool("all", false, "Include all lists")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func newTasksAddCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "add [title]",
		Aliases: []string{"new"},
		Short:   "Add a task",
		Args:    cobra.MinimumNArgs(1),
		RunE:    runTasksAdd,
	}
	c.Flags().String("due", "", "Due date (NL or YYYY-MM-DD)")
	c.Flags().String("list", "", "Task list title")
	c.Flags().String("list-id", "", "Task list id")
	c.Flags().String("notes", "", "Long description")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func newTasksDoneCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "done [task-id]",
		Aliases: []string{"complete"},
		Short:   "Mark a task complete",
		Args:    cobra.ExactArgs(1),
		RunE:    runTasksDone,
	}
	c.Flags().String("list-id", "", "Task list id (required)")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func runTasksList(cmd *cobra.Command, _ []string) error {
	days, _ := cmd.Flags().GetInt("days")
	all, _ := cmd.Flags().GetBool("all")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	req := map[string]any{}
	if days != 7 && days > 0 {
		req["days"] = days
	}
	if all {
		req["all"] = true
	}
	if account != "" {
		req["accountSlug"] = account
	}

	client := http.New()
	var resp wire.HubTasksListResp
	if err := client.Hub(cmd.Context(), "tasks", "list", req, &resp); err != nil {
		return err
	}

	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	total := 0
	for _, g := range resp.Groups {
		total += len(g.Tasks)
	}
	if total == 0 {
		fmt.Println("Nothing pending. ✨")
		return nil
	}
	for _, g := range resp.Groups {
		if len(g.Tasks) == 0 {
			continue
		}
		fmt.Printf("\n### %s (%d)\n", g.ListTitle, len(g.Tasks))
		for _, t := range g.Tasks {
			due := ""
			if t.Due != "" {
				due = " — *due " + fmtDue(t.Due) + "*"
			}
			fmt.Printf("- **%s**%s\n", t.Title, due)
			fmt.Printf("  - ID: `%s` | List: `%s`\n", t.ID, g.ListID)
			if t.Notes != "" {
				lines := strings.ReplaceAll(t.Notes, "\n", "\n    ")
				fmt.Printf("    > %s\n", lines)
			}
		}
	}
	return nil
}

func runTasksAdd(cmd *cobra.Command, args []string) error {
	title := strings.TrimSpace(strings.Join(args, " "))
	if title == "" {
		return fmt.Errorf("title is required")
	}
	due, _ := cmd.Flags().GetString("due")
	listFlag, _ := cmd.Flags().GetString("list")
	listID, _ := cmd.Flags().GetString("list-id")
	notes, _ := cmd.Flags().GetString("notes")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	req := map[string]any{"title": title}
	var dueDisplay string
	if due != "" {
		parsed, err := nldate.ParseDate(due, time.Now())
		if err != nil {
			return err
		}
		req["due"] = parsed.ISO
		dueDisplay = parsed.ISO
	}
	if listFlag != "" {
		req["listTitle"] = listFlag
	}
	if listID != "" {
		req["listId"] = listID
	}
	if notes != "" {
		req["notes"] = notes
	}
	if account != "" {
		req["accountSlug"] = account
	}

	client := http.New()
	var resp wire.HubTasksAddResp
	if err := client.Hub(cmd.Context(), "tasks", "add", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("✓ Added to %s\n  %s\n", resp.ListTitle, title)
	if dueDisplay != "" {
		fmt.Printf("  due %s\n", dueDisplay)
	}
	return nil
}

func runTasksDone(cmd *cobra.Command, args []string) error {
	taskID := args[0]
	listID, _ := cmd.Flags().GetString("list-id")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	if listID == "" {
		return fmt.Errorf("Missing --list-id. `mm tasks` shows ids for each task — the line below the title.")
	}
	req := map[string]any{"listId": listID, "taskId": taskID}
	if account != "" {
		req["accountSlug"] = account
	}
	client := http.New()
	var resp wire.HubTasksCompleteResp
	if err := client.Hub(cmd.Context(), "tasks", "complete", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Println("✓ Done.")
	return nil
}

func fmtDue(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
		if err != nil {
			return iso
		}
	}
	return t.Format("2 Jan")
}

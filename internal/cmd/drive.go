package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// NewDriveCmd builds the `mm drive` tree.
func NewDriveCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "drive", Short: "Google Drive — list, doc-from-markdown, rename/move"}
	cmd.AddCommand(newDriveListCmd(), newDriveDocCmd(), newDriveMoveCmd())
	return cmd
}

func newDriveListCmd() *cobra.Command {
	c := &cobra.Command{Use: "ls", Aliases: []string{"list"}, Short: "List files (filter with --q)", RunE: runDriveList}
	c.Flags().String("q", "", "Drive search query")
	c.Flags().Int("max", 20, "Max results")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func newDriveDocCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "doc [name]",
		Short: "Create a Google Doc from a markdown file (or stdin)",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runDriveDoc,
	}
	c.Flags().String("file", "", "Path to markdown file (or pipe via stdin)")
	c.Flags().String("text", "", "Inline content")
	c.Flags().String("mime", "text/markdown", "Source mime: text/markdown|text/plain|text/html")
	c.Flags().String("folder", "", "Target folder id")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func newDriveMoveCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "mv [id]",
		Aliases: []string{"rename", "move"},
		Short:   "Rename and/or move a file",
		Args:    cobra.ExactArgs(1),
		RunE:    runDriveMove,
	}
	c.Flags().String("name", "", "New name")
	c.Flags().String("parent", "", "Add a parent (move into)")
	c.Flags().String("unparent", "", "Remove a parent (move out of)")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func runDriveList(cmd *cobra.Command, _ []string) error {
	q, _ := cmd.Flags().GetString("q")
	max, _ := cmd.Flags().GetInt("max")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	req := map[string]any{}
	if q != "" {
		req["q"] = q
	}
	if max != 20 && max > 0 {
		req["max"] = max
	}
	if account != "" {
		req["accountSlug"] = account
	}
	client := http.New()
	var resp wire.HubDriveListResp
	if err := client.Hub(cmd.Context(), "drive", "list", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(resp.Files) == 0 {
		fmt.Println("No files match.")
		return nil
	}
	for _, f := range resp.Files {
		when := padLeft(fmtWhen(f.ModifiedTime), 4)
		kind := padRight(mimeLabel(f.MimeType), 6)
		fmt.Printf("  %s  %s  %s\n", when, kind[:6], f.Name)
		link := f.WebViewLink
		if link == "" {
			link = "(no link)"
		}
		fmt.Printf("        %s\n", link)
	}
	return nil
}

func runDriveDoc(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(strings.Join(args, " "))
	if name == "" {
		return fmt.Errorf("name is required")
	}
	file, _ := cmd.Flags().GetString("file")
	text, _ := cmd.Flags().GetString("text")
	mime, _ := cmd.Flags().GetString("mime")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	var content string
	switch {
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("Couldn't read --file %s: %w", file, err)
		}
		content = string(b)
	case text != "":
		content = text
	default:
		// Try stdin.
		fi, _ := os.Stdin.Stat()
		if (fi.Mode() & os.ModeCharDevice) == 0 {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			content = string(b)
		} else {
			return fmt.Errorf("No content. Pass --file PATH, --text \"...\", or pipe markdown via stdin.")
		}
	}

	req := map[string]any{
		"name":       name,
		"content":    content,
		"sourceMime": mime,
	}
	if account != "" {
		req["accountSlug"] = account
	}
	client := http.New()
	var resp wire.HubDriveCreateDocResp
	if err := client.Hub(cmd.Context(), "drive", "createDoc", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("✓ Created Google Doc: %s\n", resp.Name)
	if resp.WebViewLink != nil && *resp.WebViewLink != "" {
		fmt.Printf("  %s\n", *resp.WebViewLink)
	}
	return nil
}

func runDriveMove(cmd *cobra.Command, args []string) error {
	id := args[0]
	name, _ := cmd.Flags().GetString("name")
	parent, _ := cmd.Flags().GetString("parent")
	unparent, _ := cmd.Flags().GetString("unparent")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	req := map[string]any{"fileId": id}
	if name != "" {
		req["name"] = name
	}
	if parent != "" {
		req["addParents"] = []string{parent}
	}
	if unparent != "" {
		req["removeParents"] = []string{unparent}
	}
	if account != "" {
		req["accountSlug"] = account
	}
	if name == "" && parent == "" && unparent == "" {
		return fmt.Errorf("Nothing to do — pass at least one of --name, --parent, --unparent.")
	}

	client := http.New()
	var resp wire.HubDriveUpdateResp
	if err := client.Hub(cmd.Context(), "drive", "update", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("✓ %s\n", resp.Name)
	if resp.WebViewLink != nil && *resp.WebViewLink != "" {
		fmt.Printf("  %s\n", *resp.WebViewLink)
	}
	return nil
}

func fmtWhen(iso string) string {
	if iso == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm", int(diff.Minutes()+0.5))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh", int(diff.Hours()+0.5))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(diff.Hours()/24+0.5))
	}
	return t.Format("2 Jan")
}

func mimeLabel(m string) string {
	switch {
	case m == "application/vnd.google-apps.document":
		return "doc"
	case m == "application/vnd.google-apps.spreadsheet":
		return "sheet"
	case m == "application/vnd.google-apps.presentation":
		return "slide"
	case m == "application/vnd.google-apps.folder":
		return "folder"
	case strings.HasPrefix(m, "image/"):
		return "img"
	case m == "application/pdf":
		return "pdf"
	}
	if idx := strings.LastIndex(m, "/"); idx >= 0 {
		return m[idx+1:]
	}
	return m
}

func padLeft(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}

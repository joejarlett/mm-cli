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
	cmd := &cobra.Command{Use: "drive", Short: "Google Drive — list, read, doc-from-markdown, rename/move"}
	cmd.AddCommand(newDriveListCmd(), newDriveReadCmd(), newDriveGetCmd(), newDriveDownloadCmd(), newDriveDocCmd(), newDriveMoveCmd())
	return cmd
}

func newDriveReadCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "read [id|url]",
		Short: "Read (export) a Google Doc/Sheet/Slide as text",
		Long: "Export a Google-native file's body to text and print it (or write it with --out).\n" +
			"Accepts a bare file id or a pasted Docs/Sheets/Slides URL.\n\n" +
			"Formats: txt (default), html, pdf (use with --out). md is accepted but the\n" +
			"current gateway export path returns 503 for text/markdown — use txt for now.",
		Args: cobra.ExactArgs(1),
		RunE: runDriveRead,
	}
	c.Flags().String("as", "txt", "Export format: txt|html|pdf|md (md needs backend support — see note)")
	c.Flags().String("out", "", "Write to this path instead of stdout")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func newDriveGetCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "get [id|url]",
		Short: "Show file metadata (name, type, size, parents)",
		Args:  cobra.ExactArgs(1),
		RunE:  runDriveGet,
	}
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func newDriveDownloadCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "download [id|url]",
		Short: "Download a non-Google file's raw content (PDF, .md, .csv …)",
		Args:  cobra.ExactArgs(1),
		RunE:  runDriveDownload,
	}
	c.Flags().String("out", "", "Write to this path instead of stdout")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func newDriveListCmd() *cobra.Command {
	c := &cobra.Command{Use: "ls", Aliases: []string{"list"}, Short: "List files (filter with --q)", Args: cobra.NoArgs, RunE: runDriveList}
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
		when := fmtWhen(f.ModifiedTime)
		kind := mimeLabel(f.MimeType)
		link := f.WebViewLink
		if link != "" {
			fmt.Printf("- **%s** | `%s` | [%s](%s)\n", when, kind, f.Name, link)
		} else {
			fmt.Printf("- **%s** | `%s` | %s\n", when, kind, f.Name)
		}
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

func runDriveRead(cmd *cobra.Command, args []string) error {
	id := driveFileID(args[0])
	asFlag, _ := cmd.Flags().GetString("as")
	outPath, _ := cmd.Flags().GetString("out")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	mime, err := mimeForAs(asFlag)
	if err != nil {
		return err
	}
	req := map[string]any{"fileId": id, "mimeType": mime}
	if account != "" {
		req["accountSlug"] = account
	}
	client := http.New()
	var resp wire.HubDriveExportResp
	if err := client.Hub(cmd.Context(), "drive", "export", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if outPath != "" {
		if err := os.WriteFile(outPath, []byte(resp.Content), 0o644); err != nil {
			return fmt.Errorf("Couldn't write %s: %w", outPath, err)
		}
		fmt.Fprintf(os.Stderr, "✓ Wrote %s (%s)\n", outPath, resp.MimeType)
		return nil
	}
	fmt.Print(resp.Content)
	return nil
}

func runDriveGet(cmd *cobra.Command, args []string) error {
	id := driveFileID(args[0])
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	req := map[string]any{
		"fileId": id,
		"fields": "id,name,mimeType,modifiedTime,webViewLink,parents,size",
	}
	if account != "" {
		req["accountSlug"] = account
	}
	client := http.New()
	var resp wire.HubDriveGetResp
	if err := client.Hub(cmd.Context(), "drive", "get", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("%s\n", resp.Name)
	fmt.Printf("  type:     %s\n", mimeLabel(resp.MimeType))
	if resp.Size != "" {
		fmt.Printf("  size:     %s\n", resp.Size)
	}
	if resp.ModifiedTime != "" {
		fmt.Printf("  modified: %s\n", fmtWhen(resp.ModifiedTime))
	}
	if resp.WebViewLink != "" {
		fmt.Printf("  link:     %s\n", resp.WebViewLink)
	}
	if len(resp.Parents) > 0 {
		fmt.Printf("  parents:  %s\n", strings.Join(resp.Parents, ", "))
	}
	return nil
}

func runDriveDownload(cmd *cobra.Command, args []string) error {
	id := driveFileID(args[0])
	outPath, _ := cmd.Flags().GetString("out")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	req := map[string]any{"fileId": id}
	if account != "" {
		req["accountSlug"] = account
	}
	client := http.New()
	var resp wire.HubDriveDownloadResp
	if err := client.Hub(cmd.Context(), "drive", "download", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if outPath != "" {
		if err := os.WriteFile(outPath, []byte(resp.Content), 0o644); err != nil {
			return fmt.Errorf("Couldn't write %s: %w", outPath, err)
		}
		fmt.Fprintf(os.Stderr, "✓ Wrote %s\n", outPath)
		return nil
	}
	fmt.Print(resp.Content)
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

// mimeForAs maps the --as shorthand to an export mime type.
func mimeForAs(as string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(as)) {
	case "", "md", "markdown":
		return "text/markdown", nil
	case "txt", "text", "plain":
		return "text/plain", nil
	case "html":
		return "text/html", nil
	case "pdf":
		return "application/pdf", nil
	}
	// Escape hatch: accept a full mime literal (e.g. an office export mime).
	if strings.Contains(as, "/") {
		return as, nil
	}
	return "", fmt.Errorf("unknown --as %q (use md|txt|html|pdf, or a full mime type)", as)
}

// driveFileID accepts a bare id or a pasted Drive/Docs URL and returns the id.
// e.g. https://docs.google.com/document/d/<id>/edit → <id>
func driveFileID(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		return s
	}
	// .../d/<id>/... form (Docs, Sheets, Slides, Drive file links)
	if i := strings.Index(s, "/d/"); i >= 0 {
		rest := s[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			rest = rest[:j]
		}
		if j := strings.IndexAny(rest, "?#"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	// ...?id=<id> form (open?id=, uc?id=)
	if i := strings.Index(s, "id="); i >= 0 {
		rest := s[i+3:]
		if j := strings.IndexAny(rest, "&#"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	return s
}

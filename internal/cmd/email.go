package cmd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// NewEmailCmd builds the `mm email` tree.
func NewEmailCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "email", Short: "Gmail inbox + platform email log"}
	cmd.AddCommand(
		newEmailListCmd(), newEmailGetCmd(), newEmailSendCmd(),
		newEmailDraftCmd(), newEmailResendCmd(), newEmailSearchCmd(), newEmailReadCmd(),
	)
	return cmd
}

func newEmailListCmd() *cobra.Command {
	c := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List recent platform-sent messages", Args: cobra.NoArgs, RunE: runEmailList}
	c.Flags().String("status", "", "Filter by status")
	c.Flags().String("template", "", "Filter by template name")
	c.Flags().String("q", "", "Free-text filter")
	c.Flags().Int("limit", 0, "Max rows")
	return c
}

func newEmailGetCmd() *cobra.Command {
	return &cobra.Command{Use: "get [id]", Aliases: []string{"show"}, Short: "Show a platform-log row", Args: cobra.ExactArgs(1), RunE: runEmailGet}
}

func newEmailSendCmd() *cobra.Command {
	c := &cobra.Command{Use: "send", Short: "Send a new message", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return runEmailSend(cmd, args, false) }}
	addSendFlags(c)
	return c
}

func newEmailDraftCmd() *cobra.Command {
	c := &cobra.Command{Use: "draft", Short: "Save a draft without sending", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return runEmailSend(cmd, args, true) }}
	addSendFlags(c)
	return c
}

func newEmailResendCmd() *cobra.Command {
	return &cobra.Command{Use: "resend [id]", Short: "Resend a previous platform email", Args: cobra.ExactArgs(1), RunE: runEmailResend}
}

func newEmailSearchCmd() *cobra.Command {
	c := &cobra.Command{Use: "search [query...]", Aliases: []string{"find"}, Short: "Search Gmail inbox", RunE: runEmailSearch}
	c.Flags().String("q", "", "Gmail query string")
	c.Flags().Int("max", 20, "Max results")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func newEmailReadCmd() *cobra.Command {
	return &cobra.Command{Use: "read [id]", Aliases: []string{"open"}, Short: "Read full message body", Args: cobra.ExactArgs(1), RunE: runEmailRead}
}

func addSendFlags(c *cobra.Command) {
	c.Flags().String("to", "", "Recipient (required)")
	c.Flags().String("subject", "", "Subject line")
	c.Flags().String("body", "", "Body (HTML or plain)")
	c.Flags().String("text", "", "Plain-text body (default: stripped from --body)")
	c.Flags().String("template", "", "Template name")
}

// ─── Implementations ───────────────────────────────────────────────────

func runEmailList(cmd *cobra.Command, _ []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	req := map[string]any{}
	for _, f := range []string{"status", "template", "q"} {
		v, _ := cmd.Flags().GetString(f)
		if v != "" {
			req[f] = v
		}
	}
	if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 {
		req["limit"] = limit
	}

	client := http.New()
	var resp wire.HubEmailListResp
	if err := client.Hub(cmd.Context(), "email", "list", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(resp.Rows) == 0 {
		fmt.Println("No emails match.")
		return nil
	}
	for _, r := range resp.Rows {
		template := "—"
		if r.Template != nil {
			template = *r.Template
		}
		fmt.Printf("  %s  %s  %s  %s  %s  %s\n",
			r.ID[:min(len(r.ID), 8)],
			padRight(r.Status, 6),
			padRight(truncString(template, 22), 22),
			padRight(truncString(r.ToAddress, 28), 28),
			fmtRelative(r.CreatedAt),
			r.Subject)
	}
	if resp.NextCursor != nil && *resp.NextCursor != "" {
		fmt.Println()
		fmt.Printf("  …more. Resume with --cursor='%s' (not yet exposed in CLI; use the web UI to page deeper).\n", *resp.NextCursor)
	}
	return nil
}

func runEmailGet(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := http.New()
	var resp wire.HubEmailGetResp
	if err := client.Hub(cmd.Context(), "email", "get", map[string]any{"id": args[0]}, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("ID:        %s\n", resp.ID)
	fmt.Printf("Status:    %s\n", resp.Status)
	fmt.Printf("Template:  %s\n", strOrDash(resp.Template))
	fmt.Printf("Trigger:   %s\n", strOrDash(resp.TriggeredBy))
	fmt.Printf("To:        %s\n", resp.ToAddress)
	fmt.Printf("From:      %s\n", resp.FromAddress)
	fmt.Printf("Subject:   %s\n", resp.Subject)
	fmt.Printf("Created:   %s\n", resp.CreatedAt)
	if resp.SentAt != nil && *resp.SentAt != "" {
		fmt.Printf("Sent:      %s\n", *resp.SentAt)
	}
	if resp.FailedAt != nil && *resp.FailedAt != "" {
		fmt.Printf("Failed:    %s\n", *resp.FailedAt)
	}
	if resp.MessageID != nil && *resp.MessageID != "" {
		fmt.Printf("Message:   %s\n", *resp.MessageID)
	}
	if resp.ParentID != nil && *resp.ParentID != "" {
		fmt.Printf("Resend of: %s\n", *resp.ParentID)
	}
	if resp.UserID != nil && *resp.UserID != "" {
		fmt.Printf("User:      %s\n", *resp.UserID)
	}
	if len(resp.TemplateParams) > 0 {
		raw, _ := json.Marshal(resp.TemplateParams)
		fmt.Printf("Params:    %s\n", string(raw))
	}
	if resp.Error != nil && *resp.Error != "" {
		fmt.Println()
		fmt.Println("Error:")
		fmt.Println("  " + strings.ReplaceAll(*resp.Error, "\n", "\n  "))
	}
	fmt.Println()
	fmt.Println("--- Text body ---")
	fmt.Println(resp.BodyText)
	return nil
}

func runEmailSend(cmd *cobra.Command, _ []string, draftOnly bool) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	to, _ := cmd.Flags().GetString("to")
	subject, _ := cmd.Flags().GetString("subject")
	body, _ := cmd.Flags().GetString("body")
	text, _ := cmd.Flags().GetString("text")
	template, _ := cmd.Flags().GetString("template")

	if to == "" || subject == "" || body == "" {
		return fmt.Errorf("Usage: mm email send --to <addr> --subject <s> --body <html> [--text <plain>] [--template <name>]")
	}
	if text == "" {
		text = stripHTML(body)
	}

	client := http.New()
	var created wire.HubEmailCreateResp
	createReq := map[string]any{"to": to, "subject": subject, "html": body, "text": text}
	if template != "" {
		createReq["template"] = template
	}
	if err := client.Hub(cmd.Context(), "email", "create", createReq, &created); err != nil {
		return err
	}

	if draftOnly {
		if wantJSON {
			out, _ := json.MarshalIndent(map[string]string{"id": created.ID, "status": "draft"}, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		fmt.Printf("✓ Draft saved: %s\n", created.ID)
		fmt.Printf("  Preview: mm email get %s\n", created.ID)
		fmt.Printf("  Send:    mm email send <re-run> or via /admin/emails/%s\n", created.ID)
		return nil
	}

	var sent wire.HubEmailSendResp
	if err := client.Hub(cmd.Context(), "email", "send", map[string]any{"id": created.ID}, &sent); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(map[string]any{"id": created.ID, "success": sent.Success, "error": sent.Error}, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if sent.Success {
		fmt.Printf("✓ Sent. Row: %s\n  Detail: mm email get %s\n", created.ID, created.ID)
		return nil
	}
	return fmt.Errorf("Created row %s but send failed: %s", created.ID, sent.Error)
}

func runEmailResend(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := http.New()
	var resp wire.HubEmailResendResp
	if err := client.Hub(cmd.Context(), "email", "resend", map[string]any{"id": args[0]}, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if resp.Success {
		fmt.Printf("✓ Resent. New row: %s\n", resp.NewID)
		return nil
	}
	return fmt.Errorf("Resend row %s created but SMTP failed: %s", resp.NewID, resp.Error)
}

func runEmailSearch(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	q, _ := cmd.Flags().GetString("q")
	if q == "" && len(args) > 0 {
		q = strings.Join(args, " ")
	}
	max, _ := cmd.Flags().GetInt("max")
	account, _ := cmd.Flags().GetString("account")
	req := map[string]any{}
	if q != "" {
		req["q"] = q
	}
	if max != 20 && max > 0 {
		req["maxResults"] = max
	}
	if account != "" {
		req["accountSlug"] = account
	}
	client := http.New()
	var resp wire.HubInboxSearchResp
	if err := client.Hub(cmd.Context(), "email", "search", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(resp.Messages) == 0 {
		fmt.Println("No messages match.")
		return nil
	}
	for _, m := range resp.Messages {
		mark := " "
		if m.Unread {
			mark = "●"
		}
		when := padRight(fmtRelative(m.Date), 8)
		from := padRight(truncString(stripEmailName(m.From), 28), 28)
		subj := truncString(orFallback(m.Subject, "(no subject)"), 60)
		fmt.Printf("%s %s  %s  %s  %s\n", mark, m.ID, when, from, subj)
		if m.Snippet != "" {
			fmt.Printf("             %s\n", truncString(m.Snippet, 100))
		}
	}
	if resp.AccountSlug != nil && *resp.AccountSlug != "" {
		fmt.Printf("\n  (account: %s)\n", *resp.AccountSlug)
	}
	return nil
}

func runEmailRead(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := http.New()
	var resp wire.HubInboxReadResp
	if err := client.Hub(cmd.Context(), "email", "read", map[string]any{"id": args[0]}, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("From:    %s\n", resp.From)
	fmt.Printf("To:      %s\n", resp.To)
	if resp.CC != "" {
		fmt.Printf("Cc:      %s\n", resp.CC)
	}
	fmt.Printf("Date:    %s\n", resp.Date)
	fmt.Printf("Subject: %s\n", resp.Subject)
	if len(resp.Labels) > 0 {
		fmt.Printf("Labels:  %s\n", strings.Join(resp.Labels, ", "))
	}
	fmt.Println()
	body := resp.Body
	if body == "" {
		body = resp.Snippet
	}
	if body == "" {
		body = "(empty)"
	}
	fmt.Println(body)
	return nil
}

// ─── Helpers ───────────────────────────────────────────────────────────

var (
	stripTagRe   = regexp.MustCompile(`<[^>]+>`)
	stripStyleRe = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	stripScript  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	collapseWs   = regexp.MustCompile(`\s+`)
)

func stripHTML(s string) string {
	s = stripStyleRe.ReplaceAllString(s, "")
	s = stripScript.ReplaceAllString(s, "")
	s = stripTagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = collapseWs.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

var emailNameRe = regexp.MustCompile(`^(.+?)\s*<.+>$`)

func stripEmailName(s string) string {
	if m := emailNameRe.FindStringSubmatch(s); m != nil {
		return strings.Trim(m[1], `"`)
	}
	return s
}

func truncString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func orFallback(s, fb string) string {
	if s == "" {
		return fb
	}
	return s
}

func strOrDash(p *string) string {
	if p == nil || *p == "" {
		return "—"
	}
	return *p
}

func fmtRelative(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		// Try a few alternative formats Gmail sometimes emits.
		for _, layout := range []string{
			"2006-01-02T15:04:05.000Z",
			"2006-01-02 15:04:05",
		} {
			if t2, err2 := time.Parse(layout, iso); err2 == nil {
				t = t2
				err = nil
				break
			}
		}
		if err != nil {
			return iso
		}
	}
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()+0.5))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()+0.5))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24+0.5))
	}
	return t.Format("2006-01-02")
}

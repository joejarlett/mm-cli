package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// Overridable for tests.
var (
	auditListFunc = func(ctx context.Context, req wire.HubAuditListReq) (wire.HubAuditListResp, error) {
		client := http.New()
		var resp wire.HubAuditListResp
		err := client.Hub(ctx, "audit", "list", req, &resp)
		return resp, err
	}
	auditShowFunc = func(ctx context.Context, req wire.HubAuditShowReq) (wire.HubAuditShowResp, error) {
		client := http.New()
		var resp wire.HubAuditShowResp
		err := client.Hub(ctx, "audit", "show", req, &resp)
		return resp, err
	}
)

// NewRunCmd builds the `mm run` command — lists and shows Hermes runs
// from the hub audit_report table.
func NewRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Hermes run history from the hub audit log",
		Long:  "List recent Hermes agent runs and inspect full reports.\n\nDefault: list recent runs.",
		Args:  cobra.NoArgs,
		RunE:  runAuditListDefault,
	}
	cmd.Flags().Int("limit", 25, "How many runs to fetch")
	cmd.Flags().String("mode", "run", "Filter by mode (default: run)")
	cmd.AddCommand(newRunListCmd(), newRunShowCmd())
	return cmd
}

func newRunListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List recent Hermes runs",
		Args:    cobra.NoArgs,
		RunE:    runAuditList,
	}
	cmd.Flags().Int("limit", 25, "How many runs to fetch")
	cmd.Flags().String("mode", "run", "Filter by mode (default: run)")
	return cmd
}

func newRunShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <run-id>",
		Short: "Print the full audit report for a run",
		Args:  cobra.ExactArgs(1),
		RunE:  runAuditShow,
	}
}

func runAuditListDefault(cmd *cobra.Command, args []string) error {
	return runAuditList(cmd, args)
}

func runAuditList(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	mode, _ := cmd.Flags().GetString("mode")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	req := wire.HubAuditListReq{Limit: limit, Mode: mode}
	resp, err := auditListFunc(cmd.Context(), req)
	if err != nil {
		return err
	}

	if wantJSON {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal JSON: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	if len(resp.Runs) == 0 {
		cmd.Println("No runs yet.")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%-6s  %-14s  %-24s  %s\n",
		"WHEN", "RUN", "LOOKBACK", "SUMMARY")
	for _, run := range resp.Runs {
		ago := formatTimeAgo(run.CreatedAt)
		rid := run.RunID
		if len(rid) > 14 {
			rid = rid[:14]
		}
		lb := run.Lookback
		if lb == "" {
			lb = "-"
		}
		if len(lb) > 24 {
			lb = lb[:24]
		}
		summary := firstLine(run.Report, 100)
		fmt.Fprintf(cmd.OutOrStdout(), "%-6s  %-14s  %-24s  %s\n",
			ago, rid, lb, summary)
	}
	return nil
}

func runAuditShow(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	runID := args[0]

	resp, err := auditShowFunc(cmd.Context(), wire.HubAuditShowReq{RunID: runID})
	if err != nil {
		return err
	}

	if wantJSON {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal JSON: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	cmd.Printf("Run:    %s\n", resp.RunID)
	cmd.Printf("When:   %s\n", formatTimeAgo(resp.CreatedAt))
	cmd.Printf("Status: %s\n", nonempty(resp.Status))
	cmd.Println()

	if len(resp.Rows) == 0 {
		cmd.Println("(no detail rows)")
		return nil
	}

	for i, row := range resp.Rows {
		if i > 0 {
			cmd.Println(strings.Repeat("─", 60))
		}
		cmd.Printf("App:    %s\n", nonempty(row.AppSlug))
		cmd.Printf("Branch: %s\n", nonempty(row.Lookback))
		if row.FilesChecked > 0 || row.GapsFound > 0 || row.GapsFixed > 0 {
			cmd.Printf("Stats:  %d files checked, %d gaps found, %d fixed",
				row.FilesChecked, row.GapsFound, row.GapsFixed)
			if row.HighCount > 0 || row.MediumCount > 0 || row.LowCount > 0 {
				cmd.Printf(" (H:%d M:%d L:%d)", row.HighCount, row.MediumCount, row.LowCount)
			}
			cmd.Println()
		}
		if row.Report != "" {
			cmd.Println()
			fmt.Fprint(cmd.OutOrStdout(), row.Report)
			if !strings.HasSuffix(row.Report, "\n") {
				cmd.Println()
			}
		}
	}
	return nil
}

// firstLine returns the first line of s, truncated to maxLen chars.
func firstLine(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\n\r"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	if len(s) > maxLen {
		s = s[:maxLen-1] + "…"
	}
	if s == "" {
		return "(no summary)"
	}
	return s
}

// truncOr truncates s to maxLen (appending "…") or returns it as-is.
func truncOr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}

// nonempty returns s if non-empty, otherwise "—".
func nonempty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// formatTimeAgo returns a human-friendly relative time string.
func formatTimeAgo(iso string) string {
	if iso == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		// Fallback: try without nano.
		t2, err2 := time.Parse(time.RFC3339, iso)
		if err2 != nil {
			return iso
		}
		t = t2
	}
	d := time.Since(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		if days < 30 {
			return fmt.Sprintf("%dd ago", days)
		}
		months := days / 30
		if months == 1 {
			return "1mo ago"
		}
		return fmt.Sprintf("%dmo ago", months)
	}
}

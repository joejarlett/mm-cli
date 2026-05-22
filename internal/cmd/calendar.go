package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// NewCalendarCmd builds the `mm calendar` tree.
func NewCalendarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Google Calendar — agenda + quick create",
		Long:  "Default: next 7 days agenda. See `mm calendar new --help` to create events.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCalendarList(cmd, args)
		},
	}
	cmd.Flags().Int("days", 7, "Window (default 7)")
	cmd.Flags().String("q", "", "Filter by query string")
	cmd.Flags().String("account", "", "Pick a linked Google account")

	cmd.AddCommand(newCalendarListCmd(), newCalendarNewCmd())
	return cmd
}

func newCalendarListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Upcoming events (next 7 days by default)",
		RunE:    runCalendarList,
	}
	cmd.Flags().Int("days", 7, "Window (default 7)")
	cmd.Flags().String("q", "", "Filter by query string")
	cmd.Flags().String("account", "", "Pick a linked Google account")
	return cmd
}

func runCalendarList(cmd *cobra.Command, _ []string) error {
	days, _ := cmd.Flags().GetInt("days")
	q, _ := cmd.Flags().GetString("q")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	req := map[string]any{}
	if days != 7 && days > 0 {
		req["days"] = days
	}
	if q != "" {
		req["q"] = q
	}
	if account != "" {
		req["accountSlug"] = account
	}

	client := http.New()
	var resp wire.HubCalendarListResp
	if err := client.Hub(cmd.Context(), "calendar", "list", req, &resp); err != nil {
		return err
	}

	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Fprintln(os.Stdout, string(out))
		return nil
	}

	if len(resp.Events) == 0 {
		fmt.Printf("No events in the next %d days.\n", resp.RangeDays)
		return nil
	}

	// Group by start-date (YYYY-MM-DD).
	groups := map[string][]wire.HubCalendarEvent{}
	for _, e := range resp.Events {
		key := "unscheduled"
		if len(e.Start) >= 10 {
			key = e.Start[:10]
		}
		groups[key] = append(groups[key], e)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		head := "Unscheduled"
		if k != "unscheduled" {
			head = fmtDay(k)
		}
		fmt.Println()
		fmt.Println(head)
		for _, e := range groups[k] {
			t := fmtTime(e.Start, e.AllDay)
			loc := ""
			if e.Location != nil && *e.Location != "" {
				loc = "  📍 " + *e.Location
			}
			att := ""
			if e.Attendees > 0 {
				att = fmt.Sprintf("  👥 %d", e.Attendees)
			}
			fmt.Printf("  %s %s%s%s\n", padRight(t, 10), e.Summary, loc, att)
		}
	}
	return nil
}

// newCalendarNewCmd is a stub for Phase 2b. Args are accepted but the
// implementation lands once the NL date integration is wired through.
func newCalendarNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "new",
		Aliases: []string{"create"},
		Short:   "Quick-create a calendar event (not yet implemented in Go)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("mm calendar new — not yet implemented in the Go port, use TS mm for now")
		},
	}
	cmd.Flags().String("title", "", "Event title")
	cmd.Flags().String("when", "", "Natural-language start time")
	cmd.Flags().String("end", "", "End time (bare HH:MM relative, or NL/ISO)")
	cmd.Flags().String("at", "", "Location")
	cmd.Flags().String("describe", "", "Description")
	cmd.Flags().String("invite", "", "Comma-separated attendee emails")
	cmd.Flags().String("notify", "", "Notify: all|externalOnly|none")
	cmd.Flags().String("account", "", "Pick a linked Google account")
	return cmd
}

// ─── Formatters (ported from src/commands/calendar.ts) ─────────────────

func fmtDay(iso string) string {
	if iso == "" {
		return "—"
	}
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
		if err != nil {
			return iso
		}
	}
	return t.Format("Mon 2 Jan")
}

func fmtTime(iso string, allDay bool) string {
	if allDay {
		return "all-day"
	}
	if iso == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("15:04")
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

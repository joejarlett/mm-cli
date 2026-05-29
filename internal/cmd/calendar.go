package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/http"
	"mm-cli/internal/nldate"
	"mm-cli/internal/wire"
)

// NewCalendarCmd builds the `mm calendar` tree.
func NewCalendarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Google Calendar — agenda + quick create",
		Long:  "Default: next 7 days agenda. See `mm calendar new --help` to create events.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCalendarList(cmd, args)
		},
	}
	cmd.Flags().Int("days", 7, "Window (default 7)")
	cmd.Flags().String("q", "", "Filter by query string")
	cmd.Flags().String("account", "", "Pick a linked Google account")

	cmd.AddCommand(newCalendarListCmd(), newCalendarNewCmd(), newCalendarGetCmd(), newCalendarDeleteCmd())
	return cmd
}

func newCalendarListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Upcoming events (next 7 days by default)",
		Args:    cobra.NoArgs,
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
		fmt.Printf("### %s\n", head)
		for _, e := range groups[k] {
			t := fmtTime(e.Start, e.AllDay)
			loc := ""
			if e.Location != nil && *e.Location != "" {
				loc = " 📍 " + *e.Location
			}
			att := ""
			if e.Attendees > 0 {
				att = fmt.Sprintf(" 👥 %d", e.Attendees)
			}
			link := ""
			if e.HTMLLink != nil && *e.HTMLLink != "" {
				link = fmt.Sprintf(" — [Event](%s)", *e.HTMLLink)
			}
			fmt.Printf("- **%s** — %s%s%s%s\n", t, e.Summary, loc, att, link)
		}
	}
	return nil
}

func newCalendarNewCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "new",
		Aliases: []string{"create"},
		Short:   "Quick-create a calendar event",
		RunE:    runCalendarNew,
	}
	c.Flags().String("title", "", "Event title (required)")
	c.Flags().String("when", "", "Natural-language start time (required)")
	c.Flags().String("end", "", "End time (bare HH:MM relative, or NL/ISO)")
	c.Flags().String("at", "", "Location")
	c.Flags().String("describe", "", "Description")
	c.Flags().String("invite", "", "Comma-separated attendee emails")
	c.Flags().String("notify", "", "Notify: all|externalOnly|none")
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func runCalendarNew(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	when, _ := cmd.Flags().GetString("when")
	end, _ := cmd.Flags().GetString("end")
	at, _ := cmd.Flags().GetString("at")
	describe, _ := cmd.Flags().GetString("describe")
	invite, _ := cmd.Flags().GetString("invite")
	notify, _ := cmd.Flags().GetString("notify")
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	if title == "" || when == "" {
		return fmt.Errorf("Usage: mm calendar new --title \"X\" --when \"tomorrow 14:00\" [--end \"15:00\"] [--at \"...\"] [--invite a@x.com,b@y.com] [--describe \"...\"]")
	}

	start, err := nldate.ParseDateTime(when, time.Now())
	if err != nil {
		return err
	}
	var endISO string
	if end != "" {
		// Bare HH:MM → anchor to start day; otherwise treat as NL/ISO.
		if matchedTime := bareTimeRe.FindStringSubmatch(end); matchedTime != nil {
			endDay := time.Date(start.Date.Year(), start.Date.Month(), start.Date.Day(),
				atoiOrZero(matchedTime[1]), atoiOrZero(matchedTime[2]), 0, 0, start.Date.Location())
			endISO = formatLocalISO(endDay)
		} else {
			r, err := nldate.ParseDateTime(end, time.Now())
			if err != nil {
				return err
			}
			endISO = r.ISO
		}
	}

	req := map[string]any{"title": title, "when": start.ISO}
	if endISO != "" {
		req["end"] = endISO
	}
	if at != "" {
		req["location"] = at
	}
	if describe != "" {
		req["description"] = describe
	}
	if invite != "" {
		var atts []string
		for _, p := range strings.Split(invite, ",") {
			if s := strings.TrimSpace(p); s != "" {
				atts = append(atts, s)
			}
		}
		if len(atts) > 0 {
			req["attendees"] = atts
		}
	}
	if notify != "" {
		req["sendUpdates"] = notify
	}
	if account != "" {
		req["accountSlug"] = account
	}

	client := http.New()
	var resp wire.HubCalendarCreateResp
	if err := client.Hub(cmd.Context(), "calendar", "create", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("✓ Created: %s\n", resp.Summary)
	fmt.Printf("  %s %s–%s\n", fmtDay(resp.Start), fmtTime(resp.Start, false), fmtTime(resp.End, false))
	if resp.HTMLLink != nil && *resp.HTMLLink != "" {
		fmt.Printf("  %s\n", *resp.HTMLLink)
	}
	return nil
}

var bareTimeRe = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)

func atoiOrZero(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func formatLocalISO(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d",
		t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
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

func newCalendarGetCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "get <event-id>",
		Short: "Inspect a single event by ID",
		Args:  cobra.ExactArgs(1),
		RunE:  runCalendarGet,
	}
	c.Flags().String("account", "", "Pick a linked Google account")
	return c
}

func runCalendarGet(cmd *cobra.Command, args []string) error {
	account, _ := cmd.Flags().GetString("account")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	req := map[string]any{"eventId": args[0]}
	if account != "" {
		req["accountSlug"] = account
	}
	var resp wire.HubCalendarGetResp
	if err := http.New().Hub(cmd.Context(), "calendar", "get", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Fprintln(os.Stdout, string(out))
		return nil
	}
	e := resp.Event
	loc := ""
	if e.Location != nil && *e.Location != "" {
		loc = " 📍 " + *e.Location
	}
	att := ""
	if e.Attendees > 0 {
		att = fmt.Sprintf(" 👥 %d", e.Attendees)
	}
	fmt.Printf("%s %s — %s%s%s `%s`\n", fmtDay(e.Start), fmtTime(e.Start, e.AllDay), e.Summary, loc, att, e.ID)
	if e.Description != nil && *e.Description != "" {
		fmt.Printf("\n%s\n", *e.Description)
	}
	if e.HTMLLink != nil && *e.HTMLLink != "" {
		fmt.Printf("\n[Event](%s)\n", *e.HTMLLink)
	}
	return nil
}

func newCalendarDeleteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "delete <event-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an event by ID",
		Args:    cobra.ExactArgs(1),
		RunE:    runCalendarDelete,
	}
	c.Flags().String("account", "", "Pick a linked Google account")
	c.Flags().String("notify", "none", "Notify attendees: all|externalOnly|none")
	return c
}

func runCalendarDelete(cmd *cobra.Command, args []string) error {
	account, _ := cmd.Flags().GetString("account")
	notify, _ := cmd.Flags().GetString("notify")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	req := map[string]any{"eventId": args[0]}
	if account != "" {
		req["accountSlug"] = account
	}
	if notify != "" && notify != "none" {
		req["notify"] = notify
	}
	var resp wire.HubCalendarDeleteResp
	if err := http.New().Hub(cmd.Context(), "calendar", "delete", req, &resp); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Fprintln(os.Stdout, string(out))
		return nil
	}
	fmt.Printf("✓ Deleted event: %s\n", resp.EventID)
	return nil
}

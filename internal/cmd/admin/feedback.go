// Feedback list / inspect / triage, ported from meta-me.uk/cli/mm.ts cmdFeedback.
package admin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"mm-cli/internal/db"
)

// The single feedback vocabulary, matching meta-me.uk's admin picker. Kept
// deliberately shorter than the error statuses: feedback has no `ignored`,
// because that status only means "do not auto-reopen on recurrence", and
// feedback never recurs.
var feedbackStatuses = []string{"new", "triaged", "resolved", "wontfix"}

func allowedFeedbackStatus(s string) bool {
	for _, v := range feedbackStatuses {
		if v == s {
			return true
		}
	}
	return false
}

func newFeedbackCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "feedback [id] [<status>]",
		Short: "List feedback, inspect one, or set its status",
		Args:  cobra.RangeArgs(0, 2),
		RunE:  runFeedback,
	}
	c.Flags().String("status", "", "Filter: new|triaged|resolved|wontfix")
	c.Flags().String("app", "", "Filter by app slug")
	c.Flags().Int("limit", 50, "Max rows")
	return c
}

func runFeedback(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}

	// mutate: feedback <id> <status>
	if len(args) == 2 {
		// Validate against the same vocabulary the hub's admin UI offers. This
		// used to write whatever string it was given, and the hub did too, so a
		// row could land in a state neither surface could display or set again —
		// which is exactly how one row sat at `actioned` while the picker only
		// knew new/triaged/resolved/wontfix.
		if !allowedFeedbackStatus(args[1]) {
			return fmt.Errorf("invalid status %q — must be one of: %s",
				args[1], strings.Join(feedbackStatuses, ", "))
		}
		var id, status, message string
		err := pool.QueryRow(cmd.Context(),
			`UPDATE feedback SET status = $1 WHERE id = $2 RETURNING id, status, message`,
			args[1], args[0]).Scan(&id, &status, &message)
		if err != nil {
			return fmt.Errorf("Feedback not found: %s", args[0])
		}
		fmt.Printf("✓ %s → %s  (%s)\n", id, status, trunc(message, 60))
		return nil
	}

	// inspect: feedback <id>
	if len(args) == 1 {
		_, rows, err := queryMaps(cmd.Context(), pool, `
			SELECT f.*, u.email, u.name FROM feedback f
			LEFT JOIN "user" u ON u.id::text = f.user_id
			WHERE f.id = $1`, args[0])
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("Feedback not found: %s", args[0])
		}
		f := rows[0]
		if wantJSON {
			j, _ := json.MarshalIndent(f, "", "  ")
			fmt.Println(string(j))
			return nil
		}
		appSlug := "(hub)"
		if f["app_slug"] != nil {
			appSlug = fmt.Sprint(f["app_slug"])
		}
		fmt.Printf("# Feedback %s\n\n", f["id"])
		fmt.Printf("- status: %v · app: %s\n", f["status"], appSlug)
		fmt.Printf("- from: %v <%v>\n", f["name"], f["email"])
		fmt.Printf("- created: %s\n", fmtCell(f["created_at"]))
		if f["url"] != nil && fmt.Sprint(f["url"]) != "" {
			fmt.Printf("- url: %v\n", f["url"])
		}
		if f["screen_size"] != nil && fmt.Sprint(f["screen_size"]) != "" {
			fmt.Printf("- screen: %v\n", f["screen_size"])
		}
		fmt.Printf("\n## Message\n\n%v\n", f["message"])
		return nil
	}

	// list
	status := getString(cmd, "status")
	app := getString(cmd, "app")
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 50
	}
	conds, qargs, i := []string{"1=1"}, []any{}, 1
	if status != "" {
		conds = append(conds, fmt.Sprintf("f.status = $%d", i))
		qargs = append(qargs, status)
		i++
	}
	if app != "" {
		conds = append(conds, fmt.Sprintf("f.app_slug = $%d", i))
		qargs = append(qargs, app)
		i++
	}
	qargs = append(qargs, limit)
	_, rows, err := queryMaps(cmd.Context(), pool, `
		SELECT f.id, f.status, f.app_slug, u.email, f.message, f.created_at
		FROM feedback f
		LEFT JOIN "user" u ON u.id::text = f.user_id
		WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY f.created_at DESC
		LIMIT $`+strconv.Itoa(i), qargs...)
	if err != nil {
		return err
	}
	if wantJSON {
		j, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	fmt.Printf("# Feedback (%d)\n\n", len(rows))
	renderMD([]string{"id", "status", "app_slug", "email", "created_at", "message"}, rows)
	if len(rows) > 0 {
		fmt.Println("\n→ `mm admin feedback <id>` for full message")
	}
	return nil
}

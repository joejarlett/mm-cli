// Activity digest, ported from meta-me.uk/cli/mm.ts cmdDigest.
package admin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/db"
)

func newDigestCmd() *cobra.Command {
	c := &cobra.Command{Use: "digest", Short: "Recent platform activity (digest rows)", Args: cobra.NoArgs, RunE: runDigest}
	c.Flags().String("since", "", "Window (e.g. 24h, 7d). Default: 24h")
	c.Flags().Int("limit", 100, "Max rows")
	c.Flags().String("user", "", "Filter by user (email or id)")
	c.Flags().String("app", "", "Filter by app slug")
	return c
}

func runDigest(cmd *cobra.Command, _ []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	since := parseSince(getString(cmd, "since"))
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 100
	}
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}

	conds, qargs, i := []string{"d.occurred_at > $1"}, []any{since}, 2
	if userFlag := getString(cmd, "user"); userFlag != "" {
		u, err := findUser(cmd.Context(), pool, userFlag)
		if err != nil {
			return err
		}
		conds = append(conds, fmt.Sprintf("d.user_id = $%d", i))
		qargs = append(qargs, u.ID)
		i++
	}
	if app := getString(cmd, "app"); app != "" {
		conds = append(conds, fmt.Sprintf("d.app_slug = $%d", i))
		qargs = append(qargs, app)
		i++
	}
	qargs = append(qargs, limit)
	_, rows, err := queryMaps(cmd.Context(), pool, `
		SELECT d.occurred_at, d.app_slug, d.action, d.summary, u.email
		FROM digest d
		LEFT JOIN "user" u ON u.id = d.user_id
		WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY d.occurred_at DESC
		LIMIT $`+strconv.Itoa(i), qargs...)
	if err != nil {
		return err
	}
	if wantJSON {
		j, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	fmt.Printf("# Digest (%d, since %s)\n\n", len(rows), since.Format("2006-01-02 15:04"))
	renderMD([]string{"occurred_at", "email", "app_slug", "action", "summary"}, rows)
	return nil
}

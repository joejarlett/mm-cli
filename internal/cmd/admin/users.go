// Users + access management, ported from meta-me.uk/cli/mm.ts
// (cmdUsers / cmdUser / cmdInvite / cmdGrant / cmdRevoke / cmdRole).
package admin

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"mm-cli/internal/db"
)

func newUsersCmd() *cobra.Command {
	c := &cobra.Command{Use: "users", Short: "List users (with apps + last activity)", Args: cobra.NoArgs, RunE: runUsers}
	c.Flags().String("role", "", "Filter: admin|user")
	c.Flags().String("app", "", "Only users with this app enabled")
	return c
}

func newUserCmd() *cobra.Command {
	return &cobra.Command{Use: "user [email-or-id]", Short: "Inspect one user (apps + recent activity)", Args: cobra.ExactArgs(1), RunE: runUser}
}

func newInviteCmd() *cobra.Command {
	c := &cobra.Command{Use: "invite [email]", Short: "Create a user row (pre-SSO invite)", Args: cobra.ExactArgs(1), RunE: runInvite}
	c.Flags().String("apps", "", "Comma-separated app slugs to enable")
	c.Flags().String("role", "user", "admin|user")
	c.Flags().String("name", "", "Display name (default: email local part)")
	return c
}

func newGrantCmd() *cobra.Command {
	return &cobra.Command{Use: "grant [email-or-id] [slug]", Short: "Grant an app to a user", Args: cobra.ExactArgs(2), RunE: runGrant}
}

func newRevokeCmd() *cobra.Command {
	return &cobra.Command{Use: "revoke [email-or-id] [slug]", Short: "Revoke an app from a user", Args: cobra.ExactArgs(2), RunE: runRevoke}
}

func newRoleCmd() *cobra.Command {
	return &cobra.Command{Use: "role [email-or-id] [admin|user]", Short: "Set a user's role", Args: cobra.ExactArgs(2), RunE: runRole}
}

// ─── helpers ───────────────────────────────────────────────────────────

type hubUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// findUser resolves an email (contains @) or id to a user row.
func findUser(ctx context.Context, pool *pgxpool.Pool, identifier string) (*hubUser, error) {
	col := "id"
	if strings.Contains(identifier, "@") {
		col = "email"
	}
	var u hubUser
	err := pool.QueryRow(ctx, `SELECT id, email, name, role FROM "user" WHERE `+col+` = $1 LIMIT 1`, identifier).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role)
	if err != nil {
		return nil, fmt.Errorf("User not found: %s", identifier)
	}
	return &u, nil
}

func requireApp(ctx context.Context, pool *pgxpool.Pool, slug string) error {
	var s string
	if err := pool.QueryRow(ctx, `SELECT slug FROM app WHERE slug = $1 LIMIT 1`, slug).Scan(&s); err != nil {
		return fmt.Errorf("App not found: %s", slug)
	}
	return nil
}

// queryMaps runs a query and returns (columns, rows-as-maps) — the shared
// shape every list verb renders from.
func queryMaps(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) ([]string, []map[string]any, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols := rowFields(rows)
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, nil, err
		}
		row := map[string]any{}
		for i, c := range cols {
			row[c] = vals[i]
		}
		out = append(out, row)
	}
	return cols, out, rows.Err()
}

// newUUID returns a random v4 UUID without adding a dependency.
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ─── runners ───────────────────────────────────────────────────────────

func runUsers(cmd *cobra.Command, _ []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	role := getString(cmd, "role")
	app := getString(cmd, "app")
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	conds, args, i := []string{"1=1"}, []any{}, 1
	if role != "" {
		conds = append(conds, fmt.Sprintf("u.role = $%d", i))
		args = append(args, role)
		i++
	}
	if app != "" {
		conds = append(conds, fmt.Sprintf("EXISTS (SELECT 1 FROM user_app ua2 WHERE ua2.user_id = u.id AND ua2.app_slug = $%d)", i))
		args = append(args, app)
		i++
	}
	cols, data, err := queryMaps(cmd.Context(), pool, `
		SELECT u.id, u.email, u.name, u.role, u.created_at,
			COALESCE((SELECT array_agg(ua.app_slug ORDER BY ua.app_slug) FROM user_app ua WHERE ua.user_id = u.id), ARRAY[]::text[]) AS apps,
			(SELECT MAX(ua.last_active_at) FROM user_app ua WHERE ua.user_id = u.id) AS last_active
		FROM "user" u
		WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY u.created_at DESC`, args...)
	if err != nil {
		return err
	}
	if wantJSON {
		j, _ := json.MarshalIndent(data, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	_ = cols
	fmt.Printf("# Users (%d)\n\n", len(data))
	renderMD([]string{"email", "name", "role", "apps", "last_active", "created_at"}, data)
	return nil
}

func runUser(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	u, err := findUser(cmd.Context(), pool, args[0])
	if err != nil {
		return err
	}
	_, apps, err := queryMaps(cmd.Context(), pool,
		`SELECT app_slug, enabled_at, last_active_at FROM user_app WHERE user_id = $1 ORDER BY app_slug`, u.ID)
	if err != nil {
		return err
	}
	_, recent, err := queryMaps(cmd.Context(), pool,
		`SELECT occurred_at, app_slug, action, summary FROM digest WHERE user_id = $1 ORDER BY occurred_at DESC LIMIT 10`, u.ID)
	if err != nil {
		return err
	}
	if wantJSON {
		j, _ := json.MarshalIndent(map[string]any{"user": u, "apps": apps, "recent": recent}, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	fmt.Printf("# %s <%s>\n\n", u.Name, u.Email)
	fmt.Printf("- id: %s\n- role: %s\n", u.ID, u.Role)
	fmt.Printf("\n## Apps (%d)\n\n", len(apps))
	renderMD([]string{"app_slug", "enabled_at", "last_active_at"}, apps)
	fmt.Printf("\n## Recent activity (last 10)\n\n")
	renderMD([]string{"occurred_at", "app_slug", "action", "summary"}, recent)
	return nil
}

func runInvite(cmd *cobra.Command, args []string) error {
	email := args[0]
	if !strings.Contains(email, "@") {
		return fmt.Errorf("Usage: mm admin invite <email> [--apps=keel,kb] [--role=user] [--name=…]")
	}
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	var existing string
	if err := pool.QueryRow(cmd.Context(), `SELECT id FROM "user" WHERE email = $1 LIMIT 1`, email).Scan(&existing); err == nil {
		return fmt.Errorf("User already exists: %s", email)
	}
	id := newUUID()
	name := getString(cmd, "name")
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	role := getString(cmd, "role")
	if role == "" {
		role = "user"
	}
	if _, err := pool.Exec(cmd.Context(),
		`INSERT INTO "user" (id, email, name, role, created_at) VALUES ($1, $2, $3, $4, NOW())`,
		id, email, name, role); err != nil {
		return err
	}
	var slugs []string
	for _, s := range strings.Split(getString(cmd, "apps"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			slugs = append(slugs, s)
			if _, err := pool.Exec(cmd.Context(),
				`INSERT INTO user_app (user_id, app_slug, enabled_at) VALUES ($1, $2, NOW())`, id, s); err != nil {
				return err
			}
		}
	}
	extra := ""
	if len(slugs) > 0 {
		extra = "  apps=" + strings.Join(slugs, ",")
	}
	fmt.Printf("✓ invited %s  id=%s  role=%s%s\n", email, id, role, extra)
	return nil
}

func runGrant(cmd *cobra.Command, args []string) error {
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	u, err := findUser(cmd.Context(), pool, args[0])
	if err != nil {
		return err
	}
	if err := requireApp(cmd.Context(), pool, args[1]); err != nil {
		return err
	}
	if _, err := pool.Exec(cmd.Context(),
		`INSERT INTO user_app (user_id, app_slug, enabled_at) VALUES ($1, $2, NOW()) ON CONFLICT (user_id, app_slug) DO NOTHING`,
		u.ID, args[1]); err != nil {
		return err
	}
	fmt.Printf("✓ granted %s to %s\n", args[1], u.Email)
	return nil
}

func runRevoke(cmd *cobra.Command, args []string) error {
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	u, err := findUser(cmd.Context(), pool, args[0])
	if err != nil {
		return err
	}
	tag, err := pool.Exec(cmd.Context(), `DELETE FROM user_app WHERE user_id = $1 AND app_slug = $2`, u.ID, args[1])
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s did not have %s", u.Email, args[1])
	}
	fmt.Printf("✓ revoked %s from %s\n", args[1], u.Email)
	return nil
}

func runRole(cmd *cobra.Command, args []string) error {
	role := args[1]
	if role != "admin" && role != "user" {
		return fmt.Errorf("Role must be 'admin' or 'user'")
	}
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	u, err := findUser(cmd.Context(), pool, args[0])
	if err != nil {
		return err
	}
	if _, err := pool.Exec(cmd.Context(), `UPDATE "user" SET role = $1 WHERE id = $2`, role, u.ID); err != nil {
		return err
	}
	fmt.Printf("✓ %s → %s\n", u.Email, role)
	return nil
}

// Living docs (platform standards / runbooks / policies), ported from
// meta-me.uk/cli/mm.ts cmdDocs / cmdDoc.
package admin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"mm-cli/internal/db"
)

func newDocsCmd() *cobra.Command {
	c := &cobra.Command{Use: "docs", Short: "List living docs", Args: cobra.NoArgs, RunE: runDocs}
	c.Flags().String("type", "", "Filter: standard|runbook|policy|…")
	return c
}

func newDocCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "doc [slug] [set|delete]",
		Short: "Read, write, or delete one living doc",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runDoc,
	}
	c.Flags().String("title", "", "Doc title (required when creating)")
	c.Flags().String("type", "standard", "Doc type")
	c.Flags().String("body", "", "Body text, or @path to read from a file")
	return c
}

func runDocs(cmd *cobra.Command, _ []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}
	docType := getString(cmd, "type")
	q := `SELECT slug, title, type, version, updated_at, length(body) AS size FROM doc`
	var qargs []any
	if docType != "" {
		q += ` WHERE type = $1`
		qargs = append(qargs, docType)
	}
	q += ` ORDER BY type, slug`
	_, rows, err := queryMaps(cmd.Context(), pool, q, qargs...)
	if err != nil {
		return err
	}
	if wantJSON {
		j, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	if len(rows) == 0 {
		fmt.Println("_(no docs)_")
		return nil
	}
	fmt.Printf("# Docs (%d)\n\n", len(rows))
	renderMD([]string{"slug", "type", "title", "version", "size", "updated_at"}, rows)
	return nil
}

func runDoc(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	slug := args[0]
	pool, err := db.Pool(cmd.Context())
	if err != nil {
		return err
	}

	if len(args) == 2 && args[1] == "delete" {
		tag, err := pool.Exec(cmd.Context(), `DELETE FROM doc WHERE slug = $1`, slug)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("No doc matching slug: %s", slug)
		}
		fmt.Printf("✓ deleted %s\n", slug)
		return nil
	}

	if len(args) == 2 && args[1] == "set" {
		title := getString(cmd, "title")
		docType := getString(cmd, "type")
		if docType == "" {
			docType = "standard"
		}
		body := getString(cmd, "body")
		if strings.HasPrefix(body, "@") {
			b, err := os.ReadFile(body[1:])
			if err != nil {
				return err
			}
			body = string(b)
		}
		var version int
		err := pool.QueryRow(cmd.Context(), `SELECT version FROM doc WHERE slug = $1`, slug).Scan(&version)
		if err != nil { // not found → create
			if title == "" {
				return fmt.Errorf("--title=… is required when creating a new doc.")
			}
			if _, err := pool.Exec(cmd.Context(),
				`INSERT INTO doc (slug, title, type, body) VALUES ($1, $2, $3, $4)`,
				slug, title, docType, body); err != nil {
				return err
			}
			fmt.Printf("✓ created %s (v1, %d bytes)\n", slug, len(body))
			return nil
		}
		var titleArg *string
		if title != "" {
			titleArg = &title
		}
		if _, err := pool.Exec(cmd.Context(), `
			UPDATE doc SET title = COALESCE($1, title), type = $2, body = $3,
				version = $4, updated_at = NOW()
			WHERE slug = $5`,
			titleArg, docType, body, version+1, slug); err != nil {
			return err
		}
		fmt.Printf("✓ updated %s (v%d, %d bytes)\n", slug, version+1, len(body))
		return nil
	}

	if len(args) == 2 {
		return fmt.Errorf("Usage: mm admin doc <slug> [set|delete] [--title=…] [--type=…] [--body=@file.md]")
	}

	_, rows, err := queryMaps(cmd.Context(), pool, `SELECT * FROM doc WHERE slug = $1`, slug)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("No doc matching slug: %s", slug)
	}
	d := rows[0]
	if wantJSON {
		j, _ := json.MarshalIndent(d, "", "  ")
		fmt.Println(string(j))
		return nil
	}
	fmt.Printf("# %v  (%v, v%v)\n\n", d["title"], d["type"], d["version"])
	fmt.Printf("- slug: %v · updated: %s\n\n---\n\n%v\n", d["slug"], fmtCell(d["updated_at"]), d["body"])
	return nil
}

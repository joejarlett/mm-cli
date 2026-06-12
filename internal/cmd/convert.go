package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// cacheVersion is mixed into every cache key. Bump it whenever the docling
// image (and thus output) changes, to invalidate stale entries in one go.
const cacheVersion = "docling-v1"

// convertURL is the local docling-serve container. Override with MM_CONVERT_URL.
func convertURL() string {
	if u := os.Getenv("MM_CONVERT_URL"); u != "" {
		return u
	}
	return "http://localhost:5001"
}

// cacheDir is the shared on-disk convert cache (also used by the agent's
// convert_file tool). Keyed by content hash + params, so re-converting the
// same bytes is instant — important given PDFs take ~18s.
func cacheDir() string {
	if d := os.Getenv("MM_CONVERT_CACHE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mm", "convert-cache")
}

// cacheKey hashes the file bytes together with every param that affects output.
func cacheKey(data []byte, to string, noOCR, noTables, embed, wantJSON bool) string {
	h := sha256.New()
	h.Write(data)
	fmt.Fprintf(h, "|v=%s|to=%s|ocr=%t|tables=%t|embed=%t|json=%t",
		cacheVersion, to, !noOCR, !noTables, embed, wantJSON)
	return hex.EncodeToString(h.Sum(nil))
}

// formatField maps a --to value to docling's response field.
var formatField = map[string]string{
	"md":      "md_content",
	"html":    "html_content",
	"text":    "text_content",
	"json":    "json_content",
	"doctags": "doctags_content",
}

// NewConvertCmd builds `mm convert <file>`.
func NewConvertCmd() *cobra.Command {
	var to string
	var noOCR, noTables, embedImages, noCache bool

	c := &cobra.Command{
		Use:   "convert [file]",
		Short: "Convert a document to Markdown (docx/xlsx/pptx/pdf)",
		Long: "Convert office/PDF files to clean Markdown for LLM ingestion via the local " +
			"docling-serve container. Handles docx/xlsx/pptx and PDF (incl. scanned, via OCR). " +
			"Deterministic and offline — no LLM in the loop, no network egress.\n\n" +
			"Service URL defaults to http://localhost:5001 (override with MM_CONVERT_URL).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "help" {
				return cmd.Help()
			}
			if _, ok := formatField[to]; !ok {
				return fmt.Errorf("unknown --to %q (use: md, html, text, json, doctags)", to)
			}
			return runConvert(cmd, args[0], to, noOCR, noTables, embedImages, noCache)
		},
	}
	c.Flags().StringVar(&to, "to", "md", "output format: md|html|text|json|doctags")
	c.Flags().BoolVar(&noOCR, "no-ocr", false, "skip OCR pass")
	c.Flags().BoolVar(&noTables, "no-tables", false, "skip table-structure model (faster, loses table fidelity)")
	c.Flags().BoolVar(&embedImages, "embed-images", false, "embed images as base64 (default: replace with placeholders)")
	c.Flags().BoolVar(&noCache, "no-cache", false, "skip the content-hash cache (always re-convert)")
	return c
}

func runConvert(cmd *cobra.Command, file, to string, noOCR, noTables, embedImages, noCache bool) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("can't read %q: %w", file, err)
	}

	// Content-hash cache. Conversions are deterministic per docling version, so
	// the same bytes + params always map to the same output — return it instantly.
	var cachePath string
	if !noCache {
		cachePath = filepath.Join(cacheDir(), cacheKey(data, to, noOCR, noTables, embedImages, wantJSON))
		if cached, err := os.ReadFile(cachePath); err == nil {
			fmt.Fprintln(os.Stderr, "ℹ cache hit")
			fmt.Print(string(cached))
			return nil
		}
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", filepath.Base(file))
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	_ = w.WriteField("to_formats", to)
	// Default to placeholder so we don't inline megabytes of base64 image data
	// into LLM-bound Markdown. --embed-images opts back into docling's default.
	if embedImages {
		_ = w.WriteField("image_export_mode", "embedded")
	} else {
		_ = w.WriteField("image_export_mode", "placeholder")
	}
	if noOCR {
		_ = w.WriteField("do_ocr", "false")
	}
	if noTables {
		_ = w.WriteField("do_table_structure", "false")
	}
	if err := w.Close(); err != nil {
		return err
	}

	url := convertURL()
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, url+"/v1/convert/file", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("convert service unreachable at %s (%v)\n  start it with:  docker start mm-convert", url, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("convert failed (%d): %s", resp.StatusCode, truncString(string(respBody), 300))
	}

	// Build the exact stdout text once, then print + cache it.
	var out string
	if wantJSON {
		out = string(respBody)
	} else {
		var parsed struct {
			Document map[string]json.RawMessage `json:"document"`
			Status   string                     `json:"status"`
			Errors   []json.RawMessage          `json:"errors"`
		}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return fmt.Errorf("convert: invalid JSON response: %w", err)
		}
		raw, ok := parsed.Document[formatField[to]]
		if !ok || len(raw) == 0 {
			return fmt.Errorf("no %s content returned (status: %s)", to, parsed.Status)
		}
		// md/html/text are JSON strings; json/doctags are already serialised — a
		// failed unmarshal means "use it verbatim".
		var content string
		if to == "json" || json.Unmarshal(raw, &content) != nil {
			out = string(raw)
		} else {
			out = content
		}
	}
	if out != "" && out[len(out)-1] != '\n' {
		out += "\n"
	}

	if cachePath != "" {
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
			// Best-effort; a cache miss next time is the only cost of failure.
			_ = os.WriteFile(cachePath, []byte(out), 0o644)
		}
	}
	fmt.Print(out)
	return nil
}

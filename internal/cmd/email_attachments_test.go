package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mm-cli/internal/wire"
)

func att(id, name string, inline bool) wire.HubInboxAttachment {
	return wire.HubInboxAttachment{AttachmentID: id, Filename: name, Inline: inline}
}

func TestPickAttachments(t *testing.T) {
	pdf := att("a1", "invoice.pdf", false)
	csv := att("a2", "rows.csv", false)
	logo := att("a3", "logo.png", true)

	tests := []struct {
		name          string
		list          []wire.HubInboxAttachment
		wantID        string
		all           bool
		includeInline bool
		wantIDs       []string
		wantErr       string
	}{
		{name: "no attachments", list: nil, wantErr: "no attachments"},
		{name: "explicit id", list: []wire.HubInboxAttachment{pdf, csv}, wantID: "a2", wantIDs: []string{"a2"}},
		{name: "explicit id can pick an inline part", list: []wire.HubInboxAttachment{pdf, logo}, wantID: "a3", wantIDs: []string{"a3"}},
		{name: "unknown id", list: []wire.HubInboxAttachment{pdf}, wantID: "nope", wantErr: "no attachment nope"},
		{name: "single real attachment needs no flag", list: []wire.HubInboxAttachment{pdf, logo}, wantIDs: []string{"a1"}},
		{name: "several refuse without --all", list: []wire.HubInboxAttachment{pdf, csv}, wantErr: "pass --all"},
		{name: "several with --all", list: []wire.HubInboxAttachment{pdf, csv}, all: true, wantIDs: []string{"a1", "a2"}},
		{name: "inline only", list: []wire.HubInboxAttachment{logo}, wantErr: "--include-inline"},
		{name: "inline included", list: []wire.HubInboxAttachment{logo}, includeInline: true, wantIDs: []string{"a3"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickAttachments(tc.list, tc.wantID, tc.all, tc.includeInline, "msg-1")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var ids []string
			for _, a := range got {
				ids = append(ids, a.AttachmentID)
			}
			if strings.Join(ids, ",") != strings.Join(tc.wantIDs, ",") {
				t.Fatalf("want %v, got %v", tc.wantIDs, ids)
			}
		})
	}
}

func TestSafeFilename(t *testing.T) {
	tests := []struct{ in, want string }{
		{"invoice.pdf", "invoice.pdf"},
		{"../../etc/passwd", "passwd"},
		{`C:\Windows\evil.exe`, "evil.exe"},
		{"/absolute/path.txt", "path.txt"},
		{"weird\nname\t.txt", "weird_name_.txt"},
		{"  spaced .pdf  ", "spaced .pdf"},
		{"", "attachment-abc123def456"},
		{"...", "attachment-abc123def456"},
	}
	for _, tc := range tests {
		if got := safeFilename(tc.in, "abc123def456789"); got != tc.want {
			t.Errorf("safeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDecodeBase64URL(t *testing.T) {
	// "hello?~" encodes with base64url-specific characters.
	if b, err := decodeBase64URL("aGVsbG8_fg"); err != nil || string(b) != "hello?~" {
		t.Fatalf("raw base64url: got %q, %v", b, err)
	}
	if b, err := decodeBase64URL("aGVsbG8="); err != nil || string(b) != "hello" {
		t.Fatalf("padded: got %q, %v", b, err)
	}
	if _, err := decodeBase64URL(""); err == nil {
		t.Fatal("empty body should error")
	}
	if _, err := decodeBase64URL("!!!not base64!!!"); err == nil {
		t.Fatal("garbage should error")
	}
}

func TestUniquePathNeverClobbers(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "report.pdf")

	got, err := uniquePath(first)
	if err != nil || got != first {
		t.Fatalf("free path: got %q, %v", got, err)
	}
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = uniquePath(first)
	if err != nil || got != filepath.Join(dir, "report-1.pdf") {
		t.Fatalf("taken path: got %q, %v", got, err)
	}
	if err := os.WriteFile(got, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err = uniquePath(first); err != nil || got != filepath.Join(dir, "report-2.pdf") {
		t.Fatalf("twice taken: got %q, %v", got, err)
	}
}

func TestHumanBytes(t *testing.T) {
	for in, want := range map[int64]string{0: "0B", 512: "512B", 2048: "2.0KB", 5 * 1024 * 1024: "5.0MB"} {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

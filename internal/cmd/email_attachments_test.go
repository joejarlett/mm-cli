package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mm-cli/internal/wire"
)

// att builds a listing row. AttachmentID is deliberately set to something
// obviously throwaway: Gmail regenerates it on every messages.get, so nothing
// in the download path may key off it — see TestDownloadAddressesPartsByPartID.
func att(partID, name string, inline bool) wire.HubInboxAttachment {
	return wire.HubInboxAttachment{
		PartID:       partID,
		AttachmentID: "volatile-" + partID,
		Filename:     name,
		Inline:       inline,
	}
}

func TestPickAttachments(t *testing.T) {
	pdf := att("1", "invoice.pdf", false)
	csv := att("2", "rows.csv", false)
	logo := att("3", "logo.png", true)

	tests := []struct {
		name      string
		list      []wire.HubInboxAttachment
		sel       attachmentSelector
		wantParts []string
		wantErr   string
	}{
		{name: "no attachments", list: nil, wantErr: "no attachments"},

		{name: "explicit part", list: []wire.HubInboxAttachment{pdf, csv},
			sel: attachmentSelector{part: "2"}, wantParts: []string{"2"}},
		{name: "explicit part reaches an inline part", list: []wire.HubInboxAttachment{pdf, logo},
			sel: attachmentSelector{part: "3"}, wantParts: []string{"3"}},
		{name: "unknown part", list: []wire.HubInboxAttachment{pdf},
			sel: attachmentSelector{part: "9"}, wantErr: "no part 9"},

		{name: "by name", list: []wire.HubInboxAttachment{pdf, csv},
			sel: attachmentSelector{name: "rows"}, wantParts: []string{"2"}},
		{name: "by name is case-insensitive", list: []wire.HubInboxAttachment{pdf, csv},
			sel: attachmentSelector{name: "INVOICE"}, wantParts: []string{"1"}},
		{name: "by name reaches an inline part", list: []wire.HubInboxAttachment{pdf, logo},
			sel: attachmentSelector{name: "logo"}, wantParts: []string{"3"}},
		{name: "name matching nothing", list: []wire.HubInboxAttachment{pdf},
			sel: attachmentSelector{name: "nope"}, wantErr: `matches "nope"`},
		{name: "ambiguous name refuses", list: []wire.HubInboxAttachment{pdf, att("2", "invoice-2.pdf", false)},
			sel: attachmentSelector{name: "invoice"}, wantErr: "matches 2 attachments"},
		{name: "part and name together refuse", list: []wire.HubInboxAttachment{pdf},
			sel: attachmentSelector{part: "1", name: "invoice"}, wantErr: "not both"},

		{name: "single real attachment needs no flag", list: []wire.HubInboxAttachment{pdf, logo},
			wantParts: []string{"1"}},
		{name: "several refuse without --all", list: []wire.HubInboxAttachment{pdf, csv},
			wantErr: "pass --all"},
		{name: "several with --all", list: []wire.HubInboxAttachment{pdf, csv},
			sel: attachmentSelector{all: true}, wantParts: []string{"1", "2"}},
		{name: "inline only", list: []wire.HubInboxAttachment{logo}, wantErr: "--include-inline"},
		{name: "inline included", list: []wire.HubInboxAttachment{logo},
			sel: attachmentSelector{includeInline: true}, wantParts: []string{"3"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickAttachments(tc.list, tc.sel, "msg-1")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var parts []string
			for _, a := range got {
				parts = append(parts, a.PartID)
			}
			if strings.Join(parts, ",") != strings.Join(tc.wantParts, ",") {
				t.Fatalf("want parts %v, got %v", tc.wantParts, parts)
			}
		})
	}
}

// TestDownloadAddressesPartsByPartID is the regression guard for the bug that
// made `mm email download` fail every time it was tried: Gmail mints a fresh
// body.attachmentId on every messages.get, so an id read from one fetch never
// matches the payload of the next. The download request must therefore carry
// partId — the message's stable MIME position — and never attachmentId.
func TestDownloadAddressesPartsByPartID(t *testing.T) {
	src, err := os.ReadFile("email.go")
	if err != nil {
		t.Fatalf("read email.go: %v", err)
	}
	body := string(src)

	start := strings.Index(body, "func runEmailDownload(")
	if start < 0 {
		t.Fatal("runEmailDownload not found in email.go")
	}
	end := strings.Index(body[start:], "\nfunc ")
	if end < 0 {
		end = len(body) - start
	}
	fn := body[start : start+end]

	if !strings.Contains(fn, `"partId": a.PartID`) {
		t.Error(`runEmailDownload must send "partId": a.PartID in the email.attachment request`)
	}
	if strings.Contains(fn, `"attachmentId"`) {
		t.Error(`runEmailDownload must not send "attachmentId" — Gmail regenerates it per read, so it never resolves`)
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
		{"", "attachment-2"},
		{"...", "attachment-2"},
	}
	for _, tc := range tests {
		if got := safeFilename(tc.in, "2"); got != tc.want {
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

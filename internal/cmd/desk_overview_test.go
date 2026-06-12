package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"mm-cli/internal/wire"
)

func TestKindGlyph(t *testing.T) {
	cases := map[string]string{
		"artifact":  "✓",
		"resolved":  "✓",
		"decision":  "◆",
		"question":  "?",
		"open_loop": "⏳",
		"fact":      "•",
		"unknown":   "•",
		"":          "•",
	}
	for kind, want := range cases {
		if got := kindGlyph(kind); got != want {
			t.Errorf("kindGlyph(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestBucketLabel(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		ms   int64
		want string
	}{
		{"just now", now.Add(-1 * time.Hour).UnixMilli(), "today"},
		{"23h", now.Add(-23 * time.Hour).UnixMilli(), "today"},
		{"3 days", now.Add(-3 * 24 * time.Hour).UnixMilli(), "this week"},
		{"10 days", now.Add(-10 * 24 * time.Hour).UnixMilli(), "earlier"},
	}
	for _, c := range cases {
		if got := bucketLabel(c.ms); got != c.want {
			t.Errorf("bucketLabel(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

// captureStdout runs fn and returns everything it wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestRenderDeskOverview(t *testing.T) {
	now := time.Now()
	proj := "abc123"
	d := wire.AgentDeskOverview{
		WindowDays: 7,
		OpenLoops: []wire.AgentDeskEvent{
			{
				Kind:        "open_loop",
				Summary:     "drive --as md returns 503 from gateway export",
				ThreadTitle: "mm drive read",
				TS:          now.Add(-2 * 24 * time.Hour).UnixMilli(),
			},
		},
		Groups: []wire.AgentDeskGroup{
			{
				ProjectID: &proj,
				Label:     "mm-cli",
				Count:     2,
				Events: []wire.AgentDeskEvent{
					{Kind: "artifact", Summary: "shipped drive read commands", TS: now.Add(-2 * time.Hour).UnixMilli(), Refs: []string{"internal/cmd/drive.go"}},
					{Kind: "decision", Summary: "fold overview into bare mm desk", TS: now.Add(-4 * 24 * time.Hour).UnixMilli()},
				},
			},
		},
	}

	out := captureStdout(t, func() { renderDeskOverview(d) })

	wants := []string{
		"Desk — last 7 days",
		"⏳ Open loops",
		"drive --as md returns 503",
		"mm-cli  (2 events)",
		"today",
		"✓ shipped drive read commands",
		"→ internal/cmd/drive.go",
		"this week",
		"◆ fold overview into bare mm desk",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("render output missing %q\n--- got ---\n%s", w, out)
		}
	}
}

func TestRenderDeskOverviewEmpty(t *testing.T) {
	out := captureStdout(t, func() { renderDeskOverview(wire.AgentDeskOverview{WindowDays: 7}) })
	if !strings.Contains(out, "no activity yet") {
		t.Errorf("empty overview should hint at refresh, got:\n%s", out)
	}
}

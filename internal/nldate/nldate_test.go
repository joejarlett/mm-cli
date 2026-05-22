package nldate

import (
	"testing"
	"time"
)

// Anchor for the test matrix: 2026-05-22 14:00:00 local (Friday).
// Mirrors specs/go-port/04-nl-dates.md §4 coverage table.
func anchor(t *testing.T) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	return time.Date(2026, 5, 22, 14, 0, 0, 0, loc)
}

func TestParseDateTime(t *testing.T) {
	now := anchor(t)
	cases := []struct {
		in   string
		want string // local "YYYY-MM-DDTHH:MM:SS"
	}{
		{"2026-05-20 14:00", "2026-05-20T14:00:00"},
		{"2026-05-20T14:00", "2026-05-20T14:00:00"},
		{"tomorrow 14:00", "2026-05-23T14:00:00"},
		{"tomorrow 2pm", "2026-05-23T14:00:00"},
		{"next monday 10am", "2026-05-25T10:00:00"},
		{"next friday 14:00", "2026-05-29T14:00:00"},
		{"fri 09:00", "2026-05-29T09:00:00"}, // forward: today is Friday → next Friday
		{"in 2 hours", "2026-05-22T16:00:00"},
		{"in 30 minutes", "2026-05-22T14:30:00"},
		{"at 3pm", "2026-05-22T15:00:00"},  // today 15:00 — still future at 14:00 anchor
		{"at 10am", "2026-05-23T10:00:00"}, // past today → bump to tomorrow
		{"2026-05-20", "2026-05-20T00:00:00"},
		{"tomorrow", "2026-05-23T00:00:00"},
		{"in 3 days", "2026-05-25T14:00:00"}, // "in N days" preserves now's HH:MM
	}
	for _, c := range cases {
		got, err := ParseDateTime(c.in, now)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.in, err)
			continue
		}
		if got.ISO != c.want {
			t.Errorf("%q: got %s, want %s", c.in, got.ISO, c.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	now := anchor(t)
	cases := []struct {
		in   string
		want string
	}{
		{"2026-05-20", "2026-05-20"},
		{"tomorrow", "2026-05-23"},
		{"next friday", "2026-05-29"},
		{"in 3 days", "2026-05-25"},
		{"next week", "2026-06-01"}, // Monday of next week
		{"end of week", "2026-05-22"}, // anchor IS Friday → today
		{"end of month", "2026-05-31"},
	}
	for _, c := range cases {
		got, err := ParseDate(c.in, now)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.in, err)
			continue
		}
		if got.ISO != c.want {
			t.Errorf("%q: got %s, want %s", c.in, got.ISO, c.want)
		}
	}
}

func TestParseUnparseable(t *testing.T) {
	now := anchor(t)
	for _, in := range []string{"garbage", "the day after tomorrow", "next millennium"} {
		if _, err := ParseDateTime(in, now); err == nil {
			t.Errorf("%q: expected error, got success", in)
		}
	}
}

func TestParseEmpty(t *testing.T) {
	now := anchor(t)
	if _, err := ParseDateTime("", now); err == nil {
		t.Errorf("empty: expected error")
	}
	if _, err := ParseDateTime("   ", now); err == nil {
		t.Errorf("whitespace: expected error")
	}
}

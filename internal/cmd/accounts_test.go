package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"mm-cli/internal/wire"
)

func TestRenderAccounts(t *testing.T) {
	str := func(s string) *string { return &s }
	const manage = "https://meta-me.uk/settings/integrations/google"

	t.Run("nothing linked says how to connect one", func(t *testing.T) {
		var b bytes.Buffer
		renderAccounts(&b, wire.HubGoogleAccountsResp{ManageURL: manage})
		if want := "No Google accounts linked. Connect one at " + manage + "\n"; b.String() != want {
			t.Errorf("got %q, want %q", b.String(), want)
		}
	})

	t.Run("shows email, slug, default and reconnect", func(t *testing.T) {
		var b bytes.Buffer
		renderAccounts(&b, wire.HubGoogleAccountsResp{ManageURL: manage, Accounts: []wire.HubGoogleAccount{
			{Email: str("joe@example.com"), Slug: str("acc_1"), IsDefault: true},
			{Email: str("old@example.com"), Slug: str("acc_2"), NeedsRelink: true},
			{Email: str("new@example.com")},
		}})
		lines := strings.Split(b.String(), "\n")
		for i, want := range [][]string{
			{"joe@example.com", "acc_1", "default"},
			{"old@example.com", "acc_2", "needs reconnecting"},
			{"new@example.com", "—"},
		} {
			for _, field := range want {
				if !strings.Contains(lines[i], field) {
					t.Errorf("line %d = %q, missing %q", i, lines[i], field)
				}
			}
		}
		if strings.Contains(lines[1], "default") {
			t.Errorf("only the default should say so: %q", lines[1])
		}
		if !strings.Contains(b.String(), "Reconnect at "+manage) {
			t.Errorf("no reconnect pointer in %q", b.String())
		}
	})

	t.Run("no reconnect pointer when nothing needs it", func(t *testing.T) {
		var b bytes.Buffer
		renderAccounts(&b, wire.HubGoogleAccountsResp{ManageURL: manage, Accounts: []wire.HubGoogleAccount{
			{Email: str("joe@example.com"), Slug: str("acc_1"), IsDefault: true},
		}})
		if strings.Contains(b.String(), "Reconnect") {
			t.Errorf("unexpected pointer in %q", b.String())
		}
	})
}

func TestFutureDatesAreNotJustNow(t *testing.T) {
	// ducks4.aif's real modifiedTime, the file that raised the false alarm.
	const future = "2033-07-05T10:00:00Z"
	const want = "2033-07-05 (future date)"
	if got := fmtWhen(future); got != want {
		t.Errorf("fmtWhen = %q, want %q", got, want)
	}
	if got := fmtRelative(future); got != want {
		t.Errorf("fmtRelative = %q, want %q", got, want)
	}
}

func TestPastAndSkewedTimesFormatAsBefore(t *testing.T) {
	iso := func(d time.Duration) string { return time.Now().Add(d).Format(time.RFC3339) }
	old := time.Date(2021, 1, 11, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	cases := []struct {
		name, iso, drive, email string
	}{
		{"seconds ago", iso(-10 * time.Second), "just now", "just now"},
		{"a little clock skew", iso(2 * time.Minute), "just now", "just now"},
		{"minutes", iso(-5 * time.Minute), "5m", "5m ago"},
		{"hours", iso(-3 * time.Hour), "3h", "3h ago"},
		{"days", iso(-48 * time.Hour), "2d", "2d ago"},
		{"long ago", old, "11 Jan", "2021-01-11"},
	}
	for _, c := range cases {
		if got := fmtWhen(c.iso); got != c.drive {
			t.Errorf("fmtWhen(%s) = %q, want %q", c.name, got, c.drive)
		}
		if got := fmtRelative(c.iso); got != c.email {
			t.Errorf("fmtRelative(%s) = %q, want %q", c.name, got, c.email)
		}
	}
}

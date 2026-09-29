package card

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// meta-me.uk specs/api-standard.md, Wave 1: mm reads where an app's actions
// are from its card, and falls back to /api/v2 for an app that hasn't moved.
func serveCard(t *testing.T, body string, status int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent.json" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir()) // a fresh card cache
	t.Setenv("MM_KB_BASE_URL", srv.URL)
}

func TestActionsPath(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		want   string
	}{
		{"an app on the standard", `{"name":"kb","endpoint":"/api/actions"}`, 200, "/api/actions"},
		{"an app that hasn't moved", `{"name":"kb","chatUrl":"/api/v2"}`, 200, "/api/v2"},
		{"chatUrl alone never moves actions", `{"name":"kb","chatUrl":"/api/elsewhere"}`, 200, "/api/v2"},
		{"no card at all", `not found`, 404, "/api/v2"},
		{"a malformed card", `{"endpoint":"/api/actions"}`, 200, "/api/v2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			serveCard(t, tc.body, tc.status)
			if got := ActionsPath(context.Background(), "kb"); got != tc.want {
				t.Fatalf("ActionsPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestActionsPathNilCard(t *testing.T) {
	var c *Card
	if got := c.ActionsPath(); got != "/api/v2" {
		t.Fatalf("nil card ActionsPath = %q, want /api/v2", got)
	}
}

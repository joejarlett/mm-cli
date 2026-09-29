package manifest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The manifest is fetched at <endpoint>/manifest, the endpoint read from the
// card: /api/actions/manifest for an app on the standard, /api/v2/manifest
// for one that hasn't moved.
func TestFetchFollowsTheCard(t *testing.T) {
	cases := []struct {
		name, card, wantPath string
	}{
		{"on the standard", `{"name":"kb","endpoint":"/api/actions"}`, "/api/actions/manifest"},
		{"not moved", `{"name":"kb"}`, "/api/v2/manifest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hit string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/agent.json":
					_, _ = w.Write([]byte(tc.card))
				case "/api/actions/manifest", "/api/v2/manifest":
					hit = r.URL.Path
					_, _ = w.Write([]byte(`{"appSlug":"kb","version":"v2","endpoint":"/api/actions","features":{"notes":{"list":{"auth":"session","input":{},"output":{}}}}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("MM_KB_BASE_URL", srv.URL)

			m, err := Fetch(context.Background(), "kb")
			if err != nil {
				t.Fatal(err)
			}
			if hit != tc.wantPath {
				t.Fatalf("fetched %q, want %q", hit, tc.wantPath)
			}
			if m.Endpoint != "/api/actions" || m.ActionCount() != 1 {
				t.Fatalf("manifest not decoded: %+v", m)
			}
		})
	}
}

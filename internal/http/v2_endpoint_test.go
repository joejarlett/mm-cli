package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

// V2 posts to the endpoint it is given (the card's), /api/v2 when empty.
func TestV2Endpoint(t *testing.T) {
	for _, tc := range []struct{ endpoint, want string }{
		{"/api/actions", "/api/actions"},
		{"", "/api/v2"},
	} {
		var gotPath string
		var gotBody map[string]any
		srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			gotPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = w.Write([]byte(`{"items":[],"next":null}`))
		}))
		c := &Client{HTTPClient: srv.Client()}
		res, err := c.V2(context.Background(), srv.URL, "conversations.list", map[string]any{"limit": 5}, V2Opts{Endpoint: tc.endpoint})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if gotPath != tc.want || !res.OK {
			t.Fatalf("endpoint %q: posted to %q (ok=%v), want %q", tc.endpoint, gotPath, res.OK, tc.want)
		}
		if gotBody["feature"] != "conversations" || gotBody["action"] != "list" {
			t.Fatalf("body = %v", gotBody)
		}
	}
}

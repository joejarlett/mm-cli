package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mm-cli/internal/auth"
)

func rpcReturning(t *testing.T, status int, body string, out any) error {
	t.Helper()
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := &Client{HTTPClient: srv.Client(), Auth: &auth.State{Token: "t", UserID: "u"}}
	return c.Rpc(context.Background(), srv.URL, "/api/actions", "interaction", "log", nil, out)
}

// The maintenance page must never read as a successful write, whatever status
// it arrives with, and must not claim nothing landed: nginx serves the same page
// for a slow request whose write has already been saved.
func TestRpcMaintenancePageFails(t *testing.T) {
	const page = "<!doctype html><html><body>Back in a moment</body></html>"
	for _, status := range []int{200, 503} {
		for _, out := range []any{nil, &json.RawMessage{}} {
			err := rpcReturning(t, status, page, out)
			if err == nil {
				t.Fatalf("HTTP %d: expected an error", status)
			}
			msg := err.Error()
			if !strings.Contains(msg, "hub unreachable") || !strings.Contains(msg, "before retrying") {
				t.Fatalf("HTTP %d: message = %q", status, msg)
			}
			if strings.Contains(msg, "<html") || strings.Contains(msg, "nothing was written") {
				t.Fatalf("HTTP %d: message = %q", status, msg)
			}
		}
	}
}

func TestRpcOtherNonJSONFails(t *testing.T) {
	err := rpcReturning(t, 200, "upstream said no", nil)
	if err == nil || !strings.Contains(err.Error(), "non-JSON response") || !strings.Contains(err.Error(), "upstream said no") {
		t.Fatalf("err = %v", err)
	}
}

func TestRpcEnvelopesStillWork(t *testing.T) {
	var raw json.RawMessage
	if err := rpcReturning(t, 200, `{"data":[]}`, &raw); err != nil || string(raw) != `{"data":[]}` {
		t.Fatalf("success: err = %v, raw = %s", err, raw)
	}
	err := rpcReturning(t, 422, `{"errors":[{"detail":"text is required"}]}`, nil)
	if err == nil || err.Error() != "interaction.log: text is required" {
		t.Fatalf("envelope error: %v", err)
	}
	if err := rpcReturning(t, 204, "", nil); err != nil {
		t.Fatalf("empty success: %v", err)
	}
}

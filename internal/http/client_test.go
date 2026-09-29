package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mm-cli/internal/auth"
	"mm-cli/internal/config"
)

func hubReturning(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{
		HTTPClient: srv.Client(),
		Cfg:        &config.Config{HubURL: srv.URL},
		Auth:       &auth.State{Token: "t", UserID: "u"},
	}
}

// A reported server fault carries its ref, so the user has something to quote.
func TestHubFaultShowsRef(t *testing.T) {
	c := hubReturning(t, 502, `{"errors":[{"id":"a1b2c3d4","status":"502","code":"gateway_failure","title":"Couldn't create the draft in Gmail just now. It has been reported; try again shortly."}]}`)
	err := c.Hub(context.Background(), "email", "gmail.draft", nil, nil)
	want := "Couldn't create the draft in Gmail just now. It has been reported; try again shortly. (ref a1b2c3d4)"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

// The ref isn't repeated when the hub already put it in the sentence.
func TestHubFaultRefNotDoubled(t *testing.T) {
	c := hubReturning(t, 500, `{"errors":[{"id":"a1b2c3d4","status":"500","title":"Meta-Me couldn't complete x.y. It has been reported (ref a1b2c3d4)."}]}`)
	err := c.Hub(context.Background(), "x", "y", nil, nil)
	if err == nil || err.Error() != "Meta-Me couldn't complete x.y. It has been reported (ref a1b2c3d4)." {
		t.Fatalf("got %v", err)
	}
}

// A caller's own mistake has no ref and reads as the hub wrote it.
func TestHubUserErrorHasNoRef(t *testing.T) {
	c := hubReturning(t, 404, `{"errors":[{"status":"404","code":"no_google_account","title":"No Google account is connected for Gmail. Connect one at meta-me.uk/settings/integrations/google."}]}`)
	err := c.Hub(context.Background(), "email", "gmail.draft", nil, nil)
	if err == nil || err.Error() != "No Google account is connected for Gmail. Connect one at meta-me.uk/settings/integrations/google." {
		t.Fatalf("got %v", err)
	}
}

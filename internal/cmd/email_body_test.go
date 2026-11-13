package cmd

import "testing"

// Regression: `mm email draft --from … --text "…"` saved an empty Gmail draft
// and printed success, because the Gmail path only ever read --body.
func TestGmailBody(t *testing.T) {
	cases := []struct {
		name, body, text   string
		wantContent, wantT string
		wantErr            bool
	}{
		{"text only is used, plain", "", "Hi Nick,\n\nThanks", "Hi Nick,\n\nThanks", "plain", false},
		{"text with angle brackets stays plain", "", "Nick <nick@x.uk>", "Nick <nick@x.uk>", "plain", false},
		{"body wins over text", "<p>Hi</p>", "Hi", "<p>Hi</p>", "html", false},
		{"plain body", "Hi", "", "Hi", "plain", false},
		{"neither is an error", "", "", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, typ, err := gmailBody(c.body, c.text)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.wantContent || typ != c.wantT {
				t.Errorf("got (%q, %q), want (%q, %q)", got, typ, c.wantContent, c.wantT)
			}
		})
	}
}

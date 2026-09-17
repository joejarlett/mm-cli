package http

import (
	"errors"
	"testing"
)

// specs/go-port/01-wire.md §"Local agent REST". The rebuild against the live
// MagicDNS suffix is deliberate for our own nodes; it was silently wrong for a
// node shared in from another account, which is the case these pin down.
func TestNodeBaseURL(t *testing.T) {
	const local = "taildd974e.ts.net"
	noSuffix := errors.New("tailscale status: could not determine MagicDNS suffix")

	cases := []struct {
		name       string
		registered string
		isOwner    bool
		localSufx  string
		suffixErr  error
		want       string
		wantErr    bool
	}{{
		name:       "own tailnet is rebuilt against the live suffix",
		registered: "https://m4.taildd974e.ts.net:31415",
		isOwner:    true,
		localSufx:  local,
		want:       "https://m4.taildd974e.ts.net:31415",
	}, {
		// The reason the rebuild exists: a renamed tailnet heals itself. This
		// is why a differing suffix cannot by itself mean "someone else's".
		name:       "a stale suffix on our own node is corrected",
		registered: "https://m4.tailOLD123.ts.net:31415",
		isOwner:    true,
		localSufx:  local,
		want:       "https://m4.taildd974e.ts.net:31415",
	}, {
		// The bug. Reattaching our suffix invented a host that does not exist.
		name:       "a shared node keeps its owner's hostname",
		registered: "https://dees-imac.tail1d5901.ts.net:31415",
		isOwner:    false,
		localSufx:  local,
		want:       "https://dees-imac.tail1d5901.ts.net:31415",
	}, {
		name:       "a bare registration is still rebuilt",
		registered: "https://m1:31415",
		isOwner:    true,
		localSufx:  local,
		want:       "https://m1.taildd974e.ts.net:31415",
	}, {
		name:       "https default port",
		registered: "https://m4.taildd974e.ts.net",
		isOwner:    true,
		localSufx:  local,
		want:       "https://m4.taildd974e.ts.net:443",
	}, {
		name:       "http default port",
		registered: "http://m4.taildd974e.ts.net",
		isOwner:    true,
		localSufx:  local,
		want:       "https://m4.taildd974e.ts.net:80",
	}, {
		// A shared node never needed the local tailscaled.
		name:       "a shared node survives an unavailable tailscaled",
		registered: "https://dees-imac.tail1d5901.ts.net:31415",
		isOwner:    false,
		suffixErr:  noSuffix,
		want:       "https://dees-imac.tail1d5901.ts.net:31415",
	}, {
		name:       "our own fully-qualified node survives an unavailable tailscaled",
		registered: "https://m4.taildd974e.ts.net:31415",
		isOwner:    true,
		suffixErr:  noSuffix,
		want:       "https://m4.taildd974e.ts.net:31415",
	}, {
		name:       "a bare node without a local suffix is unrecoverable",
		registered: "https://m1:31415",
		isOwner:    true,
		suffixErr:  noSuffix,
		wantErr:    true,
	}, {
		name:       "a URL with no host is an error, not an empty dial",
		registered: "not-a-url",
		isOwner:    true,
		localSufx:  local,
		wantErr:    true,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nodeBaseURL(tc.registered, tc.isOwner, tc.localSufx, tc.suffixErr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

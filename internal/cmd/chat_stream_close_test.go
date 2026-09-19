package cmd

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/coder/websocket"
)

// The agent finishing a turn must not read as the connection failing.
//
// Regression for the 2026-09-19 hang: `mm desk send` completed a turn and then
// reconnected and resumed once a second for ever, because every read error
// reached the reconnect branch once a cursor existed.
func TestIsCleanClose(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil is not a close", nil, false},
		{"normal closure", websocket.CloseError{Code: websocket.StatusNormalClosure}, true},
		{"going away", websocket.CloseError{Code: websocket.StatusGoingAway}, true},
		{"no status received", websocket.CloseError{Code: websocket.StatusNoStatusRcvd}, true},
		// An abnormal closure is a real drop and must still reconnect - that
		// branch is the reason the reconnect logic exists at all.
		{"abnormal closure still reconnects", websocket.CloseError{Code: websocket.StatusAbnormalClosure}, false},
		{"internal error still reconnects", websocket.CloseError{Code: websocket.StatusInternalError}, false},
		{"bare EOF", io.EOF, true},
		{"wrapped EOF", fmt.Errorf("ws read: %w", io.EOF), true},
		// The transport wraps EOF in its own type on some paths, so errors.Is
		// does not see it and the text is the only signal left.
		{"EOF only in the text", errors.New("failed to read frame header: EOF"), true},
		{"a genuine network failure is not clean", errors.New("dial tcp: connection refused"), false},
		{"a timeout is not clean", errors.New("context deadline exceeded"), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isCleanClose(c.err); got != c.want {
				t.Fatalf("isCleanClose(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

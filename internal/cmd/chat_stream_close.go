package cmd

import (
	"errors"
	"io"
	"strings"

	"github.com/coder/websocket"
)

// isCleanClose reports whether a WebSocket read error is the peer finishing
// rather than the connection failing.
//
// **The distinction the stream loop could not make.** `desk-agent` closes the
// socket once a turn is done, which arrives here as an ordinary read error -
// and `chat_send`'s reconnect branch treated every read error as a drop the
// moment a cursor existed, which is after the first frame of every turn. So a
// completed turn reconnected, resumed, read the close again and reconnected
// again, once a second, indefinitely. Joe hit it on 2026-09-19; the tell was
// the agent logging the identical `[resume] … done=true` line at 1 Hz.
//
// Three spellings, because they come from three layers and only the first is
// the library's own:
//
//   - `websocket.CloseError` for a close handshake, normal or going-away;
//   - `io.EOF` for a socket that ended without one;
//   - the string, because the transport wraps EOF in its own error type on
//     some paths and `errors.Is` does not see through that. Matching on text
//     is a last resort and it is deliberate here: the cost of missing a clean
//     close is an infinite loop, and the cost of a false positive is one
//     turn ending a second early with its output already printed.
func isCleanClose(err error) bool {
	if err == nil {
		return false
	}

	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Code == websocket.StatusNormalClosure ||
			ce.Code == websocket.StatusGoingAway ||
			ce.Code == websocket.StatusNoStatusRcvd
	}

	if errors.Is(err, io.EOF) {
		return true
	}

	return strings.Contains(err.Error(), "EOF")
}

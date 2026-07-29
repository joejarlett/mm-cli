package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// sseEvent renders one hub-shaped SSE event: a single `data:` line + blank line.
func sseEvent(t *testing.T, typ string, pcm []byte) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{
		"type":  typ,
		"audio": base64.StdEncoding.EncodeToString(pcm),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return fmt.Sprintf("data: %s\n\n", b)
}

// The regression this file exists for (konte D6). The hub can send a whole
// utterance as ONE `data:` line. consumePCMSSE used a bufio.Scanner capped at
// 1 MiB, so that cap was really a limit on audio LENGTH — ~786 KB of PCM, ~24s,
// ~250 characters of input — and it surfaced as `token too long`, which looks
// like a protocol fault rather than "your text was too long". Three of a video's
// 22 voice jobs failed this way, and they were exactly the three longest lines.
func TestConsumePCMSSE_SingleOversizedEvent(t *testing.T) {
	// 4 MB of PCM in one event — comfortably past the old 1 MiB token cap, and
	// past the base64-inflated wire size of it too.
	pcm := make([]byte, 4<<20)
	for i := range pcm {
		pcm[i] = byte(i % 251)
	}
	stream := sseEvent(t, "chunk", pcm) + sseEvent(t, "done", nil)

	got, err := consumePCMSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(pcm) {
		t.Fatalf("got %d bytes, want %d", len(got), len(pcm))
	}
	if string(got) != string(pcm) {
		t.Fatal("payload mismatch")
	}
}

func TestConsumePCMSSE_ConcatenatesChunks(t *testing.T) {
	a, b := []byte("first-chunk-pcm"), []byte("second-chunk-pcm")
	stream := sseEvent(t, "chunk", a) + sseEvent(t, "chunk", b) + sseEvent(t, "done", nil)

	got, err := consumePCMSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := string(a) + string(b); string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A stream that ends without a trailing blank line must still yield its audio —
// the old code had a flush path for this and it must survive the Reader rewrite.
func TestConsumePCMSSE_NoTrailingBlankLine(t *testing.T) {
	pcm := []byte("tail-without-newline")
	b, _ := json.Marshal(map[string]string{
		"type":  "chunk",
		"audio": base64.StdEncoding.EncodeToString(pcm),
	})
	stream := fmt.Sprintf("data: %s", b) // no \n\n

	got, err := consumePCMSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(pcm) {
		t.Fatalf("got %q, want %q", got, pcm)
	}
}

// `done` terminates the stream; anything after it is ignored.
func TestConsumePCMSSE_StopsAtDone(t *testing.T) {
	stream := sseEvent(t, "chunk", []byte("kept")) +
		sseEvent(t, "done", nil) +
		sseEvent(t, "chunk", []byte("ignored"))

	got, err := consumePCMSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "kept" {
		t.Fatalf("got %q, want %q", got, "kept")
	}
}

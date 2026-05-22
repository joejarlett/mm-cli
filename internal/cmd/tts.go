package cmd

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"mm-cli/internal/auth"
	"mm-cli/internal/config"
)

const ttsSampleRate = 24000

// NewTtsCmd builds `mm tts <text>`.
func NewTtsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "tts [text]",
		Short: "Synthesise speech",
		Long:  "Stream WAV to stdout, or use --out to write a file, --play to play, --voice to pick a voice.",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runTts,
	}
	c.Flags().String("out", "", "Output file (mp3 if .mp3, otherwise wav)")
	c.Flags().Bool("play", false, "Synthesise + play")
	c.Flags().String("voice", "af_heart", "Voice id")
	c.Flags().String("format", "", "Output format: wav|mp3 (defaults to wav, or mp3 if --out ends .mp3)")
	return c
}

func runTts(cmd *cobra.Command, args []string) error {
	text := strings.Join(args, " ")
	out, _ := cmd.Flags().GetString("out")
	play, _ := cmd.Flags().GetBool("play")
	voice, _ := cmd.Flags().GetString("voice")
	format, _ := cmd.Flags().GetString("format")
	if format == "" {
		if strings.HasSuffix(strings.ToLower(out), ".mp3") {
			format = "mp3"
		} else {
			format = "wav"
		}
	}

	if text == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		text = strings.TrimSpace(string(b))
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("text input is empty")
	}

	state, err := auth.MustLoad()
	if err != nil {
		return err
	}
	cfg := config.Load()

	bodyJSON, _ := json.Marshal(map[string]string{"text": text, "voice": voice})
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost,
		cfg.HubURL+"/api/tts/stream", bytes.NewReader(bodyJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+state.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 || resp.Body == nil {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("TTS failed (%d): %s", resp.StatusCode, truncString(string(body), 200))
	}

	pcm, err := consumePCMSSE(resp.Body)
	if err != nil {
		return err
	}
	if len(pcm) == 0 {
		return fmt.Errorf("TTS returned no audio")
	}
	wav := wrapWAV(pcm, ttsSampleRate)

	var bytesOut []byte
	if format == "mp3" {
		bytesOut, err = wavToMP3(wav)
		if err != nil {
			return err
		}
	} else {
		bytesOut = wav
	}

	if play {
		dir, err := os.MkdirTemp("", "mm-tts-")
		if err != nil {
			return err
		}
		name := "out.wav"
		if format == "mp3" {
			name = "out.mp3"
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, bytesOut, 0o600); err != nil {
			return err
		}
		return playFile(path)
	}
	if out != "" {
		if err := os.WriteFile(out, bytesOut, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %d bytes to %s\n", len(bytesOut), out)
		return nil
	}
	_, err = os.Stdout.Write(bytesOut)
	return err
}

// consumePCMSSE parses the SSE event stream the hub emits and returns the
// concatenated base64-decoded PCM chunks.
func consumePCMSSE(body io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(body)
	// 1 MB max event size — TTS chunks are small but headroom doesn't hurt.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var (
		out  bytes.Buffer
		evt  strings.Builder
		done bool
	)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			evt.WriteString(line)
			evt.WriteByte('\n')
			continue
		}
		// blank line = event boundary
		processed, terminate := processSSEEvent(evt.String(), &out)
		evt.Reset()
		if processed && terminate {
			done = true
			break
		}
	}
	// flush final partial event in case stream ended without trailing blank
	if !done {
		_, _ = processSSEEvent(evt.String(), &out)
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return nil, err
	}
	return out.Bytes(), nil
}

// processSSEEvent extracts the `data:` payload from a raw event block,
// JSON-decodes it, and writes its base64 audio chunk into `out` (if any).
// Returns (processed, terminate). `terminate=true` on a {type:"done"} event.
func processSSEEvent(raw string, out *bytes.Buffer) (bool, bool) {
	if strings.TrimSpace(raw) == "" {
		return false, false
	}
	for _, ln := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(ln, "data: ") {
			continue
		}
		payload := ln[len("data: "):]
		var evt struct {
			Type  string `json:"type"`
			Audio string `json:"audio"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}
		if evt.Type == "chunk" && evt.Audio != "" {
			b, err := base64.StdEncoding.DecodeString(evt.Audio)
			if err == nil {
				out.Write(b)
			}
		}
		if evt.Type == "done" {
			return true, true
		}
	}
	return true, false
}

// wrapWAV prepends a 44-byte PCM16-mono RIFF header to raw PCM bytes.
func wrapWAV(pcm []byte, sampleRate uint32) []byte {
	dataLen := uint32(len(pcm))
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], 36+dataLen)
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // PCM chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM format
	binary.LittleEndian.PutUint16(buf[22:24], 1)  // mono
	binary.LittleEndian.PutUint32(buf[24:28], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:32], sampleRate*2) // byte rate
	binary.LittleEndian.PutUint16(buf[32:34], 2)            // block align
	binary.LittleEndian.PutUint16(buf[34:36], 16)           // bits per sample
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], dataLen)
	copy(buf[44:], pcm)
	return buf
}

// wavToMP3 pipes WAV bytes through ffmpeg and returns the MP3 bytes.
func wavToMP3(wav []byte) ([]byte, error) {
	ff := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", "pipe:0", "-f", "mp3", "-q:a", "4", "pipe:1")
	ff.Stdin = bytes.NewReader(wav)
	var stdout, stderr bytes.Buffer
	ff.Stdout = &stdout
	ff.Stderr = &stderr
	if err := ff.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %s", strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func playFile(path string) error {
	var player string
	switch runtime.GOOS {
	case "darwin":
		player = "afplay"
	case "linux":
		player = "aplay"
	default:
		fmt.Fprintf(os.Stderr, "No supported player for %s; wrote to %s\n", runtime.GOOS, path)
		return nil
	}
	c := exec.Command(player, path)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

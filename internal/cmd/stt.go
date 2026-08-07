package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/auth"
	"mm-cli/internal/config"
)

// NewSttCmd builds `mm stt <file>`.
func NewSttCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "stt [file]",
		Short: "Transcribe audio (wav/mp3/m4a/…)",
		Long: "Pass a file path or `-` to read audio bytes from stdin. WAV is the fast path; other formats use server-side ffmpeg.\n\n" +
			"With --speakers, the audio is diarized: each line of the transcript is attributed to a speaker.\n" +
			"That path is asynchronous server-side (a long recording is minutes of work), so the command\n" +
			"blocks and reports progress while the job runs.",
		Example: "  mm stt memo.m4a\n" +
			"  mm stt meeting.m4a --speakers\n" +
			"  mm stt interview.wav --speakers=2\n" +
			"  mm stt meeting.m4a --speakers --json",
		Args: cobra.ExactArgs(1),
		RunE: runStt,
	}
	c.Flags().String("speakers", "", "Diarize by speaker. Bare flag auto-detects; --speakers=N pins the count")
	// Lets `--speakers` work as a bare flag (auto) as well as `--speakers=3`.
	c.Flags().Lookup("speakers").NoOptDefVal = "auto"
	c.Flags().Int("min-speakers", 0, "Lower bound on speaker count (ignored if --speakers=N)")
	c.Flags().Int("max-speakers", 0, "Upper bound on speaker count (ignored if --speakers=N)")
	c.Flags().String("language", "en", "ASR language; empty string auto-detects")
	return c
}

func runStt(cmd *cobra.Command, args []string) error {
	if len(args) > 0 && args[0] == "help" {
		return cmd.Help()
	}
	src := args[0]
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	if cmd.Flags().Changed("speakers") {
		return runSttConversation(cmd, src, wantJSON)
	}

	state, err := auth.MustLoad()
	if err != nil {
		return err
	}
	cfg := config.Load()

	var audio []byte
	if src == "-" {
		audio, err = io.ReadAll(os.Stdin)
	} else {
		audio, err = os.ReadFile(src)
	}
	if err != nil {
		return err
	}
	if len(audio) == 0 {
		return fmt.Errorf("audio input is empty")
	}

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost,
		cfg.HubURL+"/api/stt/transcribe", bytes.NewReader(audio))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+state.Token)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("STT failed (%d): %s", resp.StatusCode, truncString(string(body), 200))
	}

	var data struct {
		Text      string  `json:"text"`
		DurationS float64 `json:"duration_s"`
		InferMs   float64 `json:"infer_ms"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("stt: invalid JSON: %w", err)
	}
	if wantJSON {
		fmt.Println(string(body))
		return nil
	}
	fmt.Println(data.Text)
	return nil
}

// ── Diarized (multi-speaker) transcription ───────────────────────────

// sttSegment is one contiguous stretch of speech from a single speaker.
type sttSegment struct {
	Speaker string  `json:"speaker"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
}

type sttJob struct {
	JobID    string  `json:"job_id"`
	Status   string  `json:"status"`
	Stage    string  `json:"stage"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error"`
	Result   *struct {
		Segments    []sttSegment `json:"segments"`
		Text        string       `json:"text"`
		DurationS   float64      `json:"duration_s"`
		NumSpeakers int          `json:"num_speakers"`
		Speakers    []struct {
			Speaker   string  `json:"speaker"`
			SpeakingS float64 `json:"speaking_s"`
			Words     int     `json:"words"`
		} `json:"speakers"`
	} `json:"result"`
}

// runSttConversation submits a diarization job and blocks until it
// finishes. Unlike plain `mm stt` this is a two-call protocol server-side
// (POST to queue, GET to poll) because a long recording takes minutes —
// far longer than any HTTP client or proxy will hold a connection open.
// The CLI hides that: from the user's side it's still one command that
// prints a transcript.
func runSttConversation(cmd *cobra.Command, src string, wantJSON bool) error {
	speakers, _ := cmd.Flags().GetString("speakers")
	minSpeakers, _ := cmd.Flags().GetInt("min-speakers")
	maxSpeakers, _ := cmd.Flags().GetInt("max-speakers")
	language, _ := cmd.Flags().GetString("language")

	qs := url.Values{}
	if speakers != "" && speakers != "auto" {
		n, err := strconv.Atoi(speakers)
		if err != nil || n < 1 || n > 20 {
			return fmt.Errorf("--speakers must be `auto` or a number 1-20, got %q", speakers)
		}
		qs.Set("num_speakers", strconv.Itoa(n))
	} else {
		if minSpeakers > 0 {
			qs.Set("min_speakers", strconv.Itoa(minSpeakers))
		}
		if maxSpeakers > 0 {
			qs.Set("max_speakers", strconv.Itoa(maxSpeakers))
		}
	}
	qs.Set("language", language)

	state, err := auth.MustLoad()
	if err != nil {
		return err
	}
	cfg := config.Load()

	var audio []byte
	if src == "-" {
		audio, err = io.ReadAll(os.Stdin)
	} else {
		audio, err = os.ReadFile(src)
	}
	if err != nil {
		return err
	}
	if len(audio) == 0 {
		return fmt.Errorf("audio input is empty")
	}

	submitURL := cfg.HubURL + "/api/stt/conversation?" + qs.Encode()
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, submitURL, bytes.NewReader(audio))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+state.Token)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("STT failed (%d): %s", resp.StatusCode, truncString(string(body), 300))
	}

	var job sttJob
	if err := json.Unmarshal(body, &job); err != nil {
		return fmt.Errorf("stt: invalid JSON: %w", err)
	}
	if job.JobID == "" {
		return fmt.Errorf("stt: server returned no job id")
	}

	// Progress goes to stderr so `mm stt x.m4a --speakers > out.txt` still
	// produces a clean transcript file.
	quiet := wantJSON
	if !quiet {
		fmt.Fprintf(os.Stderr, "job %s queued…\n", job.JobID)
	}

	pollURL := fmt.Sprintf("%s/api/stt/conversation/%s", cfg.HubURL, job.JobID)
	lastStage := ""
	for {
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(2 * time.Second):
		}

		preq, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, pollURL, nil)
		if err != nil {
			return err
		}
		preq.Header.Set("Authorization", "Bearer "+state.Token)
		presp, err := http.DefaultClient.Do(preq)
		if err != nil {
			return err
		}
		pbody, _ := io.ReadAll(presp.Body)
		presp.Body.Close()
		if presp.StatusCode/100 != 2 {
			return fmt.Errorf("STT poll failed (%d): %s", presp.StatusCode, truncString(string(pbody), 300))
		}
		if err := json.Unmarshal(pbody, &job); err != nil {
			return fmt.Errorf("stt: invalid JSON: %w", err)
		}

		if !quiet && job.Stage != lastStage {
			fmt.Fprintf(os.Stderr, "  [%3.0f%%] %s\n", job.Progress*100, job.Stage)
			lastStage = job.Stage
		}

		switch job.Status {
		case "done":
			if wantJSON {
				fmt.Println(string(pbody))
				return nil
			}
			printDiarizedTranscript(&job)
			return nil
		case "error":
			return fmt.Errorf("STT job failed: %s", job.Error)
		}
	}
}

// printDiarizedTranscript renders the speaker-labelled transcript.
//
// pyannote emits opaque labels (SPEAKER_00, SPEAKER_01…) whose numbering
// reflects clustering order, not who spoke first. We renumber by first
// appearance so "Speaker 1" is whoever opened the conversation, which is
// what a reader expects.
func printDiarizedTranscript(job *sttJob) {
	if job.Result == nil {
		return
	}
	names := map[string]string{}
	order := 0
	label := func(raw string) string {
		if raw == "" {
			return "Unknown"
		}
		if n, ok := names[raw]; ok {
			return n
		}
		order++
		names[raw] = fmt.Sprintf("Speaker %d", order)
		return names[raw]
	}

	for _, seg := range job.Result.Segments {
		fmt.Printf("[%s] %s: %s\n", fmtTimestamp(seg.Start), label(seg.Speaker), seg.Text)
	}

	if len(job.Result.Segments) == 0 && job.Result.Text != "" {
		// Diarization found no turns but whisper heard something.
		fmt.Println(job.Result.Text)
	}

	fmt.Fprintf(os.Stderr, "\n%d speaker(s), %s of audio\n",
		job.Result.NumSpeakers, fmtTimestamp(job.Result.DurationS))
}

func fmtTimestamp(seconds float64) string {
	total := int(seconds)
	if total >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", total/3600, (total%3600)/60, total%60)
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

// dummy reference to keep context import live across Go versions.
var _ = context.Background

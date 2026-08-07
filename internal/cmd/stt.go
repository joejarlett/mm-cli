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
			"That runs as a server-side job at roughly 0.7x realtime — an hour-long meeting takes about\n" +
			"45 minutes — so by default the command blocks and reports progress.\n\n" +
			"For anything long, prefer --detach: it prints a job id and exits, and you pick the result up\n" +
			"later with `mm stt job <id>`. The job survives the client going away either way, so an\n" +
			"interrupted wait is never lost work — reattach rather than resubmitting.",
		Example: "  mm stt memo.m4a\n" +
			"  mm stt meeting.m4a --speakers\n" +
			"  mm stt interview.wav --speakers=2\n" +
			"  mm stt meeting.m4a --speakers --detach\n" +
			"  mm stt job 0d1fbb7bb9fb44de88cba47248a8c5e7\n" +
			"  mm stt job 0d1fbb7bb9fb44de88cba47248a8c5e7 --wait",
		Args: cobra.ExactArgs(1),
		RunE: runStt,
	}
	c.Flags().String("speakers", "", "Diarize by speaker. Bare flag auto-detects; --speakers=N pins the count")
	// Lets `--speakers` work as a bare flag (auto) as well as `--speakers=3`.
	c.Flags().Lookup("speakers").NoOptDefVal = "auto"
	c.Flags().Int("min-speakers", 0, "Lower bound on speaker count (ignored if --speakers=N)")
	c.Flags().Int("max-speakers", 0, "Upper bound on speaker count (ignored if --speakers=N)")
	c.Flags().String("language", "en", "ASR language; empty string auto-detects")
	c.Flags().Bool("detach", false, "Submit and exit, printing the job id (use for long recordings)")
	c.AddCommand(newSttJobCmd())
	return c
}

// newSttJobCmd builds `mm stt job <id>` — reattach to a running or
// finished diarization job.
//
// This exists because diarization runs at roughly 0.7x realtime, so an
// hour-long meeting is a ~45-minute job. Nothing sensible waits that long
// on one connection: an agent's tool call caps out well before it, and a
// shell will usually be interrupted first. Without a way back to a job,
// a timeout looks like a failure — and the obvious response, retrying,
// queues a *second* 45-minute job behind the first on a single-worker
// queue and makes everything worse.
func newSttJobCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "job [job-id]",
		Short: "Check or resume a diarization job",
		Long: "Reattach to a job started with `mm stt <file> --speakers --detach`.\n\n" +
			"Without --wait it prints the current status and exits, which is the right\n" +
			"shape for an agent or a cron: check, go away, check again.",
		Example: "  mm stt job 0d1fbb7bb9fb44de88cba47248a8c5e7\n" +
			"  mm stt job 0d1fbb7bb9fb44de88cba47248a8c5e7 --wait",
		Args: cobra.ExactArgs(1),
		RunE: runSttJob,
	}
	c.Flags().Bool("wait", false, "Block until the job finishes, then print the transcript")
	return c
}

func runSttJob(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	wait, _ := cmd.Flags().GetBool("wait")

	state, err := auth.MustLoad()
	if err != nil {
		return err
	}
	cfg := config.Load()

	if wait {
		return pollUntilDone(cmd, cfg.HubURL, state.Token, args[0], wantJSON)
	}

	job, body, err := fetchJob(cmd, cfg.HubURL, state.Token, args[0])
	if err != nil {
		return err
	}
	if wantJSON {
		fmt.Println(string(body))
		return nil
	}
	switch job.Status {
	case "done":
		printDiarizedTranscript(job)
	case "error":
		return fmt.Errorf("STT job failed: %s", job.Error)
	default:
		fmt.Fprintf(os.Stderr, "%s: %s (%.0f%%)\nNot finished — re-run with --wait to block, or check again later.\n",
			job.Status, job.Stage, job.Progress*100)
	}
	return nil
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
	// SvelteKit content-negotiates its error(): without this we get an
	// HTML error page, which is unreadable in a terminal.
	req.Header.Set("Accept", "application/json")

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
	JobID     string  `json:"job_id"`
	Status    string  `json:"status"`
	Stage     string  `json:"stage"`
	Progress  float64 `json:"progress"`
	Error     string  `json:"error"`
	DurationS float64 `json:"duration_s"`
	EtaS      int     `json:"eta_s"`
	Result    *struct {
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
	// SvelteKit content-negotiates its error(): without this we get an
	// HTML error page, which is unreadable in a terminal.
	req.Header.Set("Accept", "application/json")

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

	// Detached: hand back the id and get out of the way. This is the right
	// mode for anything long — an hour-long meeting is a ~45-minute job,
	// which outlives an agent's tool-call timeout and most people's
	// patience. The job id on stdout is the whole point; everything else
	// goes to stderr so `--detach` composes in a pipeline.
	if detach, _ := cmd.Flags().GetBool("detach"); detach {
		if wantJSON {
			fmt.Println(string(body))
			return nil
		}
		fmt.Println(job.JobID)
		msg := fmt.Sprintf("submitted (%s of audio", fmtTimestamp(job.DurationS))
		if job.EtaS > 0 {
			msg += fmt.Sprintf(", ~%s to process", fmtTimestamp(float64(job.EtaS)))
		}
		fmt.Fprintf(os.Stderr, "%s)\n  mm stt job %s\n", msg, job.JobID)
		return nil
	}

	if !wantJSON {
		eta := ""
		if job.EtaS > 0 {
			eta = fmt.Sprintf(" (~%s)", fmtTimestamp(float64(job.EtaS)))
		}
		fmt.Fprintf(os.Stderr, "job %s queued%s…\n", job.JobID, eta)
		if job.EtaS > 540 {
			// Warn before they lose a long wait to a shell timeout rather
			// than after.
			fmt.Fprintf(os.Stderr,
				"  long job — if this is interrupted, resume with: mm stt job %s --wait\n", job.JobID)
		}
	}

	return pollUntilDone(cmd, cfg.HubURL, state.Token, job.JobID, wantJSON)
}

// fetchJob reads a job's current state once.
func fetchJob(cmd *cobra.Command, hubURL, token, jobID string) (*sttJob, []byte, error) {
	url := fmt.Sprintf("%s/api/stt/conversation/%s", hubURL, jobID)
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, nil, fmt.Errorf("STT poll failed (%d): %s", resp.StatusCode, truncString(string(body), 300))
	}

	var job sttJob
	if err := json.Unmarshal(body, &job); err != nil {
		return nil, nil, fmt.Errorf("stt: invalid JSON: %w", err)
	}
	return &job, body, nil
}

// pollUntilDone blocks on a job, reporting stage changes to stderr so a
// redirected stdout still yields a clean transcript.
func pollUntilDone(cmd *cobra.Command, hubURL, token, jobID string, wantJSON bool) error {
	lastStage := ""
	for {
		job, body, err := fetchJob(cmd, hubURL, token, jobID)
		if err != nil {
			return err
		}

		if !wantJSON && job.Stage != lastStage {
			fmt.Fprintf(os.Stderr, "  [%3.0f%%] %s\n", job.Progress*100, job.Stage)
			lastStage = job.Stage
		}

		switch job.Status {
		case "done":
			if wantJSON {
				fmt.Println(string(body))
				return nil
			}
			printDiarizedTranscript(job)
			return nil
		case "error":
			return fmt.Errorf("STT job failed: %s", job.Error)
		}

		select {
		case <-cmd.Context().Done():
			// Interrupted, not failed — make the way back obvious.
			fmt.Fprintf(os.Stderr, "\ninterrupted; the job is still running:\n  mm stt job %s --wait\n", jobID)
			return cmd.Context().Err()
		case <-time.After(5 * time.Second):
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

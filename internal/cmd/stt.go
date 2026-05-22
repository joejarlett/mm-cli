package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"mm-cli/internal/auth"
	"mm-cli/internal/config"
)

// NewSttCmd builds `mm stt <file>`.
func NewSttCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stt [file]",
		Short: "Transcribe audio (wav/mp3/m4a/…)",
		Long:  "Pass a file path or `-` to read audio bytes from stdin. WAV is the fast path; other formats use server-side ffmpeg.",
		Args:  cobra.ExactArgs(1),
		RunE:  runStt,
	}
}

func runStt(cmd *cobra.Command, args []string) error {
	if len(args) > 0 && args[0] == "help" {
		return cmd.Help()
	}
	src := args[0]
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

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
		Text       string  `json:"text"`
		DurationS  float64 `json:"duration_s"`
		InferMs    float64 `json:"infer_ms"`
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

// dummy reference to keep context import live across Go versions.
var _ = context.Background

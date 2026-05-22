package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// Overridable for tests.
var (
	captureCreateFunc = func(ctx context.Context, req wire.HubCaptureCreateReq) (wire.HubCaptureCreateResp, error) {
		client := http.New()
		var resp wire.HubCaptureCreateResp
		err := client.Hub(ctx, "capture", "create", req, &resp)
		return resp, err
	}
	captureListFunc = func(ctx context.Context, req wire.HubCaptureListReq) (wire.HubCaptureListResp, error) {
		client := http.New()
		var resp wire.HubCaptureListResp
		err := client.Hub(ctx, "capture", "list", req, &resp)
		return resp, err
	}
	captureClassifyFunc = func(ctx context.Context, req wire.HubCaptureClassifyReq) (wire.HubCaptureClassifyResp, error) {
		client := http.New()
		var resp wire.HubCaptureClassifyResp
		err := client.Hub(ctx, "capture", "classify", req, &resp)
		return resp, err
	}
	captureApproveFunc = func(ctx context.Context, req wire.HubCaptureApproveReq) (wire.HubCaptureApproveResp, error) {
		client := http.New()
		var resp wire.HubCaptureApproveResp
		err := client.Hub(ctx, "capture", "approve", req, &resp)
		return resp, err
	}
	captureDiscardFunc = func(ctx context.Context, req wire.HubCaptureDiscardReq) (wire.HubCaptureDiscardResp, error) {
		client := http.New()
		var resp wire.HubCaptureDiscardResp
		err := client.Hub(ctx, "capture", "discard", req, &resp)
		return resp, err
	}
)

// NewCaptureCmd builds the `mm capture` command — drops text into the
// capture inbox at meta-me.uk for later classification + review. Same
// destination as the menubar `mm-tray` binary's textarea.
func NewCaptureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capture [text]",
		Short: "Drop a note into the capture inbox",
		Long: `Capture freeform text into the capture inbox. Lands at meta-me.uk/capture.

Reads from positional args, or from stdin if no args given.

With --auto, chains capture → classify → approve in one shot. The
approval only fires when the proposal clears the auto-fire threshold
(confidence ≥0.95, target is wired, not in the hard safety floor).
Anything below auto-fire prints the proposal and exits — run
'mm capture approve <id>' to commit it manually.

Examples:
  mm capture "research EdTech companies in Bristol"
  mm capture "buy milk" --auto                       # capture → classify → fire
  echo "follow up with Alex on Tuesday" | mm capture
  mm capture list                                    # recent captures
`,
		Args: cobra.ArbitraryArgs,
		RunE: runCaptureSubmit,
	}
	cmd.Flags().Bool("auto", false, "Auto-classify and auto-approve when the proposal clears the safe threshold")
	cmd.AddCommand(newCaptureListCmd())
	cmd.AddCommand(newCaptureClassifyCmd())
	cmd.AddCommand(newCaptureApproveCmd())
	cmd.AddCommand(newCaptureDiscardCmd())
	return cmd
}

func newCaptureApproveCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "approve <capture-id>",
		Short: "Approve and fire the proposed action",
		Long: `Reads the persisted proposal for the capture and dispatches the
target RPC. Hard safety floor: email.send, crm.interaction.log, and
calendar events with attendees require --override even at high confidence.`,
		Args: cobra.ExactArgs(1),
		RunE: runCaptureApprove,
	}
	c.Flags().Bool("override", false, "Bypass the hard safety floor for irreversible/visible-side-effect targets")
	return c
}

func newCaptureDiscardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discard <capture-id>",
		Short: "Mark a capture as dropped",
		Args:  cobra.ExactArgs(1),
		RunE:  runCaptureDiscard,
	}
}

func runCaptureApprove(cmd *cobra.Command, args []string) error {
	override, _ := cmd.Flags().GetBool("override")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	resp, err := captureApproveFunc(cmd.Context(), wire.HubCaptureApproveReq{
		CaptureID: args[0],
		Override:  override,
	})
	if err != nil {
		return err
	}
	if wantJSON {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal JSON: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	switch resp.Status {
	case "approved":
		cmd.Printf("✓ %s.%s fired\n", resp.Target.Feature, resp.Target.Action)
		if resp.Outcome.Data != nil {
			data, _ := json.MarshalIndent(resp.Outcome.Data, "  ", "  ")
			cmd.Printf("  %s\n", string(data))
		}
	case "noted":
		cmd.Printf("✓ kept as note (%s)\n", resp.CaptureID)
	case "blocked":
		cmd.Printf("⛔ blocked: %s\n", resp.Outcome.Message)
		cmd.Printf("   pass --override to fire anyway\n")
	case "failed":
		if resp.Outcome.Kind == "unwired" {
			cmd.Printf("⚠ target %s not wired yet (target not yet wired)\n", resp.Outcome.Target)
		} else {
			cmd.Printf("✗ %s.%s failed: [%s] %s\n",
				resp.Target.Feature, resp.Target.Action, resp.Outcome.Code, resp.Outcome.Message)
		}
	}
	return nil
}

func runCaptureDiscard(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	resp, err := captureDiscardFunc(cmd.Context(), wire.HubCaptureDiscardReq{CaptureID: args[0]})
	if err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	cmd.Printf("✓ dropped %s\n", resp.CaptureID)
	return nil
}

func newCaptureClassifyCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "classify <capture-id>",
		Short: "Propose a routing for an existing capture",
		Long: `Asks the hub's capture.classify RPC to pick a tool + payload for the
referenced capture, and persists the proposal back onto the row.

Pass --retry to re-roll with the previous proposal moved into
prior_attempts (the LLM sees them as anti-examples), and optionally
--hint "..." to nudge the next try.`,
		Args: cobra.ExactArgs(1),
		RunE: runCaptureClassify,
	}
	c.Flags().Bool("retry", false, "Move the existing proposal into prior_attempts and ask for a different target")
	c.Flags().String("hint", "", "Free-form hint to nudge the next classification (used with --retry)")
	return c
}

func runCaptureClassify(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	retry, _ := cmd.Flags().GetBool("retry")
	hint, _ := cmd.Flags().GetString("hint")
	resp, err := captureClassifyFunc(cmd.Context(), wire.HubCaptureClassifyReq{
		CaptureID: args[0],
		Retry:     retry,
		Hint:      hint,
	})
	if err != nil {
		return err
	}

	if wantJSON {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal JSON: %w", err)
		}
		// fmt.Fprintln(cmd.OutOrStdout(), …) → real stdout in prod
		// (cmd.Println defaults to stderr in Cobra and would break
		// `--json | jq`), and the test-injected writer in unit tests.
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	cmd.Printf("Capture: %s\n", resp.Text)
	cmd.Println()
	cmd.Printf("→ %s.%s  (confidence %.2f, %dms, %s)\n",
		resp.Target.Feature, resp.Target.Action, resp.Confidence, resp.LatencyMs, resp.Model)
	cmd.Printf("  %s\n", resp.Rationale)
	if len(resp.Payload) > 0 {
		payloadJSON, _ := json.MarshalIndent(resp.Payload, "  ", "  ")
		cmd.Printf("  payload: %s\n", string(payloadJSON))
	}
	if len(resp.Alternatives) > 0 {
		cmd.Println()
		cmd.Println("Alternatives:")
		for _, alt := range resp.Alternatives {
			cmd.Printf("  • %s.%s  (%.2f) — %s\n", alt.Feature, alt.Action, alt.Confidence, alt.Rationale)
		}
	}
	return nil
}

func newCaptureListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent tray captures",
		Args:  cobra.NoArgs,
		RunE:  runCaptureList,
	}
	cmd.Flags().Int("limit", 25, "How many captures to fetch (max 200)")
	return cmd
}

// Auto-fire threshold. Matches the system prompt's 0.95+ band ("I would
// auto-fire this") and the agent-card hint convention. Below this we
// keep the proposal in review.
const autoFireConfidence = 0.95

// Targets that always require explicit override even at confidence 1.0
// — mirrors HARD_SAFETY_TARGETS on the hub. Duplicated here so --auto
// can skip the round-trip to the safety-blocked path. Calendar with
// attendees is flagged separately at dispatch time on the hub.
var hardSafetyTargets = map[string]struct{}{
	"email.send":           {},
	"crm.interaction.log":  {},
}

func runCaptureSubmit(cmd *cobra.Command, args []string) error {
	text, err := captureText(args)
	if err != nil {
		return err
	}
	if text == "" {
		return fmt.Errorf("capture text is required (pass as args or on stdin)")
	}

	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	auto, _ := cmd.Flags().GetBool("auto")

	resp, err := captureCreateFunc(cmd.Context(), wire.HubCaptureCreateReq{
		Text:   text,
		Source: "cli",
	})
	if err != nil {
		return err
	}

	if !auto {
		if wantJSON {
			out, err := json.MarshalIndent(resp, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal JSON: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}
		cmd.Printf("✓ Captured %s (%s)\n", resp.ID, resp.Status)
		return nil
	}

	// --auto path: classify, then approve if safe.
	fmt.Fprintf(os.Stderr, "✓ Captured %s — classifying…\n", resp.ID)
	classify, err := captureClassifyFunc(cmd.Context(), wire.HubCaptureClassifyReq{CaptureID: resp.ID})
	if err != nil {
		return fmt.Errorf("classify: %w", err)
	}

	targetKey := fmt.Sprintf("%s.%s", classify.Target.Feature, classify.Target.Action)
	_, isHardSafety := hardSafetyTargets[targetKey]
	canAutoFire := classify.Confidence >= autoFireConfidence && !isHardSafety

	if !canAutoFire {
		reason := "below 0.95 confidence"
		if isHardSafety {
			reason = "requires explicit override"
		}
		cmd.Printf("→ %s (%.2f) — held for review (%s)\n", targetKey, classify.Confidence, reason)
		cmd.Printf("  %s\n", classify.Rationale)
		cmd.Printf("  approve: mm capture approve %s%s\n", resp.ID,
			func() string {
				if isHardSafety {
					return " --override"
				}
				return ""
			}())
		return nil
	}

	approve, err := captureApproveFunc(cmd.Context(), wire.HubCaptureApproveReq{CaptureID: resp.ID})
	if err != nil {
		return fmt.Errorf("approve: %w", err)
	}

	if wantJSON {
		out, err := json.MarshalIndent(map[string]any{
			"capture":  resp,
			"classify": classify,
			"approve":  approve,
		}, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal JSON: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	switch approve.Status {
	case "approved":
		cmd.Printf("✓ %s fired (%.2f confidence)\n", targetKey, classify.Confidence)
		if approve.Outcome.Data != nil {
			data, _ := json.MarshalIndent(approve.Outcome.Data, "  ", "  ")
			cmd.Printf("  %s\n", string(data))
		}
	case "noted":
		cmd.Printf("✓ kept as note (%s)\n", resp.ID)
	case "failed":
		if approve.Outcome.Kind == "unwired" {
			cmd.Printf("⚠ %s not wired yet — kept as classified proposal\n", approve.Outcome.Target)
		} else {
			cmd.Printf("✗ approve failed: [%s] %s\n", approve.Outcome.Code, approve.Outcome.Message)
		}
	}
	return nil
}

func runCaptureList(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	resp, err := captureListFunc(cmd.Context(), wire.HubCaptureListReq{Limit: limit})
	if err != nil {
		return err
	}

	if wantJSON {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal JSON: %w", err)
		}
		// fmt.Fprintln(cmd.OutOrStdout(), …) → real stdout in prod
		// (cmd.Println defaults to stderr in Cobra and would break
		// `--json | jq`), and the test-injected writer in unit tests.
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	if len(resp.Items) == 0 {
		cmd.Println("No captures yet.")
		return nil
	}

	for _, item := range resp.Items {
		when := formatCaptureTime(item.CreatedAt)
		// Truncate body to one line for the list view; full text via --json
		// or by piping `mm capture list --json | jq`.
		body := strings.ReplaceAll(item.Text, "\n", " ")
		if len(body) > 80 {
			body = body[:77] + "…"
		}
		cmd.Printf("%s  %s  %s  %s\n", item.ID, when, item.Status, body)
	}
	return nil
}

// captureText returns the freeform text either from positional args or
// from stdin when no args are supplied. Tabs/newlines from stdin are
// preserved; positional args are joined with spaces.
func captureText(args []string) (string, error) {
	if len(args) > 0 {
		return strings.TrimSpace(strings.Join(args, " ")), nil
	}
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	// Only consume stdin if it's piped — bare `mm capture` with no args
	// and an interactive TTY should error out cleanly, not block.
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return "", nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func formatCaptureTime(iso string) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("Jan 2 15:04")
}

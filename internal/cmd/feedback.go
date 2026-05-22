package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"mm-cli/internal/http"
	"mm-cli/internal/version"
	"mm-cli/internal/wire"
)

var submitFeedbackFunc = func(ctx context.Context, req wire.HubFeedbackSubmitReq) (wire.HubFeedbackSubmitResp, error) {
	client := http.New()
	var resp wire.HubFeedbackSubmitResp
	err := client.Hub(ctx, "feedback", "submit", req, &resp)
	return resp, err
}

// NewFeedbackCmd builds the `mm feedback` command.
func NewFeedbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feedback [message]",
		Short: "Submit a feedback or friction report",
		Long: `Submit a lightweight bug, friction, or idea report directly from the CLI or agent.
Posts to the Hub at meta-me.uk/api/mm.

Examples:
  mm feedback "the crm log command needs a default instance"
  mm feedback submit "unintuitive error in kb status" --kind bug --app kb`,
		Args: cobra.ArbitraryArgs,
		RunE: runFeedbackDefault,
	}

	cmd.PersistentFlags().String("app", "mm", "App slug the feedback is about (e.g. crm, kb, mm)")
	cmd.PersistentFlags().String("kind", "friction", "Classification: bug, friction, or idea")
	cmd.PersistentFlags().String("context", "", "Optional extra detail (repro steps, error output)")

	cmd.AddCommand(newFeedbackSubmitCmd())
	return cmd
}

func newFeedbackSubmitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "submit [message]",
		Short: "Submit a feedback item",
		Args:  cobra.ArbitraryArgs,
		RunE:  runFeedbackSubmit,
	}
}

func runFeedbackDefault(cmd *cobra.Command, args []string) error {
	if len(args) > 0 && args[0] == "help" {
		return cmd.Help()
	}
	return runFeedbackSubmit(cmd, args)
}

func runFeedbackSubmit(cmd *cobra.Command, args []string) error {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		return fmt.Errorf("feedback message is required. Run 'mm feedback help' for usage.")
	}

	app, _ := cmd.Flags().GetString("app")
	kind, _ := cmd.Flags().GetString("kind")
	contextFlag, _ := cmd.Flags().GetString("context")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	// Validate kind
	kindLower := strings.ToLower(kind)
	if kindLower != "bug" && kindLower != "friction" && kindLower != "idea" {
		return fmt.Errorf("invalid classification kind: %q (must be one of: bug, friction, idea)", kind)
	}

	// Auto-capture source and version
	source := "cli"
	if os.Getenv("MM_AGENT") == "true" || os.Getenv("MM_SOURCE") == "agent" {
		source = "agent"
	}

	req := wire.HubFeedbackSubmitReq{
		Message:   text,
		AppSlug:   app,
		URL:       contextFlag,
		UserAgent: fmt.Sprintf("mm-go %s (source: %s, kind: %s)", version.String(), source, kindLower),
	}

	resp, err := submitFeedbackFunc(cmd.Context(), req)
	if err != nil {
		return err
	}

	if wantJSON {
		// Retain fields for --json parity backwards compatibility in UI testing
		outMap := map[string]string{
			"id":     resp.ID,
			"status": "filed",
			"kind":   kindLower,
			"app":    app,
		}
		out, err := json.MarshalIndent(outMap, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal JSON: %w", err)
		}
		cmd.Println(string(out))
		return nil
	}

	cmd.Printf("✓ Filed feedback %s (%s, app: %s)\n", resp.ID, kindLower, app)
	return nil
}

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/config"
	"mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// Overridable for tests.
var (
	auditListFunc = func(ctx context.Context, req wire.HubAuditListReq) (wire.HubAuditListResp, error) {
		client := http.New()
		var resp wire.HubAuditListResp
		err := client.Hub(ctx, "audit", "list", req, &resp)
		return resp, err
	}
	auditShowFunc = func(ctx context.Context, req wire.HubAuditShowReq) (wire.HubAuditShowResp, error) {
		client := http.New()
		var resp wire.HubAuditShowResp
		err := client.Hub(ctx, "audit", "show", req, &resp)
		return resp, err
	}
	hermesLookPath = exec.LookPath
	// hermesAuthStatusFunc reports a provider's auth status line (e.g. "zai: logged in").
	// Overridable for tests.
	hermesAuthStatusFunc = func(provider string) string {
		out, _ := exec.Command("hermes", "auth", "status", provider).CombinedOutput()
		return string(out)
	}
	hermesRunFunc = func(ctx context.Context, name string, args []string, dir string, env []string, wait bool) error {
		c := exec.Command(name, args...)
		c.Dir = dir
		c.Env = env
		if wait {
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			c.Stdin = os.Stdin
			return c.Run()
		}
		c.SysProcAttr = detachSysProcAttr()
		return c.Start()
	}
)

// modelAliases maps short, memorable names to a canonical provider/model pair,
// reflecting the providers this setup actually has authed. Keeps `mm run --model glm`
// one keystroke instead of `--model zai/glm-5.2` + a separate --provider.
var modelAliases = map[string]string{
	"glm":      "zai/glm-5.2",
	"gemini":   "gemini/gemini-3.7-flash",
	"flash":    "gemini/gemini-3.7-flash",
	"deepseek": "deepseek/deepseek-v4-pro",
	"kimi":     "kimi-for-coding/k3",
	"k3":       "kimi-for-coding/k3",
	"sonnet":   "anthropic/claude-sonnet-5",
	"opus":     "anthropic/claude-opus-5",
}

// defaultRunModel resolves the model when --model is omitted: MM_RUN_MODEL env
// (so the default is one .env line away, no rebuild) then a built-in fallback.
func defaultRunModel() string {
	if m := strings.TrimSpace(os.Getenv("MM_RUN_MODEL")); m != "" {
		return m
	}
	return "gemini/gemini-3.7-flash"
}

// resolveModel turns a --model value into explicit (provider, name) parts for
// `hermes chat`. It expands short aliases, then splits on the first "/". Hermes
// needs --provider set explicitly — the bare "provider/model" string does NOT
// auto-resolve the provider on the chat path (it silently falls back). An empty
// provider means "no /" was given; leave Hermes to auto-detect.
func resolveModel(model string) (provider, name string) {
	if expanded, ok := modelAliases[strings.ToLower(strings.TrimSpace(model))]; ok {
		model = expanded
	}
	if idx := strings.IndexByte(model, '/'); idx >= 0 {
		return model[:idx], model[idx+1:]
	}
	return "", model
}

// NewRunCmd builds the `mm run` command — delegate tasks to Hermes and review results.
func NewRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [spec]",
		Short: "Delegate a task to a background Hermes agent, then review the result",
		Long: `mm run — delegate tasks to Hermes and review results

Subcommands:
  run "<spec>" [options]    Fire a Hermes run in an isolated worktree
  run list [--limit N] [--json]
                             Show recent runs, newest first (default limit 20)
  run show <run-id> [--json]
                             Show full detail for a single run (accepts prefix)

Run options:
  --project <name|path>   Project to work in (resolved from registered projects).
                          Sets the cwd for Hermes — worktree is branched from here.
  --thread <id>           Desk chat thread ID. Hermes injects a completion message
                          when done ("results posted to admin/audit").
  --model <id>            Model: alias (glm|gemini|deepseek|kimi|sonnet|opus), provider/model,
                          or bare model. Default: $MM_RUN_MODEL or gemini/gemini-3.7-flash.
  --max-turns <n>         Max tool-calling iterations (0 = Hermes default of 90). Raise
                          for long one-shot tasks; no need to touch global config.
  --skills <s1,s2>        Extra Hermes skills to preload (meta-me is always loaded)
  --wait                  Run in foreground and stream Hermes output (default: background)
  --dry-run               Print the Hermes command without running it

Examples:
  mm run "refactor error handling in keel's API routes" --project keel --thread abc123
  mm run list
  mm run list --limit 5 --json
  mm run show hermes-bb44

Hermes works in an isolated git worktree, posts a structured report to
https://meta-me.uk/admin/audit, and notifies the desk thread when done.
mm run list and mm run show read those reports from the CLI.`,
		Args: cobra.ArbitraryArgs,
		RunE: runParent,
	}

	// List flags
	cmd.Flags().Int("limit", 25, "How many runs to fetch")
	cmd.Flags().String("mode", "run", "Filter by mode (default: run)")

	// Dispatch flags
	cmd.Flags().StringP("project", "p", "", "Project to work in (resolved from registered projects)")
	cmd.Flags().StringP("thread", "t", "", "Desk chat thread ID")
	cmd.Flags().StringP("model", "m", "", "Model: alias (glm|gemini|deepseek|kimi|sonnet|opus), provider/model, or bare model. Default: $MM_RUN_MODEL or gemini/gemini-3.7-flash")
	cmd.Flags().Int("max-turns", 0, "Max tool-calling iterations for the run (0 = Hermes default of 90)")
	cmd.Flags().StringP("skills", "s", "", "Extra Hermes skills to preload (comma-separated)")
	cmd.Flags().Bool("wait", false, "Run in foreground and stream Hermes output")
	cmd.Flags().Bool("dry-run", false, "Print the Hermes command without running it")

	cmd.AddCommand(newRunListCmd(), newRunShowCmd())
	return cmd
}

func newRunListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List recent Hermes runs",
		Args:    cobra.NoArgs,
		RunE:    runAuditList,
	}
	cmd.Flags().Int("limit", 25, "How many runs to fetch")
	cmd.Flags().String("mode", "run", "Filter by mode (default: run)")
	return cmd
}

func newRunShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <run-id>",
		Short: "Print the full audit report for a run",
		Args:  cobra.ExactArgs(1),
		RunE:  runAuditShow,
	}
}

func runParent(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runAuditList(cmd, args)
	}
	return runDispatch(cmd, args)
}

var resolveProjectRoot = func(ctx context.Context, ref string) (string, error) {
	client := http.New()
	resp, err := client.AgentFetch(ctx, "", "/api/projects", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET /api/projects %d", resp.StatusCode)
	}
	var data wire.AgentProjectsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	abs, _ := filepath.Abs(expandTilde(ref))
	for _, p := range data.Projects {
		if strings.EqualFold(p.Label, ref) || p.RootPath == abs || p.ID == ref {
			return p.RootPath, nil
		}
	}
	return "", fmt.Errorf("project not found: %s", ref)
}

func runDispatch(cmd *cobra.Command, args []string) error {
	// Load ~/.mm/.env into the process env so MM_RUN_MODEL (and friends) resolve.
	config.Load()

	project, _ := cmd.Flags().GetString("project")
	thread, _ := cmd.Flags().GetString("thread")
	model, _ := cmd.Flags().GetString("model")
	maxTurns, _ := cmd.Flags().GetInt("max-turns")
	skillsStr, _ := cmd.Flags().GetString("skills")
	wait, _ := cmd.Flags().GetBool("wait")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if model == "" {
		model = defaultRunModel()
	}
	provider, modelName := resolveModel(model)
	resolvedModel := modelName
	if provider != "" {
		resolvedModel = provider + "/" + modelName
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	if project != "" {
		pRoot, err := resolveProjectRoot(cmd.Context(), project)
		if err != nil {
			return fmt.Errorf("Project %q not found. Run `mm project list` to see registered projects.", project)
		}
		cwd = pRoot
	}

	// Build prompt
	spec := strings.Join(args, " ")
	threadMarker := ""
	if thread != "" {
		threadMarker = fmt.Sprintf("MM_THREAD_ID=%s ", thread)
	}
	prompt := threadMarker + spec

	// Build skills
	var skills []string
	skills = append(skills, "meta-me")
	if skillsStr != "" {
		for _, s := range strings.Split(skillsStr, ",") {
			s = strings.TrimSpace(s)
			if s != "" && s != "meta-me" {
				skills = append(skills, s)
			}
		}
	}
	skillsArg := strings.Join(skills, ",")

	// Assemble Hermes invocation. --model/--provider/--max-turns are `chat`
	// subcommand flags, so they go after `chat`. We pass --model AND --provider
	// explicitly: the chat path ignores HERMES_INFERENCE_MODEL and won't infer
	// the provider from a "provider/model" prefix.
	hermesArgs := []string{
		"--worktree",
		"--yolo",
		"--accept-hooks",
		"--pass-session-id",
		"-s", skillsArg,
		"chat",
		"-q", prompt,
		"--model", modelName,
	}
	if provider != "" {
		hermesArgs = append(hermesArgs, "--provider", provider)
	}
	if maxTurns > 0 {
		hermesArgs = append(hermesArgs, "--max-turns", strconv.Itoa(maxTurns))
	}

	if dryRun {
		var quotedArgs []string
		for _, a := range hermesArgs {
			if strings.Contains(a, " ") {
				quotedArgs = append(quotedArgs, fmt.Sprintf(`"%s"`, a))
			} else {
				quotedArgs = append(quotedArgs, a)
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "HERMES_INFERENCE_MODEL=%s hermes %s\n", resolvedModel, strings.Join(quotedArgs, " "))
		fmt.Fprintf(cmd.OutOrStdout(), "cwd: %s\n", cwd)
		return nil
	}

	_, err = hermesLookPath("hermes")
	if err != nil {
		return fmt.Errorf("hermes not found on PATH. Make sure Hermes Agent is installed")
	}

	// Pre-flight auth guard: refuse to spawn on a provider that isn't logged in,
	// rather than let Hermes silently fall back to another model and waste the run.
	if provider != "" && strings.Contains(strings.ToLower(hermesAuthStatusFunc(provider)), "logged out") {
		return fmt.Errorf("provider %q is not authenticated — run `hermes auth add %s` or pick a --model on an authed provider.\nRefusing to run: Hermes would silently fall back to a different model and burn the run on the wrong one.", provider, provider)
	}

	env := append(os.Environ(), "HERMES_INFERENCE_MODEL="+resolvedModel)

	if wait {
		err = hermesRunFunc(cmd.Context(), "hermes", hermesArgs, cwd, env, true)
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				os.Exit(exitError.ExitCode())
			}
			return fmt.Errorf("failed to run hermes: %w", err)
		}
		return nil
	}

	err = hermesRunFunc(cmd.Context(), "hermes", hermesArgs, cwd, env, false)
	if err != nil {
		return fmt.Errorf("failed to spawn hermes: %w", err)
	}

	modelLabel := resolvedModel
	projectLabel := project
	if projectLabel == "" {
		projectLabel = filepath.Base(cwd)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "▶ Hermes running in background (%s, worktree from %s)\n", modelLabel, projectLabel)
	if thread != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Will notify thread %s on completion.\n", thread)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "  Results → https://meta-me.uk/admin/audit")

	return nil
}

func runAuditList(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	mode, _ := cmd.Flags().GetString("mode")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	req := wire.HubAuditListReq{Limit: limit, Mode: mode}
	resp, err := auditListFunc(cmd.Context(), req)
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

	if len(resp.Runs) == 0 {
		cmd.Println("No runs yet.")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%-6s  %-14s  %-24s  %s\n",
		"WHEN", "RUN", "LOOKBACK", "SUMMARY")
	for _, run := range resp.Runs {
		ago := formatTimeAgo(run.CreatedAt)
		rid := run.RunID
		if len(rid) > 14 {
			rid = rid[:14]
		}
		lb := run.Lookback
		if lb == "" {
			lb = "-"
		}
		if len(lb) > 24 {
			lb = lb[:24]
		}
		summary := firstLine(run.Report, 100)
		fmt.Fprintf(cmd.OutOrStdout(), "%-6s  %-14s  %-24s  %s\n",
			ago, rid, lb, summary)
	}
	return nil
}

func runAuditShow(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	runID := args[0]

	resp, err := auditShowFunc(cmd.Context(), wire.HubAuditShowReq{RunID: runID})
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

	cmd.Printf("Run:    %s\n", resp.RunID)
	cmd.Printf("When:   %s\n", formatTimeAgo(resp.CreatedAt))
	cmd.Printf("Status: %s\n", nonempty(resp.Status))
	cmd.Println()

	if len(resp.Rows) == 0 {
		cmd.Println("(no detail rows)")
		return nil
	}

	for i, row := range resp.Rows {
		if i > 0 {
			cmd.Println(strings.Repeat("─", 60))
		}
		cmd.Printf("App:    %s\n", nonempty(row.AppSlug))
		cmd.Printf("Branch: %s\n", nonempty(row.Lookback))
		if row.FilesChecked > 0 || row.GapsFound > 0 || row.GapsFixed > 0 {
			cmd.Printf("Stats:  %d files checked, %d gaps found, %d fixed",
				row.FilesChecked, row.GapsFound, row.GapsFixed)
			if row.HighCount > 0 || row.MediumCount > 0 || row.LowCount > 0 {
				cmd.Printf(" (H:%d M:%d L:%d)", row.HighCount, row.MediumCount, row.LowCount)
			}
			cmd.Println()
		}
		if row.Report != "" {
			cmd.Println()
			fmt.Fprint(cmd.OutOrStdout(), row.Report)
			if !strings.HasSuffix(row.Report, "\n") {
				cmd.Println()
			}
		}
	}
	return nil
}

// firstLine returns the first line of s, truncated to maxLen chars.
func firstLine(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\n\r"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	if len(s) > maxLen {
		s = s[:maxLen-1] + "…"
	}
	if s == "" {
		return "(no summary)"
	}
	return s
}

// truncOr truncates s to maxLen (appending "…") or returns it as-is.
func truncOr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}

// nonempty returns s if non-empty, otherwise "—".
func nonempty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// formatTimeAgo returns a human-friendly relative time string.
func formatTimeAgo(iso string) string {
	if iso == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		// Fallback: try without nano.
		t2, err2 := time.Parse(time.RFC3339, iso)
		if err2 != nil {
			return iso
		}
		t = t2
	}
	d := time.Since(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		if days < 30 {
			return fmt.Sprintf("%dd ago", days)
		}
		months := days / 30
		if months == 1 {
			return "1mo ago"
		}
		return fmt.Sprintf("%dmo ago", months)
	}
}

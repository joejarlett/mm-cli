// Package cmd holds the Cobra command implementations.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mm-cli/internal/auth"
	"mm-cli/internal/config"
)

// defaultClientName names the machine, not the tool.
//
// The key list is read when deciding which of several rows to revoke, and two
// machines that both ran `mm login` used to produce two rows both called
// "mm CLI" — indistinguishable at exactly the moment you need to tell them
// apart. Same reasoning as the Android client's "Desk · Pixel 8".
func defaultClientName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "mm CLI"
	}
	// Trim the mDNS suffix: "Joes-Mac-mini.local" reads better as "Joes-Mac-mini".
	host = strings.TrimSuffix(host, ".local")
	return "mm CLI · " + host
}

// NewLoginCmd builds `mm login [<name>]`.
func NewLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login [name]",
		Short: "Authenticate via browser",
		Long:  `Starts a device-flow OAuth handshake with auth.meta-me.uk. Opens your default browser, polls for approval, and saves the token at ~/.config/mm/auth.json.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runLogin(cmd.Context(), name)
		},
	}
	return cmd
}

func runLogin(ctx context.Context, name string) error {
	existing, _ := auth.Load()
	if existing != nil {
		fmt.Fprintf(os.Stderr, "Already authenticated as %s (%s)\n", existing.UserName, existing.UserEmail)
		fmt.Fprintln(os.Stderr, "Run `mm logout` first to re-authenticate.")
		os.Exit(1)
	}

	fmt.Println("Starting device authentication...")
	fmt.Println()

	cfg := config.Load()
	client := auth.NewClient(cfg.AuthURL)

	// Resolved before DeviceInit, not after: the consent screen reads this name
	// when it asks you to approve, and Poll only reports it once you already
	// have. One string, so the page and the key list cannot disagree.
	clientName := name
	if clientName == "" {
		clientName = defaultClientName()
	}

	init, err := client.DeviceInit(ctx, clientName)
	if err != nil {
		return err
	}

	fmt.Printf("1. Opening browser: %s\n", init.VerificationURL())
	fmt.Printf("2. Enter code: %s\n", init.UserCode)
	fmt.Println()

	openBrowser(init.VerificationURL())

	pollInterval := time.Duration(init.Interval) * time.Second
	timeout := time.Duration(init.ExpiresIn+10) * time.Second
	deadline := time.Now().Add(timeout)

	// Spinner.
	fmt.Print("Waiting for authorization")
	dots := []string{".", "..", "..."}
	dotIdx := 0

	for time.Now().Before(deadline) {
		token, err := client.Poll(ctx, init.DeviceCode, clientName)
		if err == nil {
			fmt.Println()
			validated, err := client.Validate(ctx, token.AccessToken)
			if err != nil {
				return fmt.Errorf("token validation failed: %w", err)
			}
			state := &auth.State{
				Token:     token.AccessToken,
				Prefix:    token.Key.Prefix,
				UserID:    validated.User.ID,
				UserName:  validated.User.Name,
				UserEmail: validated.User.Email,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}
			if err := auth.Save(state); err != nil {
				return fmt.Errorf("save auth: %w", err)
			}
			fmt.Printf("✅ Authenticated as %s (%s)\n", validated.User.Name, validated.User.Email)
			return nil
		}
		if errors.Is(err, auth.ErrPending) {
			fmt.Printf("\rWaiting for authorization%s   ", dots[dotIdx])
			dotIdx = (dotIdx + 1) % len(dots)
			select {
			case <-time.After(pollInterval):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		if errors.Is(err, auth.ErrExpired) {
			fmt.Println()
			return fmt.Errorf("The device code has expired. Run `mm login` again.")
		}
		fmt.Println()
		return err
	}

	fmt.Println()
	return fmt.Errorf("Timed out waiting for authorization. Run `mm login` again.")
}

// openBrowser opens the default browser at url. Best-effort — failure is fine,
// the user can copy the URL from the printed line.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}

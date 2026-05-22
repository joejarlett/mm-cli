// Command mm-tray — Meta-Me menu-bar tray.
//
// Wave 1: a deliberately tiny shell. One menu item ("Capture…") opens
// https://meta-me.uk/tray in the default browser, where the real Svelte
// surface lives. No webview, no hotkey, no LLM dispatch yet — those are
// Wave 2.
//
// The tray binary stays separate from `mm` (different lifecycle: tray is
// a long-running GUI process; mm is one-shot CLI) but shares the same
// Go module so future waves can reuse internal/config, internal/http,
// and version metadata.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/getlantern/systray"

	"mm-cli/internal/version"
)

// Monochrome template icon derived from @meta-me/ui's MetaMeLogo.svg
// (saturation-channel mask → black silhouette on transparent). macOS
// treats template images as masks and inverts them automatically for
// light/dark menu bars.
//
//go:embed assets/icon@2x.png
var iconBytes []byte

const trayURL = "https://meta-me.uk/capture?from=tray"

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTemplateIcon(iconBytes, iconBytes)
	systray.SetTooltip(fmt.Sprintf("Meta-Me tray %s", version.String()))

	capture := systray.AddMenuItem("Capture…", "Open the Meta-Me tray capture page")
	systray.AddSeparator()
	versionItem := systray.AddMenuItem(fmt.Sprintf("mm-tray %s", version.Version), "")
	versionItem.Disable()
	quit := systray.AddMenuItem("Quit", "Quit mm-tray")

	go func() {
		for {
			select {
			case <-capture.ClickedCh:
				if err := openBrowser(trayURL); err != nil {
					fmt.Fprintf(os.Stderr, "open browser: %v\n", err)
				}
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}


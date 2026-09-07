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
//
// It does know how to replace itself — see selfupdate.go. `mm` cannot do it: a tray is
// installed by copying a bundle out of a release zip, and nothing but the tray can swap that
// bundle cleanly while it is running.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

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

// awaitFlag is passed to the replacement process by restartSelf. Internal, not a user flag.
const awaitFlag = "--await-pid"

func main() {
	// Answered BEFORE anything else, on purpose. installRelease runs this on a downloaded
	// bundle to prove the binary loads and runs before it is allowed to replace a working
	// tray, so it must not depend on the machine's config, the network, or a window server.
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" || a == "version" {
			fmt.Println(version.String())
			return
		}
	}

	// Wait for the tray we are replacing to let go of the menu bar before claiming it.
	for i, a := range os.Args[1:] {
		var pid string
		switch {
		case a == awaitFlag && i+2 <= len(os.Args[1:]):
			pid = os.Args[i+2]
		case strings.HasPrefix(a, awaitFlag+"="):
			pid = strings.TrimPrefix(a, awaitFlag+"=")
		default:
			continue
		}
		if n, err := strconv.Atoi(pid); err == nil {
			awaitPid(n)
		}
		break
	}

	systray.Run(onReady, onExit)
}

// stripAwait drops an awaitFlag left over from a previous restart. Without it the flag
// accumulates a pair per update, and the second restart waits on a pid that died an update
// ago — which resolves instantly, so it would look fine and quietly stop protecting anything.
func stripAwait(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == awaitFlag {
			i++ // and the pid that follows it
			continue
		}
		if strings.HasPrefix(args[i], awaitFlag+"=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func onReady() {
	systray.SetTemplateIcon(iconBytes, iconBytes)
	systray.SetTooltip(fmt.Sprintf("Meta-Me tray %s", version.String()))

	capture := systray.AddMenuItem("Capture…", "Open the Meta-Me tray capture page")
	systray.AddSeparator()

	// Hidden until a check says there is something to update to. A permanently visible
	// "Update Tray…" invites the user to download, verify and restart their way back to the
	// version they are already on — the click costs the same whether or not it does anything,
	// and only one of those outcomes is worth a restarted menu bar.
	updateSelf := systray.AddMenuItem("Update Tray…", "Install the published tray build")
	updateSelf.Hide()

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

	go func() {
		for range updateSelf.ClickedCh {
			updateSelf.SetTitle("Updating tray…")
			updateSelf.Disable()
			ver, err := selfUpdate()
			if err != nil {
				notify("Tray update failed", err.Error())
				updateSelf.SetTitle("Update Tray…")
				updateSelf.Enable()
				continue
			}
			if strings.Contains(ver, "already current") {
				updateSelf.Hide()
				updateSelf.SetTitle("Update Tray…")
				updateSelf.Enable()
				continue
			}
			notify("Tray updated", "Now on "+ver+" — restarting.")
			if err := restartSelf(); err != nil {
				notify("Restart failed", err.Error()+" — quit and start it again.")
				updateSelf.SetTitle("Update Tray…")
				updateSelf.Enable()
			}
		}
	}()

	// Ask the hub what it has published. On launch after a short delay, then every 30
	// minutes: a tray runs for weeks, so a check that only happened at startup would answer
	// for a machine that had long since been rebooted into a different world.
	go func() {
		check := func() {
			hasUpdate, ver, err := checkUpdate(hubBase())
			if err != nil || !hasUpdate {
				return // offline is not news; say nothing rather than nagging
			}
			updateSelf.SetTitle("Update Tray to " + ver + "…")
			updateSelf.SetTooltip("Currently on " + version.Version)
			updateSelf.Show()
		}
		time.Sleep(2 * time.Second)
		check()
		for range time.Tick(30 * time.Minute) {
			check()
		}
	}()
}

func onExit() {}

func notify(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e",
			fmt.Sprintf("display notification %q with title %q", body, title)).Start()
	default:
		_ = exec.Command("notify-send", title, body).Start()
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, body)
}

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

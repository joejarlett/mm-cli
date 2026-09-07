//go:build darwin || linux

package main

// Bringing the tray back on the newly installed bundle.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// restartSelf relaunches the tray after its bundle has been replaced.
//
// It ALWAYS starts a fresh process on darwin and never exec's. The obvious implementation is
// syscall.Exec, on the reasoning that replacing the process image in place brings the icon
// back with no gap and no orphan. It does not: a process that has already stood up
// NSApplication and an NSStatusItem does not get a working one back once its image is
// replaced underneath the window server. You get an icon still drawn in the menu bar, every
// click ignored, and nothing logged anywhere. jarlett-tray shipped that bug and this is the
// fix it arrived at.
//
// What exec bought — same pid, no gap, no orphan — solves a problem this process does not
// have. The tray holds no port, owns no children and nothing refers to its pid. A sub-second
// gap in a menu-bar icon, during an update the user just clicked, costs nothing.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if runtime.GOOS != "darwin" {
		return syscall.Exec(exe, os.Args, os.Environ())
	}

	// Hand the replacement our pid so it waits for us to go before it claims the menu bar.
	args := append([]string{awaitFlag, strconv.Itoa(os.Getpid())}, stripAwait(os.Args[1:])...)

	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		// A bundle is a directory, not a binary: launch it through LaunchServices so its
		// Info.plist is honoured (LSUIElement, the bundle id) rather than running the inner
		// executable as a loose process. `open` returns once the app has been launched, so a
		// non-zero exit here is a real failure and not a race — hence Run, not Start.
		app := exe[:i+len(".app")]
		out, err := exec.Command("open", append([]string{"-n", app, "--args"}, args...)...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("relaunch %s: %s", filepath.Base(app), lastLine(string(out)))
		}
		os.Exit(0)
	}

	cmd := exec.Command(exe, args...)
	// New session, so the replacement outlives this process and the terminal it was started
	// from. Stdin is left at /dev/null: a Setsid'd process reading a terminal it no longer
	// controls takes SIGTTIN, and the tray never reads stdin anyway.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	// Inheriting stdout/stderr rather than piping them is deliberate: a pipe whose read end
	// dies with this process would hand the replacement EPIPE on its first stderr write, and
	// Go turns SIGPIPE on fd 2 into a fatal signal. The tray would then die minutes later
	// with no connection to the update that killed it.
	//
	// So failure is detected by watching the process instead. It is blocked on awaitPid and
	// cannot report success yet, but it can die — and if it does, this process is still here
	// to say so rather than vanishing and leaving nothing.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		return fmt.Errorf("replacement exited immediately: %v", err)
	case <-time.After(750 * time.Millisecond):
	}
	os.Exit(0)
	return nil
}

// awaitPid blocks until pid is gone, so the replacement never claims the menu bar while the
// process it replaces still holds it. The entire failure being avoided is two ideas of one
// app existing at once on macOS, and starting the new tray on top of the old one is another
// helping of that. Bounded, because a tray that never appears is worse than a duplicate icon
// for half a second.
func awaitPid(pid int) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

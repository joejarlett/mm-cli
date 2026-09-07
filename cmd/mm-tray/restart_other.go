//go:build !darwin && !linux

package main

import "fmt"

// No tray bundle is published for this platform, so selfUpdate refuses before anything can
// reach these. They exist so the package still builds.
func restartSelf() error { return fmt.Errorf("restart is not supported on this platform") }
func awaitPid(int)       {}

//go:build !windows

package cmd

import "syscall"

// detachSysProcAttr starts the background Hermes process in its own session
// (Setsid) so it survives the parent `mm run` exiting.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

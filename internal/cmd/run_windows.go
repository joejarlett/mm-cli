//go:build windows

package cmd

import "syscall"

// detachSysProcAttr starts the background Hermes process detached from the
// parent console and in a new process group, the Windows analogue of Setsid,
// so it survives `mm run` exiting.
func detachSysProcAttr() *syscall.SysProcAttr {
	const (
		detachedProcess       = 0x00000008 // DETACHED_PROCESS
		createNewProcessGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
	)
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}

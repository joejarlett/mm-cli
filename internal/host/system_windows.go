//go:build windows

package host

// Snapshot reports host system state. The detailed memory/pressure metrics are
// macOS (vm_stat/sysctl) and Linux (/proc) specific; a Windows implementation
// would read them via the Windows API. Until then it returns a benign default
// so the rest of the CLI works on Windows.
func Snapshot() System {
	return System{PressureLevel: 1, PressureLabel: "unknown"}
}

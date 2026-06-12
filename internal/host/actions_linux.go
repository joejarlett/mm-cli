//go:build linux

package host

import "time"

// serviceActionPlatform drives systemd user units. systemctl restart also
// starts a stopped unit, so both actions map cleanly.
func serviceActionPlatform(label, action string) (string, error) {
	return run(10*time.Second, "systemctl", "--user", action, label+".service")
}

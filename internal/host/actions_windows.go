//go:build windows

package host

import "fmt"

// serviceActionPlatform performs start/stop/restart on a managed service.
// Not yet implemented on Windows (would shell out to `sc`/SCM).
func serviceActionPlatform(label, action string) (string, error) {
	return "", fmt.Errorf("host service actions are not supported on windows yet (%s %s)", action, label)
}

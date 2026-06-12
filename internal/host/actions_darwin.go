//go:build darwin

package host

import (
	"fmt"
	"os"
	"time"
)

// serviceActionPlatform drives launchd. restart = kickstart -k (kill + re-exec
// from disk), start = plain kickstart (runs a cron-type job now).
func serviceActionPlatform(label, action string) (string, error) {
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	args := []string{"kickstart"}
	if action == "restart" {
		args = append(args, "-k")
	}
	args = append(args, target)
	return run(10*time.Second, "launchctl", args...)
}

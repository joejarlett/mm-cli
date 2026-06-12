//go:build darwin

package host

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Services lists tracked launchd jobs (mm-/jj- + cloudflared), ported from
// infra-api.mjs getServices(): merge `launchctl list` state with the plist
// files in ~/Library/LaunchAgents to classify daemon vs cron.
func Services() []Service {
	type active struct {
		pid    *int
		status *int
	}
	activeMap := map[string]active{}

	out, _ := run(5*time.Second, "launchctl", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 0 {
		lines = lines[1:] // skip header
	}
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		label := parts[2]
		if !isTrackedService(label) {
			continue
		}
		var pid, status *int
		if parts[0] != "-" {
			if v, err := strconv.Atoi(parts[0]); err == nil {
				pid = &v
			}
		}
		if parts[1] != "-" {
			if v, err := strconv.Atoi(parts[1]); err == nil {
				status = &v
			}
		}
		activeMap[label] = active{pid: pid, status: status}
	}

	home, _ := os.UserHomeDir()
	agentsDir := home + "/Library/LaunchAgents"
	var services []Service
	seen := map[string]bool{}

	entries, _ := os.ReadDir(agentsDir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".plist") {
			continue
		}
		label := strings.TrimSuffix(e.Name(), ".plist")
		if !isTrackedService(label) {
			continue
		}
		seen[label] = true

		isDaemon := false
		if content, err := os.ReadFile(agentsDir + "/" + e.Name()); err == nil {
			if i := strings.Index(string(content), "<key>KeepAlive</key>"); i >= 0 {
				rest := strings.TrimSpace(string(content)[i+len("<key>KeepAlive</key>"):])
				isDaemon = strings.HasPrefix(rest, "<true/>")
			}
		}
		typ := "cron"
		if isDaemon {
			typ = "daemon"
		}

		a, loaded := activeMap[label]
		services = append(services, Service{
			Label:          label,
			Name:           serviceShortName(label),
			Pid:            a.pid,
			LastExitStatus: a.status,
			Loaded:         loaded,
			Type:           typ,
			Actionable:     !protectedServices[label],
		})
	}

	// Loaded jobs without a plist in ~/Library/LaunchAgents (e.g. system-managed).
	for label, a := range activeMap {
		if seen[label] {
			continue
		}
		services = append(services, Service{
			Label:          label,
			Name:           serviceShortName(label),
			Pid:            a.pid,
			LastExitStatus: a.status,
			Loaded:         true,
			Type:           "unknown",
			Actionable:     !protectedServices[label],
		})
	}
	return services
}

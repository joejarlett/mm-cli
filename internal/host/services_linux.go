//go:build linux

package host

import (
	"strconv"
	"strings"
	"time"
)

// Services lists tracked systemd *user* units (mm-/jj-), the Linux analogue of
// the darwin launchd backend. systemd services are long-running, so Type is
// always "daemon" (there's no launchd-style cron distinction here; timer units
// would surface separately). lastExitStatus comes from ExecMainStatus.
//
// NOTE: unvalidated against jj-server/fedora yet — confirmed when first served.
func Services() []Service {
	out, err := run(5*time.Second, "systemctl", "--user", "list-units",
		"--type=service", "--all", "--no-legend", "--plain", "--no-pager")
	if err != nil {
		return nil
	}
	var services []Service
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		unit := strings.TrimSuffix(fields[0], ".service")
		if !isTrackedService(unit) {
			continue
		}
		active := fields[2] == "active" // ACTIVE column
		var pid, status *int
		svcType := "daemon"
		// MainPID + last exit + unit Type, one show call per tracked unit. A
		// `oneshot` unit is timer-driven and idle between runs (not a down daemon),
		// so map it to "cron" — the UI then shows it as scheduled, not stopped.
		if props, e := run(4*time.Second, "systemctl", "--user", "show", fields[0],
			"-p", "MainPID", "-p", "ExecMainStatus", "-p", "Type", "--no-pager"); e == nil {
			m := map[string]string{}
			for _, l := range strings.Split(strings.TrimSpace(props), "\n") {
				if k, v, ok := strings.Cut(l, "="); ok {
					m[k] = v
				}
			}
			if v, _ := strconv.Atoi(m["MainPID"]); v > 0 {
				pid = &v
			}
			if m["ExecMainStatus"] != "" {
				if v, err := strconv.Atoi(m["ExecMainStatus"]); err == nil {
					status = &v
				}
			}
			if m["Type"] == "oneshot" {
				svcType = "cron"
			}
		}
		services = append(services, Service{
			Label:          unit,
			Name:           serviceShortName(unit),
			Pid:            pid,
			LastExitStatus: status,
			Loaded:         active,
			Type:           svcType,
			Actionable:     !protectedServices[unit],
		})
	}

	// cloudflared on Linux is a SYSTEM unit (not --user), so the loop above misses
	// it — surface it read-only so the dashboard shows the connector here too (it's
	// how this node serves). Acting on a system unit needs root, so display-only.
	if props, e := run(4*time.Second, "systemctl", "show", "cloudflared.service",
		"-p", "ActiveState", "-p", "MainPID", "-p", "LoadState", "--no-pager"); e == nil {
		m := map[string]string{}
		for _, l := range strings.Split(strings.TrimSpace(props), "\n") {
			if k, v, ok := strings.Cut(l, "="); ok {
				m[k] = v
			}
		}
		if m["LoadState"] == "loaded" {
			var pid *int
			if v, _ := strconv.Atoi(m["MainPID"]); v > 0 {
				pid = &v
			}
			services = append(services, Service{
				Label:      "cloudflared",
				Name:       "cloudflared",
				Pid:        pid,
				Loaded:     m["ActiveState"] == "active",
				Type:       "daemon",
				Actionable: false, // system unit — needs root, display-only
			})
		}
	}
	return services
}

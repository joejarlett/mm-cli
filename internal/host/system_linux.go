//go:build linux

package host

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"time"
)

// Snapshot reads Linux memory from /proc/meminfo. Several System fields are
// macOS-specific (compressed/wired pages, jetsam pressure level, OrbStack
// SIGKILLs) and have no Linux equivalent — those are best-effort or zero, noted
// inline. pressureLevel/Label are derived from used-% since Linux has no single
// authoritative pressure scalar like macOS kern.memorystatus_vm_pressure_level.
//
// NOTE: unvalidated against a live box yet — wire-checked when jj-server/fedora
// are first served. The shapes match darwin so the dashboard renders uniformly.
func Snapshot() System {
	meminfo, _ := os.ReadFile("/proc/meminfo")
	kb := func(key string) float64 {
		m := regexp.MustCompile(`(?m)^` + key + `:\s+(\d+) kB`).FindStringSubmatch(string(meminfo))
		if m == nil {
			return 0
		}
		v, _ := strconv.ParseFloat(m[1], 64)
		return v * 1024 // → bytes
	}
	_, dockerErr := run(5*time.Second, dockerBin(), "ps", "-q")

	total := kb("MemTotal")
	available := kb("MemAvailable")
	cached := kb("Cached") + kb("SReclaimable")
	anon := kb("AnonPages")
	wired := kb("Slab") + kb("KernelStack") + kb("PageTables") // kernel-held ≈ "wired"
	swapTotal := kb("SwapTotal")
	swapUsed := swapTotal - kb("SwapFree")

	used := total - available
	level := 1
	label := "normal"
	if total > 0 {
		switch pct := used / total * 100; {
		case pct >= 90:
			level, label = 4, "critical"
		case pct >= 75:
			level, label = 2, "warning"
		}
	}

	gb := func(bytes float64) float64 { return math.Round(bytes/1073741824*100) / 100 }
	return System{
		PressureLevel:      level,
		PressureLabel:      label,
		MemoryTotalGb:      gb(total),
		MemoryAvailableGb:  gb(available),
		MemoryUsedGb:       gb(used),
		MemoryWiredGb:      gb(wired),
		MemoryCompressedGb: 0, // no zswap accounting exposed by default
		MemoryAppGb:        gb(anon),
		MemoryCachedGb:     gb(cached),
		SwapUsedGb:         gb(swapUsed),
		SwapAllocatedGb:    gb(swapTotal),
		RecentSigkills1h:   0, // macOS/OrbStack-specific signal
		BridgeUptimeSec:    int64(time.Since(processStart).Seconds()),
		BridgeRssMb:        selfRssMb(),
		BridgePid:          os.Getpid(),
		DockerOk:           dockerErr == nil,
	}
}

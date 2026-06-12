//go:build darwin

package host

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Snapshot reads macOS memory pressure + footprint, ported from infra-api.mjs
// getSystem(). Absolute tool paths because the launchd PATH omits /usr/sbin.
func Snapshot() System {
	vm, _ := run(3*time.Second, "/usr/bin/vm_stat")
	sw, _ := run(3*time.Second, "/usr/sbin/sysctl", "-n", "vm.swapusage")
	memsize, _ := run(3*time.Second, "/usr/sbin/sysctl", "-n", "hw.memsize")
	pressure, _ := run(3*time.Second, "/usr/sbin/sysctl", "-n", "kern.memorystatus_vm_pressure_level")
	_, dockerErr := run(5*time.Second, dockerBin(), "ps", "-q")

	pageSize := 16384
	if m := regexp.MustCompile(`page size of (\d+) bytes`).FindStringSubmatch(vm); m != nil {
		pageSize, _ = strconv.Atoi(m[1])
	}
	grab := func(label string) int {
		m := regexp.MustCompile(label + `:\s+(\d+)`).FindStringSubmatch(vm)
		if m == nil {
			return 0
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	free := grab("Pages free")
	inactive := grab("Pages inactive")
	speculative := grab("Pages speculative")
	purgeable := grab("Pages purgeable")
	wired := grab("Pages wired down")
	compressed := grab("Pages occupied by compressor")
	fileBacked := grab("File-backed pages")
	anonymous := grab("Anonymous pages")

	totalBytes, _ := strconv.ParseFloat(strings.TrimSpace(memsize), 64)
	availableBytes := float64((free + inactive + speculative + purgeable) * pageSize)

	levelRaw := 1
	if v, err := strconv.Atoi(strings.TrimSpace(pressure)); err == nil {
		levelRaw = v
	}
	label := "normal"
	if levelRaw >= 4 {
		label = "critical"
	} else if levelRaw >= 2 {
		label = "warning"
	}

	swapUsedM := matchFloat(sw, `used = ([\d.]+)M`)
	swapTotalM := matchFloat(sw, `total = ([\d.]+)M`)

	gb := func(bytes float64) float64 { return math.Round(bytes/1073741824*100) / 100 }

	return System{
		PressureLevel:      levelRaw,
		PressureLabel:      label,
		MemoryTotalGb:      gb(totalBytes),
		MemoryAvailableGb:  gb(availableBytes),
		MemoryUsedGb:       gb(totalBytes - availableBytes),
		MemoryWiredGb:      gb(float64(wired * pageSize)),
		MemoryCompressedGb: gb(float64(compressed * pageSize)),
		MemoryAppGb:        gb(float64(anonymous * pageSize)),
		MemoryCachedGb:     gb(float64(fileBacked * pageSize)),
		SwapUsedGb:         math.Round(swapUsedM/1024*100) / 100,
		SwapAllocatedGb:    math.Round(swapTotalM/1024*100) / 100,
		RecentSigkills1h:   countRecentSigkills(),
		BridgeUptimeSec:    int64(time.Since(processStart).Seconds()),
		BridgeRssMb:        selfRssMb(),
		BridgePid:          os.Getpid(),
		DockerOk:           dockerErr == nil,
	}
}

func matchFloat(s, pattern string) float64 {
	m := regexp.MustCompile(pattern).FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	f, _ := strconv.ParseFloat(m[1], 64)
	return f
}

// countRecentSigkills counts OrbStack OOM-kill markers in gui.log in the last
// hour — the early warning that macOS jetsam is killing the Docker VM.
func countRecentSigkills() int {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(home + "/.orbstack/log/gui.log")
	if err != nil {
		return 0
	}
	re := regexp.MustCompile(`(?m)^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\.\d+ OrbStack\[\d+:\d+\] Daemon exited: killed \(SIG`)
	cutoff := time.Now().UTC().Add(-time.Hour)
	count := 0
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		if t, err := time.Parse("2006-01-02T15:04:05", strings.Replace(m[1], " ", "T", 1)); err == nil {
			if t.After(cutoff) {
				count++
			}
		}
	}
	return count
}

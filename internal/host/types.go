// Package host serves this machine's own telemetry — memory pressure, Docker
// containers, and init-system services — over a small token-gated HTTP API.
//
// It is the cross-platform Go successor to infra/scripts/infra-api.mjs (a
// macOS-only bun script). The JSON shapes below are byte-for-byte what the
// home /server dashboard already consumes, so the dashboard needs no changes
// when a node is cut over from the bun agent to `mm host serve`.
//
// Platform specifics live behind build tags: system_darwin.go / system_linux.go
// and services_darwin.go / services_linux.go. Everything else (Docker, the HTTP
// server) is shared.
package host

// System is the host memory/pressure snapshot. Field names + JSON tags match
// infra-api.mjs getSystem() exactly. The "bridge*" fields describe the serving
// process itself (named for the retired pi-bridge lineage the dashboard expects).
type System struct {
	PressureLevel      int     `json:"pressureLevel"` // 1=normal, 2=warning, 4=critical
	PressureLabel      string  `json:"pressureLabel"` // normal | warning | critical
	MemoryTotalGb      float64 `json:"memoryTotalGb"`
	MemoryAvailableGb  float64 `json:"memoryAvailableGb"`
	MemoryUsedGb       float64 `json:"memoryUsedGb"`
	MemoryWiredGb      float64 `json:"memoryWiredGb"`
	MemoryCompressedGb float64 `json:"memoryCompressedGb"`
	MemoryAppGb        float64 `json:"memoryAppGb"`
	MemoryCachedGb     float64 `json:"memoryCachedGb"`
	SwapUsedGb         float64 `json:"swapUsedGb"`
	SwapAllocatedGb    float64 `json:"swapAllocatedGb"`
	RecentSigkills1h   int     `json:"recentSigkills1h"`
	BridgeUptimeSec    int64   `json:"bridgeUptimeSec"`
	BridgeRssMb        int     `json:"bridgeRssMb"`
	BridgePid          int     `json:"bridgePid"`
	DockerOk           bool    `json:"dockerOk"`
}

// Container mirrors infra-api.mjs getContainers(). MemMb/CpuPct are pointers so
// they marshal to JSON null (not 0) when the container isn't running — the
// dashboard distinguishes "0 MB" from "—".
type Container struct {
	Name        string   `json:"name"`
	State       string   `json:"state"`
	Status      string   `json:"status"`
	Healthy     bool     `json:"healthy"`
	Unhealthy   bool     `json:"unhealthy"`
	RunningFor  string   `json:"runningFor"`
	Actionable  bool     `json:"actionable"`
	MemMb       *float64 `json:"memMb"`
	CpuPct      *float64 `json:"cpuPct"`
	ImageSizeMb int      `json:"imageSizeMb"`
}

// Service mirrors infra-api.mjs getServices(). Pid/LastExitStatus are pointers
// for the same null-vs-0 reason. Type is daemon | cron | unknown.
type Service struct {
	Label          string `json:"label"`
	Name           string `json:"name"`
	Pid            *int   `json:"pid"`
	LastExitStatus *int   `json:"lastExitStatus"`
	Loaded         bool   `json:"loaded"`
	Type           string `json:"type"`
	Actionable     bool   `json:"actionable"`
}

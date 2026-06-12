package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// processStart approximates this serving process's start, for bridgeUptimeSec.
var processStart = time.Now()

// PROTECTED_CONTAINERS / PROTECTED_SERVICES mirror infra-api.mjs — the dashboard
// renders these read-only ("protected") so a stray click can't bounce critical
// infrastructure. Kept in sync with the bun agent during the migration.
var protectedContainers = map[string]bool{
	"mm-postgres": true, "mm-nginx": true, "mm-auth": true, "jj-home": true,
}
var protectedServices = map[string]bool{
	"mm-infra-api": true, "mm-local-agent": true, "mm-host": true,
	"com.cloudflare.cloudflared": true,
}

// Service tracking is shared across platforms (launchd labels and systemd unit
// names both use these prefixes). servicesExtra surfaces critical non-prefixed
// services by exact name — chiefly the Cloudflare tunnel on the primary.
var servicesPrefixes = []string{"mm-", "jj-"}
var servicesExtra = map[string]bool{"com.cloudflare.cloudflared": true}

func isTrackedService(label string) bool {
	if servicesExtra[label] {
		return true
	}
	for _, p := range servicesPrefixes {
		if strings.HasPrefix(label, p) {
			return true
		}
	}
	return false
}

func serviceShortName(label string) string {
	if label == "com.cloudflare.cloudflared" {
		return "cloudflared"
	}
	for _, p := range servicesPrefixes {
		if strings.HasPrefix(label, p) {
			return strings.TrimPrefix(label, p)
		}
	}
	return label
}

// dockerBin resolves the docker CLI. The launchd/systemd PATH is minimal, so we
// fall back to the usual install locations (matches infra-api.mjs's DOCKER const).
func dockerBin() string {
	if p, err := exec.LookPath("docker"); err == nil {
		return p
	}
	for _, c := range []string{"/usr/local/bin/docker", "/opt/homebrew/bin/docker", "/usr/bin/docker"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "docker"
}

// run executes a command with a timeout and returns trimmed stdout. A non-zero
// exit returns the error plus whatever stdout/stderr was captured.
func run(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

// selfRssMb reads this process's resident set size via ps (works on darwin + linux).
func selfRssMb() int {
	out, err := run(3*time.Second, "ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid()))
	if err != nil {
		return 0
	}
	kb, _ := strconv.Atoi(strings.TrimSpace(out))
	return kb / 1024
}

// ── Docker (cross-platform: shells out to the docker CLI) ───────────────────

var sizeRe = regexp.MustCompile(`(?i)virtual\s+([\d.]+)\s*([KMGT]?i?B)`)
var memRe = regexp.MustCompile(`^([\d.]+)\s*([KMGT]?i?B)`)

func unitToMb(n float64, unit string) float64 {
	switch strings.ToUpper(unit[:1]) {
	case "G":
		return n * 1024
	case "T":
		return n * 1024 * 1024
	case "K":
		return n / 1024
	default: // M or bare B (B is negligible, treat as M-scale 0)
		return n
	}
}

func parseDockerSize(s string) int {
	m := sizeRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	return int(math.Round(unitToMb(n, m[2]))) // round to match infra-api.mjs .toFixed(0)
}

type liveStat struct {
	memMb  float64
	cpuPct float64
}

// dockerStats returns per-running-container memory (MB) + CPU%.
func dockerStats() map[string]liveStat {
	stats := map[string]liveStat{}
	out, err := run(8*time.Second, dockerBin(), "stats", "--no-stream", "--format", "{{.Name}}|{{.MemUsage}}|{{.CPUPerc}}")
	if err != nil {
		return stats
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		var memMb float64
		if m := memRe.FindStringSubmatch(strings.TrimSpace(parts[1])); m != nil {
			n, _ := strconv.ParseFloat(m[1], 64)
			memMb = unitToMb(n, m[2])
		}
		cpu, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[2]), "%"), 64)
		stats[parts[0]] = liveStat{memMb: float64(int(memMb*10)) / 10, cpuPct: cpu}
	}
	return stats
}

// dockerPS is the subset of `docker ps --format '{{json .}}'` we read.
type dockerPS struct {
	Names      string `json:"Names"`
	State      string `json:"State"`
	Status     string `json:"Status"`
	RunningFor string `json:"RunningFor"`
	Size       string `json:"Size"`
}

// Containers lists all containers with size + live stats. Returns (nil, error)
// when the docker daemon is unreachable, so the caller can 503 like the bun agent.
func Containers() ([]Container, error) {
	out, err := run(6*time.Second, dockerBin(), "ps", "-a", "--size", "--format", "{{json .}}")
	if err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}
	stats := dockerStats()
	var list []Container
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var p dockerPS
		if json.Unmarshal([]byte(line), &p) != nil {
			continue
		}
		c := Container{
			Name:        p.Names,
			State:       p.State,
			Status:      p.Status,
			Healthy:     strings.Contains(p.Status, "(healthy)"),
			Unhealthy:   strings.Contains(p.Status, "(unhealthy)"),
			RunningFor:  p.RunningFor,
			Actionable:  !protectedContainers[p.Names],
			ImageSizeMb: parseDockerSize(p.Size),
		}
		if s, ok := stats[p.Names]; ok {
			mem, cpu := s.memMb, s.cpuPct
			c.MemMb, c.CpuPct = &mem, &cpu
		}
		list = append(list, c)
	}
	return list, nil
}

// ── HTTP server ─────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, code int, v any) {
	body, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(body)
}

// Serve starts the telemetry API on 127.0.0.1:port, gated by the X-API-Token
// header against token. peer, when non-empty, is the ssh host whose own agent
// backs the /peer/* relay routes. wake holds the WoL targets for /wake routes.
// Blocks until ctx is cancelled.
func Serve(ctx context.Context, port int, token, peer string, wake map[string]WakeTarget) error {
	mux := http.NewServeMux()

	guard := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if token == "" || r.Header.Get("X-API-Token") != token {
				respond(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			h(w, r)
		}
	}

	mux.HandleFunc("GET /system", guard(func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, Snapshot())
	}))
	mux.HandleFunc("GET /containers", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := Containers()
		if err != nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "docker unavailable", "detail": err.Error()})
			return
		}
		if list == nil {
			list = []Container{} // marshal as [] not null on empty hosts
		}
		respond(w, http.StatusOK, map[string][]Container{"containers": list})
	}))
	mux.HandleFunc("GET /services", guard(func(w http.ResponseWriter, r *http.Request) {
		list := Services()
		if list == nil {
			list = []Service{} // marshal as [] not null on empty hosts
		}
		respond(w, http.StatusOK, map[string][]Service{"services": list})
	}))

	// Peer relay — /peer/{system,containers,services} mirror the local routes
	// for the other machine. Always 200 with { peer, reachable } so the
	// dashboard renders "unreachable" rather than a hard error.
	mux.HandleFunc("GET /peer/{sub}", guard(func(w http.ResponseWriter, r *http.Request) {
		sub := r.PathValue("sub")
		if sub != "system" && sub != "containers" && sub != "services" {
			respond(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if peer == "" {
			respond(w, http.StatusOK, map[string]any{"peer": "", "reachable": false, "error": "no peer configured"})
			return
		}
		data, err := peerFetch(peer, token, "/"+sub)
		if err != nil {
			respond(w, http.StatusOK, map[string]any{"peer": peer, "reachable": false, "error": err.Error()})
			return
		}
		data["peer"] = peer
		data["reachable"] = true
		respond(w, http.StatusOK, data)
	}))

	// Wake-on-LAN — GET /wake lists targets with awake probes; GET /wake/{name}
	// probes one; POST /wake/{name} broadcasts the magic packet.
	mux.HandleFunc("GET /wake", guard(func(w http.ResponseWriter, r *http.Request) {
		type status struct {
			Name  string `json:"name"`
			Awake bool   `json:"awake"`
		}
		out := []status{}
		for _, t := range wake {
			out = append(out, status{Name: t.Name, Awake: ProbeAwake(t.Probe)})
		}
		respond(w, http.StatusOK, map[string]any{"targets": out})
	}))
	mux.HandleFunc("GET /wake/{name}", guard(func(w http.ResponseWriter, r *http.Request) {
		t, ok := wake[r.PathValue("name")]
		if !ok {
			respond(w, http.StatusNotFound, map[string]string{"error": "unknown wake target"})
			return
		}
		respond(w, http.StatusOK, map[string]any{"name": t.Name, "awake": ProbeAwake(t.Probe)})
	}))
	mux.HandleFunc("POST /wake/{name}", guard(func(w http.ResponseWriter, r *http.Request) {
		t, ok := wake[r.PathValue("name")]
		if !ok {
			respond(w, http.StatusNotFound, map[string]string{"error": "unknown wake target"})
			return
		}
		if err := Wake(t.Mac); err != nil {
			respond(w, http.StatusInternalServerError, ActionResult{Ok: false, Error: err.Error()})
			return
		}
		respond(w, http.StatusOK, map[string]any{"ok": true, "name": t.Name, "mac": t.Mac})
	}))

	// Actions — restart/stop/start a container, restart/start a service.
	// Protected names refuse with 403, matching infra-api.mjs.
	actionCode := func(res ActionResult) int {
		switch {
		case res.Ok:
			return http.StatusOK
		case strings.HasPrefix(res.Error, "protected"):
			return http.StatusForbidden
		case res.Error == "unknown action" || res.Error == "invalid name":
			return http.StatusBadRequest
		default:
			return http.StatusInternalServerError
		}
	}
	mux.HandleFunc("POST /containers/{name}/{action}", guard(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !nameRe.MatchString(name) {
			respond(w, http.StatusBadRequest, ActionResult{Ok: false, Error: "invalid name"})
			return
		}
		res := ContainerAction(name, r.PathValue("action"))
		respond(w, actionCode(res), res)
	}))
	mux.HandleFunc("POST /services/{label}/{action}", guard(func(w http.ResponseWriter, r *http.Request) {
		label := r.PathValue("label")
		if !nameRe.MatchString(label) {
			respond(w, http.StatusBadRequest, ActionResult{Ok: false, Error: "invalid name"})
			return
		}
		res := ServiceAction(label, r.PathValue("action"))
		respond(w, actionCode(res), res)
	}))

	srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: mux}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()
	fmt.Fprintf(os.Stderr, "mm host serve listening on 127.0.0.1:%d\n", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

package host

import (
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// Wake-on-LAN. The agent runs on a machine on the same L2 segment as the
// target (the home /server dashboard's container cannot reliably broadcast
// through the docker bridge — the host agent is the right sender).
//
// Targets come from WAKE_TARGETS: "name=mac@probeAddr,name2=mac2@probe2",
// e.g. "jj-server=ec:8e:b5:78:c9:98@192.168.1.102:22". probeAddr is optional;
// when present, GET /wake/{name} TCP-dials it to report {awake}.

// WakeTarget is one wakeable machine.
type WakeTarget struct {
	Name  string
	Mac   string
	Probe string // host:port to TCP-dial for an "awake" check ("" = no probe)
}

var macRe = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)

// ParseWakeTargets parses the WAKE_TARGETS env format, skipping invalid entries.
func ParseWakeTargets(env string) map[string]WakeTarget {
	targets := map[string]WakeTarget{}
	for _, entry := range strings.Split(env, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, rest, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		mac, probe, _ := strings.Cut(rest, "@")
		if !macRe.MatchString(mac) {
			continue
		}
		targets[name] = WakeTarget{Name: name, Mac: strings.ToLower(mac), Probe: probe}
	}
	return targets
}

// Wake broadcasts a WoL magic packet (6×0xFF + 16×MAC) for mac on UDP 9 and 7,
// to both the limited broadcast and each interface's directed broadcast.
func Wake(mac string) error {
	raw, err := hex.DecodeString(strings.ReplaceAll(strings.ToLower(mac), ":", ""))
	if err != nil || len(raw) != 6 {
		return fmt.Errorf("invalid MAC %q", mac)
	}
	payload := make([]byte, 0, 102)
	payload = append(payload, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	for i := 0; i < 16; i++ {
		payload = append(payload, raw...)
	}

	dests := []string{"255.255.255.255"}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() {
				continue
			}
			// directed broadcast for this subnet (ip | ^mask)
			ip := ipn.IP.To4()
			mask := ipn.Mask
			b := net.IPv4(ip[0]|^mask[0], ip[1]|^mask[1], ip[2]|^mask[2], ip[3]|^mask[3])
			dests = append(dests, b.String())
		}
	}

	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return err
	}
	defer conn.Close()
	sent := 0
	for _, d := range dests {
		for _, port := range []int{9, 7} {
			if addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", d, port)); err == nil {
				if _, err := conn.WriteTo(payload, addr); err == nil {
					sent++
				}
			}
		}
	}
	if sent == 0 {
		return fmt.Errorf("no magic packets sent")
	}
	return nil
}

// ProbeAwake reports whether addr (host:port) accepts a TCP connection.
func ProbeAwake(addr string) bool {
	if addr == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

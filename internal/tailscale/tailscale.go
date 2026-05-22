// Package tailscale locates the `tailscale` binary and reads the
// MagicDNS suffix from `tailscale status --json`. Mirrors src/tailscale.ts.
//
// The macOS App Store build of Tailscale does NOT put its CLI on PATH,
// so we probe a fallback list. Suffix + path are cached per-process.
package tailscale

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var paths = []string{
	"tailscale", // first try PATH
	"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	"/Applications/Tailscale.app/Contents/Resources/bin/tailscale",
	"/usr/local/bin/tailscale",
	"/opt/homebrew/bin/tailscale",
	"/usr/bin/tailscale",
}

var (
	pathOnce   sync.Once
	cachedPath string
	pathErr    error

	suffixOnce   sync.Once
	cachedSuffix string
	suffixErr    error
)

// Path returns the first usable tailscale binary path. Cached per-process.
func Path() (string, error) {
	pathOnce.Do(func() {
		for _, p := range paths {
			if isUsable(p) {
				cachedPath = p
				return
			}
		}
		pathErr = fmt.Errorf("Tailscale CLI not found. Install Tailscale and ensure it is on PATH or in a standard location.")
	})
	return cachedPath, pathErr
}

func isUsable(p string) bool {
	if strings.HasPrefix(p, "/") {
		fi, err := os.Stat(p)
		return err == nil && fi.Mode().IsRegular()
	}
	c := exec.Command(p, "--version")
	if err := c.Start(); err != nil {
		return false
	}
	doneCh := make(chan error, 1)
	go func() { doneCh <- c.Wait() }()
	select {
	case err := <-doneCh:
		return err == nil
	case <-time.After(2 * time.Second):
		_ = c.Process.Kill()
		return false
	}
}

type statusBlob struct {
	MagicDNSSuffix string `json:"MagicDNSSuffix"`
	Self           struct {
		DNSName string `json:"DNSName"`
	} `json:"Self"`
}

// Suffix returns the local tailscaled's current MagicDNS suffix
// (e.g. "tail69dfd7.ts.net"). Cached per-process.
func Suffix() (string, error) {
	suffixOnce.Do(func() {
		bin, err := Path()
		if err != nil {
			suffixErr = err
			return
		}
		c := exec.Command(bin, "status", "--json")
		out, err := c.Output()
		if err != nil {
			suffixErr = fmt.Errorf("tailscale status --json failed: %w", err)
			return
		}
		var s statusBlob
		if err := json.Unmarshal(out, &s); err != nil {
			suffixErr = fmt.Errorf("tailscale status --json: invalid JSON: %w", err)
			return
		}
		suffix := s.MagicDNSSuffix
		if suffix == "" && s.Self.DNSName != "" {
			// Fallback: derive from Self.DNSName ("m4.taildd974e.ts.net." → "taildd974e.ts.net")
			trimmed := strings.TrimSuffix(s.Self.DNSName, ".")
			if dot := strings.Index(trimmed, "."); dot > 0 {
				suffix = trimmed[dot+1:]
			}
		}
		if suffix == "" {
			suffixErr = fmt.Errorf("tailscale status: could not determine MagicDNS suffix")
			return
		}
		cachedSuffix = suffix
	})
	return cachedSuffix, suffixErr
}

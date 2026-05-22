// Package update implements `mm update` — self-replacement of the running
// binary via atomic rename + re-exec. Spec: specs/go-port/05-distribution.md.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"mm-cli/internal/config"
	"mm-cli/internal/version"
)

// Latest fetches `{HubURL}/dist/mm/latest` (a single-line version tag).
func Latest(ctx context.Context) (string, error) {
	cfg := config.Load()
	url := cfg.HubURL + "/dist/mm/latest"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// Platform returns the platform tag for the running binary, e.g.
// "darwin-arm64" or "linux-amd64".
func Platform() (string, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
	default:
		return "", fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "arm64", "amd64":
	default:
		return "", fmt.Errorf("unsupported arch %s", runtime.GOARCH)
	}
	return runtime.GOOS + "-" + runtime.GOARCH, nil
}

// CheckResult is what Check returns: current vs latest and a flag.
type CheckResult struct {
	Current string
	Latest  string
	Newer   bool
}

// Check compares Latest() against version.Version. "Newer" is true only
// when latest doesn't equal current AND current isn't "dev".
func Check(ctx context.Context) (*CheckResult, error) {
	latest, err := Latest(ctx)
	if err != nil {
		return nil, err
	}
	out := &CheckResult{Current: version.Version, Latest: latest}
	out.Newer = latest != version.Version && version.Version != "dev"
	return out, nil
}

// Apply downloads + atomic-renames the binary, then re-execs.
// `versionTag` may be empty to use Latest().
func Apply(ctx context.Context, versionTag string) error {
	cfg := config.Load()
	if versionTag == "" {
		v, err := Latest(ctx)
		if err != nil {
			return err
		}
		versionTag = v
	}
	plat, err := Platform()
	if err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(self)
	if err == nil {
		self = resolved
	}

	binURL := fmt.Sprintf("%s/dist/mm/%s/mm-%s", cfg.HubURL, versionTag, plat)
	sumsURL := fmt.Sprintf("%s/dist/mm/%s/SHA256SUMS", cfg.HubURL, versionTag)

	// Download binary into a temp file next to the existing one (same FS
	// so the final rename is atomic).
	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, ".mm.new-*")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	defer func() {
		if err != nil {
			cleanup()
		}
	}()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, binURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_ = tmp.Close()
		return fmt.Errorf("download %s: HTTP %d", binURL, resp.StatusCode)
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write binary: %w", err)
	}
	_ = tmp.Close()
	gotSum := hex.EncodeToString(hasher.Sum(nil))

	// Verify against SHA256SUMS.
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	sumsResp, err := http.DefaultClient.Do(req2)
	if err != nil {
		return fmt.Errorf("checksums: %w", err)
	}
	defer sumsResp.Body.Close()
	sumsRaw, _ := io.ReadAll(sumsResp.Body)
	wantSum := ""
	wantName := "mm-" + plat
	for _, line := range strings.Split(string(sumsRaw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.HasSuffix(fields[1], wantName) {
			wantSum = fields[0]
			break
		}
	}
	if wantSum == "" {
		return fmt.Errorf("SHA256SUMS missing entry for %s", wantName)
	}
	if wantSum != gotSum {
		return fmt.Errorf("checksum mismatch: want %s, got %s", wantSum, gotSum)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}

	// Atomic rename over self.
	if err := os.Rename(tmpPath, self); err != nil {
		return fmt.Errorf("rename: %w (you may need sudo if the binary is root-owned)", err)
	}

	// Re-exec to print the new version. From here we don't return.
	if err := syscall.Exec(self, []string{"mm", "version"}, os.Environ()); err != nil {
		// If exec fails, fall back to a non-failing notice.
		fmt.Fprintf(os.Stderr, "updated to %s — re-exec failed: %v\n", versionTag, err)
	}
	return nil
}

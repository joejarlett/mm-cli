package main

// Updating the tray on the machine it was installed to.
//
// mm-tray is distributed as a zipped .app inside the same release the `mm` binary ships in,
// and until now nothing could replace it: a user installs the tray by copying a bundle out
// of a zip and from then on runs whatever they copied, forever. `mm` itself has no way to
// help — it is a separate binary with a separate lifecycle, and the one process that can
// swap the tray cleanly is the tray.
//
// Same channel and the same rules the CLI installer already uses (scripts/install.sh):
//
//	<hub>/dist/mm/latest                            the version, written LAST by the publisher
//	<hub>/dist/mm/<version>/SHA256SUMS              every asset in that directory
//	<hub>/dist/mm/<version>/MetaMe-Tray-<plat>.zip  the bundle
//
// Nothing is unpacked before its checksum matches, and nothing replaces the running app
// before the new binary has proved it runs. Ported from jarlett-tray, which learned each of
// those rules the expensive way.

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"mm-cli/internal/version"
)

// Where the published releases live. MM_HUB_URL is the same override scripts/install.sh
// takes, so a release can be rehearsed against a local publisher before it is real.
func hubBase() string {
	if v := os.Getenv("MM_HUB_URL"); v != "" {
		return strings.TrimRight(v, "/") + "/dist/mm"
	}
	return "https://meta-me.uk/dist/mm"
}

// mmRoot is ~/.mm — where install.sh already puts the CLI, and where the installed tray
// version is recorded. Outside the .app on purpose: the app directory is what gets replaced.
func mmRoot() string {
	if v := os.Getenv("MM_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mm")
}

var versionRe = regexp.MustCompile(`^v?[0-9A-Za-z][0-9A-Za-z.+_-]{0,31}$`)

// installedRelease is the release this tray came from.
//
// Unlike jarlett-tray, the binary usually knows this itself: release.sh stamps
// version.Version from `git describe`, so a published tray carries its own release tag. The
// file is written on every successful update and wins when present, because a bundle that
// has been updated in place is more authoritative about what it is than the stamp baked into
// whatever was downloaded. Returns "" for a dev build, which reads as "unknown, offer the
// update" rather than "current".
func installedRelease() string {
	if b, err := os.ReadFile(filepath.Join(mmRoot(), "TRAY_VERSION")); err == nil {
		if v := strings.TrimSpace(string(b)); versionRe.MatchString(v) {
			return v
		}
	}
	v := strings.TrimSpace(version.Version)
	// `git describe --tags --always` answers v0.2.4-3-gabc1234 for a build ahead of the tag,
	// and a bare commit SHA when no tag is reachable at all — which is what a fresh clone
	// with no tags fetched actually produces. Neither is a release, and neither may compare
	// equal to one. The SHA case is recognised by Version and Commit being the same string,
	// which is the only thing that distinguishes it from a legitimate tag.
	if v == "" || v == "dev" || strings.Contains(v, "-g") || v == strings.TrimSpace(version.Commit) {
		return ""
	}
	if !versionRe.MatchString(v) {
		return ""
	}
	return v
}

func httpGet(url string, timeout time.Duration, limit int64) ([]byte, error) {
	c := &http.Client{Timeout: timeout}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// appDir is the .app bundle the running executable lives in, or "" for a bare binary.
func appDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return ""
	}
	// …/MetaMe Tray.app/Contents/MacOS/mm-tray
	dir := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if strings.HasSuffix(dir, ".app") {
		return dir
	}
	return ""
}

// sumFor finds this machine's tray asset in SHA256SUMS and returns its name and digest. The
// publisher names assets and this reads them: deriving the filename from a convention here
// would be a second place for that convention to live, and the two would disagree the first
// time an architecture was added.
//
// Matched on the tray prefix as well as the platform, because SHA256SUMS covers the whole
// release — the `mm` binaries carry the same -darwin-arm64 suffix and would otherwise match.
func sumFor(sums, platform, arch string) (string, string, error) {
	want := fmt.Sprintf("-%s-%s.", platform, arch)
	var name, sum string
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || len(f[0]) != 64 {
			continue
		}
		n := filepath.Base(strings.TrimPrefix(f[1], "*"))
		if !strings.HasPrefix(n, "MetaMe-Tray-") || !strings.Contains(n, want) {
			continue
		}
		if name != "" {
			return "", "", fmt.Errorf("two tray assets match %s-%s", platform, arch)
		}
		name, sum = n, f[0]
	}
	if name == "" {
		return "", "", fmt.Errorf("no tray build for %s-%s in this release", platform, arch)
	}
	return name, sum, nil
}

// unzipInto extracts src under dst, refusing any member that would escape it. A zip is an
// untrusted archive even when its checksum matches the one we were told to expect.
func unzipInto(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		target := filepath.Join(dst, f.Name) //nolint:gosec // checked immediately below
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator),
			filepath.Clean(dst)+string(os.PathSeparator)) {
			return fmt.Errorf("archive member escapes the extraction directory: %s", f.Name)
		}
		mode := f.Mode()
		switch {
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			rc, err := f.Open()
			if err != nil {
				return err
			}
			link, err := io.ReadAll(io.LimitReader(rc, 4096))
			rc.Close()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// A symlink out of the bundle is the same escape as a path out of it.
			if filepath.IsAbs(string(link)) || strings.HasPrefix(string(link), "..") {
				return fmt.Errorf("archive symlink escapes: %s -> %s", f.Name, link)
			}
			if err := os.Symlink(string(link), target); err != nil {
				return err
			}
		default:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
			if err != nil {
				rc.Close()
				return err
			}
			_, err = io.Copy(out, io.LimitReader(rc, 512<<20))
			rc.Close()
			out.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// checkUpdate reports whether the hub has a newer release that this machine can actually
// install. Used to decide whether to OFFER an update at all — a menu item inviting a restart
// onto the version already running is worse than no menu item, and one that cannot succeed
// at all is worse again.
//
// Deliverability is checked, not assumed. release.sh builds the tray on macOS only and only
// for the HOST arch, so a release cut on an arm64 Mac contains no amd64 bundle — and every
// release published before the tray was added to SHA256SUMS contains no verifiable bundle at
// all. Offering those means a user clicking Update every thirty minutes and being told no.
// One extra request per check, against a file the update would fetch a moment later anyway.
func checkUpdate(base string) (bool, string, error) {
	latestRaw, err := httpGet(base+"/latest", 10*time.Second, 64)
	if err != nil {
		return false, "", fmt.Errorf("asking for the latest release: %w", err)
	}
	latest := strings.TrimSpace(string(latestRaw))
	if !versionRe.MatchString(latest) {
		return false, "", fmt.Errorf("the hub answered something that is not a version")
	}
	if cur := installedRelease(); cur != "" && cur == latest {
		return false, latest, nil
	}
	sums, err := httpGet(fmt.Sprintf("%s/%s/SHA256SUMS", base, latest), 10*time.Second, 1<<20)
	if err != nil {
		return false, "", fmt.Errorf("fetching checksums: %w", err)
	}
	if _, _, err := sumFor(string(sums), "darwin", runtime.GOARCH); err != nil {
		return false, latest, nil // published, but not for this machine — say nothing
	}
	return true, latest, nil
}

// selfUpdate replaces this tray with the published build, returning the version it moved to.
func selfUpdate() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("tray builds are published for macOS only")
	}
	app := appDir()
	if app == "" {
		return "", fmt.Errorf("not running from an installed app bundle — nothing to replace")
	}
	return installRelease(app, hubBase())
}

// installRelease fetches the published bundle from base and swaps app for the copy inside
// it. Separate from selfUpdate so the whole path — resolve, verify, unpack, preflight, swap
// — can be exercised against a local publisher and a throwaway bundle, which is the only
// honest way to test a function whose job is replacing itself.
func installRelease(app, base string) (string, error) {
	latestRaw, err := httpGet(base+"/latest", 15*time.Second, 64)
	if err != nil {
		return "", fmt.Errorf("asking for the latest release: %w", err)
	}
	latest := strings.TrimSpace(string(latestRaw))
	if !versionRe.MatchString(latest) {
		return "", fmt.Errorf("the hub answered something that is not a version")
	}
	if cur := installedRelease(); cur == latest {
		return latest + " (already current)", nil
	}

	sums, err := httpGet(fmt.Sprintf("%s/%s/SHA256SUMS", base, latest), 15*time.Second, 1<<20)
	if err != nil {
		return "", fmt.Errorf("fetching checksums: %w", err)
	}
	name, want, err := sumFor(string(sums), "darwin", runtime.GOARCH)
	if err != nil {
		return "", err
	}
	blob, err := httpGet(fmt.Sprintf("%s/%s/%s", base, latest, name), 10*time.Minute, 512<<20)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", name, err)
	}
	if got := hex.EncodeToString(hashOf(blob)); got != want {
		return "", fmt.Errorf("checksum mismatch on %s — refusing to install it", name)
	}

	tmp, err := os.MkdirTemp("", "mm-tray-update-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	zipPath := filepath.Join(tmp, name)
	if err := os.WriteFile(zipPath, blob, 0o600); err != nil {
		return "", err
	}
	if err := unzipInto(zipPath, tmp); err != nil {
		return "", err
	}

	// release.sh zips the bundle from the root of dist-go, so it lands at the top of the
	// archive under its own name.
	staged := filepath.Join(tmp, filepath.Base(app))
	if _, err := os.Stat(staged); err != nil {
		return "", fmt.Errorf("%s is not in this release", filepath.Base(app))
	}

	// Preflight while there is still a working tray to complain with — past the swap, a bad
	// bundle leaves an empty menu bar with nothing to click and no message.
	//
	// BOUNDED, because "does not answer" is a real outcome and not a rare one: a binary from
	// before --version existed ignores the flag and starts a tray instead, so an unbounded
	// wait here would hang the update forever and leave a second icon in the menu bar. Every
	// mm-tray released so far is such a binary, so this is the expected case on the first
	// update, not a hypothetical one.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, filepath.Join(staged, "Contents", "MacOS", "mm-tray"), "--version")
	probe.Stdin = nil
	// Killing the process is not enough to unblock CombinedOutput: anything it spawned still
	// holds the inherited pipe, so the read waits for THAT to exit. WaitDelay closes the
	// pipes shortly after the context is done.
	probe.WaitDelay = 2 * time.Second
	out, err := probe.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("the downloaded tray did not answer --version — refusing to install it")
	}
	if err != nil {
		return "", fmt.Errorf("the downloaded tray does not run: %s", lastLine(string(out)))
	}

	// Swap by rename, never by writing over the running bundle: replacing a binary in place
	// leaves the kernel holding a stale signature for it, and every later launch is killed
	// with no message at all. A rename gives the new app a new inode and leaves this process
	// on the one it started from.
	old := app + ".old"
	os.RemoveAll(old)
	if err := os.Rename(app, old); err != nil {
		return "", fmt.Errorf("moving the old app aside: %w", err)
	}
	if err := os.Rename(staged, app); err != nil {
		os.Rename(old, app) // put it back rather than leaving the machine with no tray
		return "", fmt.Errorf("installing the new app: %w", err)
	}
	os.RemoveAll(old)

	if err := os.MkdirAll(mmRoot(), 0o755); err == nil {
		os.WriteFile(filepath.Join(mmRoot(), "TRAY_VERSION"), []byte(latest+"\n"), 0o644)
	}
	return latest, nil
}

func hashOf(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return s
	}
	return lines[len(lines)-1]
}

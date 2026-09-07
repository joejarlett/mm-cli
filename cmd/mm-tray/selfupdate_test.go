package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mm-cli/internal/version"
)

// The release carries five mm binaries and one tray zip, and every darwin/arm64 asset shares
// the suffix the platform match keys on. Picking the tray out of that is the whole job.
func TestSumForPicksTheTrayAsset(t *testing.T) {
	sums := strings.Join([]string{
		strings.Repeat("a", 64) + "  mm-darwin-arm64",
		strings.Repeat("b", 64) + "  mm-darwin-amd64",
		strings.Repeat("c", 64) + "  mm-linux-arm64",
		strings.Repeat("d", 64) + "  MetaMe-Tray-darwin-arm64.zip",
	}, "\n")

	name, sum, err := sumFor(sums, "darwin", "arm64")
	if err != nil {
		t.Fatalf("sumFor: %v", err)
	}
	if name != "MetaMe-Tray-darwin-arm64.zip" {
		t.Errorf("picked %q, want the tray zip", name)
	}
	if sum != strings.Repeat("d", 64) {
		t.Errorf("digest %q is not the tray's", sum)
	}

	// A release built on an arm64 Mac publishes no amd64 tray. Saying so beats installing
	// the wrong one.
	if _, _, err := sumFor(sums, "darwin", "amd64"); err == nil {
		t.Error("expected an error when no tray matches the arch")
	}
}

func TestStripAwait(t *testing.T) {
	for _, tc := range []struct{ in, want []string }{
		{[]string{"--await-pid", "12345"}, []string{}},
		{[]string{"--await-pid", "12345", "--debug"}, []string{"--debug"}},
		{[]string{"--debug", "--await-pid=12345"}, []string{"--debug"}},
		{[]string{"--debug"}, []string{"--debug"}},
		{[]string{"--await-pid"}, []string{}},
	} {
		got := stripAwait(tc.in)
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("stripAwait(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// A build ahead of its tag is not a release, and must not compare equal to one — otherwise a
// dev build reports itself current and never offers the update it most likely needs.
func TestInstalledReleaseIgnoresDescribeSuffix(t *testing.T) {
	t.Setenv("MM_DIR", t.TempDir()) // no TRAY_VERSION file, so the stamp is the only source
	for in, want := range map[string]string{
		"v0.2.4":            "v0.2.4",
		"v0.2.4-3-gabc1234": "",
		"dev":               "",
		"":                  "",
		// `git describe --tags --always` with no tags reachable: Version IS the SHA.
		"f4b849c": "",
	} {
		orig, origCommit := version.Version, version.Commit
		version.Version, version.Commit = in, "f4b849c"
		got := installedRelease()
		version.Version, version.Commit = orig, origCommit
		if got != want {
			t.Errorf("installedRelease() with stamp %q = %q, want %q", in, got, want)
		}
	}
}

// The TRAY_VERSION file wins over the stamp: a bundle updated in place is more authoritative
// about what it is than whatever was baked into the binary that was downloaded.
func TestInstalledReleasePrefersTheFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MM_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "TRAY_VERSION"), []byte("v9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := version.Version
	version.Version = "v0.2.4"
	defer func() { version.Version = orig }()
	if got := installedRelease(); got != "v9.9.9" {
		t.Errorf("installedRelease() = %q, want v9.9.9 from the file", got)
	}
}

// End to end against a local publisher: resolve, verify, unpack, preflight, swap. The only
// honest way to test a function whose job is replacing itself is to give it a throwaway
// bundle and check the throwaway moved.
func TestInstallReleaseSwapsTheBundle(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("uses a shell script as the bundle executable")
	}
	root := t.TempDir()
	t.Setenv("MM_DIR", filepath.Join(root, "mmhome"))

	// The bundle that will be published, with an executable that answers --version.
	staged := filepath.Join(root, "staging", "MetaMe Tray.app")
	mustWriteExec(t, filepath.Join(staged, "Contents", "MacOS", "mm-tray"),
		"#!/bin/sh\necho 'mm v9.9.9'\n")
	zipPath := filepath.Join(root, "MetaMe-Tray-darwin-"+runtime.GOARCH+".zip")
	zipDir(t, filepath.Join(root, "staging"), zipPath)

	blob, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(blob)
	asset := filepath.Base(zipPath)
	sums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), asset)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintln(w, "v9.9.9")
		case "/v9.9.9/SHA256SUMS":
			fmt.Fprint(w, sums)
		case "/v9.9.9/" + asset:
			w.Write(blob)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// The "installed" bundle, marked so the swap is provable.
	app := filepath.Join(root, "Applications", "MetaMe Tray.app")
	mustWriteExec(t, filepath.Join(app, "Contents", "MacOS", "mm-tray"), "#!/bin/sh\nexit 1\n")

	got, err := installRelease(app, srv.URL)
	if err != nil {
		t.Fatalf("installRelease: %v", err)
	}
	if got != "v9.9.9" {
		t.Errorf("installRelease returned %q, want v9.9.9", got)
	}

	// The swapped-in executable is the published one, not the old failing stub.
	b, err := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "mm-tray"))
	if err != nil {
		t.Fatalf("reading the installed executable: %v", err)
	}
	if !strings.Contains(string(b), "v9.9.9") {
		t.Error("the old bundle is still in place — the swap did not happen")
	}
	if _, err := os.Stat(app + ".old"); !os.IsNotExist(err) {
		t.Error("the .old bundle was left behind")
	}
	v, err := os.ReadFile(filepath.Join(root, "mmhome", "TRAY_VERSION"))
	if err != nil || strings.TrimSpace(string(v)) != "v9.9.9" {
		t.Errorf("TRAY_VERSION = %q (err %v), want v9.9.9", v, err)
	}
}

// A bundle whose checksum does not match the one the publisher advertised must not be
// unpacked, and the running tray must survive saying so.
func TestInstallReleaseRefusesABadChecksum(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MM_DIR", filepath.Join(root, "mmhome"))
	asset := "MetaMe-Tray-darwin-" + runtime.GOARCH + ".zip"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintln(w, "v9.9.9")
		case "/v9.9.9/SHA256SUMS":
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), asset)
		case "/v9.9.9/" + asset:
			w.Write([]byte("not the bundle you were promised"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := filepath.Join(root, "Applications", "MetaMe Tray.app")
	mustWriteExec(t, filepath.Join(app, "Contents", "MacOS", "mm-tray"), "#!/bin/sh\nexit 0\n")

	_, err := installRelease(app, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected a checksum mismatch, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(app, "Contents", "MacOS", "mm-tray")); err != nil {
		t.Error("the running bundle was disturbed by a failed update")
	}
}

func mustWriteExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func zipDir(t *testing.T, dir, out string) {
	t.Helper()
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	defer w.Close()
	err = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		h.Name, h.Method = rel, zip.Deflate
		hw, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = hw.Write(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A release that carries no bundle for this machine must not be offered. Every release
// published before the tray entered SHA256SUMS is one, and so is every release cut on a Mac
// of the other architecture.
func TestCheckUpdateIgnoresAReleaseWithNoTrayForThisMachine(t *testing.T) {
	t.Setenv("MM_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintln(w, "v0.2.4")
		case "/v0.2.4/SHA256SUMS":
			// Exactly what the hub serves for v0.2.4: the mm binaries, no tray.
			fmt.Fprintf(w, "%s  mm-darwin-%s\n", strings.Repeat("a", 64), runtime.GOARCH)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	has, ver, err := checkUpdate(srv.URL)
	if err != nil {
		t.Fatalf("checkUpdate: %v", err)
	}
	if has {
		t.Error("offered an update this machine cannot install")
	}
	if ver != "v0.2.4" {
		t.Errorf("version = %q, want v0.2.4", ver)
	}
}

// The happy path: a newer release that does carry a bundle for this machine.
func TestCheckUpdateOffersADeliverableRelease(t *testing.T) {
	t.Setenv("MM_DIR", t.TempDir())
	asset := "MetaMe-Tray-darwin-" + runtime.GOARCH + ".zip"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintln(w, "v9.9.9")
		case "/v9.9.9/SHA256SUMS":
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("a", 64), asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	has, ver, err := checkUpdate(srv.URL)
	if err != nil || !has || ver != "v9.9.9" {
		t.Fatalf("checkUpdate = (%v, %q, %v), want (true, v9.9.9, nil)", has, ver, err)
	}
}

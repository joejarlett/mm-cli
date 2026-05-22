# mm-cli — distribution + self-update

> How the Go binary gets built, hosted, downloaded, installed, and updates itself.

---

## 1. Build matrix

Single `go build` invocation per platform. No CGo (avoids cross-compile pain).

```sh
GOOS=darwin  GOARCH=arm64  CGO_ENABLED=0  go build -trimpath -ldflags="$LDFLAGS" -o dist/mm-darwin-arm64  ./cmd/mm
GOOS=darwin  GOARCH=amd64  CGO_ENABLED=0  go build -trimpath -ldflags="$LDFLAGS" -o dist/mm-darwin-amd64  ./cmd/mm
GOOS=linux   GOARCH=arm64  CGO_ENABLED=0  go build -trimpath -ldflags="$LDFLAGS" -o dist/mm-linux-arm64   ./cmd/mm
GOOS=linux   GOARCH=amd64  CGO_ENABLED=0  go build -trimpath -ldflags="$LDFLAGS" -o dist/mm-linux-amd64   ./cmd/mm
```

`LDFLAGS = "-s -w -X main.Version=$VERSION -X main.Commit=$COMMIT -X main.BuildDate=$DATE"`. `-s -w` strips symbol + DWARF tables → ~30% smaller binary. Trimpath strips build-host paths.

Expected sizes: ~10-15 MB per binary (smaller than `bun --compile`'s ~104 MB or even node-bundle + node runtime).

### Critical: GOAMD64

Default `GOAMD64=v1` works on any x86_64 since 2003. Means Pippa's 2008 Core 2 Duo MacBook Air can run the Linux build — the SSE4.2 ceiling that broke Bun is gone. Do **not** bump to `GOAMD64=v3` (Haswell+) — we get nothing for it and lose those old machines.

### macOS code signing

Reuse the existing `MetaMe Local Agent` self-signed cert from `meta-me-local-agent/scripts/`. The `dist/mm-darwin-*` binaries get re-signed with identifier `uk.meta-me.cli` so they have a stable signature across rebuilds (relevant if/when `mm` gets called from non-Terminal contexts like a hotkey wrapper).

Pattern: lift `meta-me-local-agent/scripts/sign-darwin.sh` into `mm-cli/scripts/sign-darwin.sh`, set `MM_SIGN_IDENTIFIER=uk.meta-me.cli` (same `MetaMe Local Agent` identity).

Linux: nothing to sign.

---

## 2. Hosting

Static files on `chat.meta-me.uk` under `/dist/mm/`:

```
/dist/mm/latest                              ← text file, contents = "v0.1.0\n"
/dist/mm/v0.1.0/mm-darwin-arm64              ← binary
/dist/mm/v0.1.0/mm-darwin-arm64.sha256       ← checksum
/dist/mm/v0.1.0/mm-darwin-amd64
/dist/mm/v0.1.0/mm-darwin-amd64.sha256
/dist/mm/v0.1.0/mm-linux-arm64
/dist/mm/v0.1.0/mm-linux-arm64.sha256
/dist/mm/v0.1.0/mm-linux-amd64
/dist/mm/v0.1.0/mm-linux-amd64.sha256
/dist/mm/v0.1.0/SHA256SUMS                   ← all checksums together (for verification)
```

Served by chat.meta-me.uk's existing SvelteKit `static/` dir. No CDN, no signed URLs — these are public binaries.

Optional later: GPG-sign `SHA256SUMS` so the installer can verify the publisher's key as well as the file integrity. Not for v1.

---

## 3. Install script

`/install` route on chat.meta-me.uk serves a shell script. One command on a fresh machine:

```sh
curl -fsSL https://chat.meta-me.uk/install | bash
```

The script:

1. Detects `uname -s` + `uname -m` → maps to `darwin-arm64` / `darwin-amd64` / `linux-arm64` / `linux-amd64`. Errors out on unsupported.
2. `curl https://chat.meta-me.uk/dist/mm/latest` → resolve version tag.
3. `curl https://chat.meta-me.uk/dist/mm/$VER/mm-$PLATFORM` → download to `~/.mm/bin/mm.new`.
4. `curl https://chat.meta-me.uk/dist/mm/$VER/SHA256SUMS` → verify checksum (`shasum -a 256 -c -` filtered to the platform line).
5. `chmod +x ~/.mm/bin/mm.new && mv ~/.mm/bin/mm.new ~/.mm/bin/mm` — atomic move.
6. **Both symlinks** (audit §10):
   - `ln -sf ~/.mm/bin/mm ~/.local/bin/mm`  (for user shells)
   - `ln -sf ~/.mm/bin/mm ~/.mm/pi-agent/bin/mm`  (for the local agent's bash tool)
7. Print `mm installed at ~/.mm/bin/mm vX.Y.Z. Run \`mm login\` next.`

The script is one of the artifacts in the release pipeline, served as static content at `chat.meta-me.uk/install`.

`#!/bin/bash` shebang (not `sh`) — same lesson as the local-agent install: on Linux Mint and other distros `/bin/sh` is dash and doesn't grok `pipefail`. Document on the install page: `curl ... | bash`, not `| sh`.

---

## 4. Self-update protocol

`mm update [--check] [--version X.Y.Z]`:

### Check (`--check`)

```
GET https://chat.meta-me.uk/dist/mm/latest
```

Compare to embedded `main.Version`. Print one of:

- `mm is up to date (vX.Y.Z)`
- `mm update available: vX.Y.Z → vA.B.C  (run: mm update)`

Exit 0 either way.

### Apply (no flag, or `--version X.Y.Z`)

1. Resolve `$VER` (from `latest` or `--version`).
2. Resolve current platform tag.
3. `GET /dist/mm/$VER/mm-$PLATFORM` → write to `$tmpdir/mm.new` (next to the existing binary on the same filesystem — same disk = atomic rename works).
4. `GET /dist/mm/$VER/SHA256SUMS` → verify `mm.new` checksum.
5. `chmod 0755 $tmpdir/mm.new`.
6. `os.Rename(tmpdir/mm.new, self-path)` — atomic on POSIX.
7. `syscall.Exec(self-path, []string{"mm", "version"}, os.Environ())` — re-exec to print the new version. The user sees the upgraded binary running before the shell prompt returns.

`self-path` from `os.Executable()`. If it's a symlink, follow it with `filepath.EvalSymlinks` so we replace the real file, not the link.

### Errors

- Network failure → print, exit 1. Old binary unchanged.
- Checksum mismatch → unlink `mm.new`, print, exit 1.
- Permission denied on rename (e.g. binary owned by root, user is non-root) → print "permission denied — re-run with sudo or reinstall via curl |bash", exit 1.

### Cadence

mm-cli does **not** auto-check on every invocation. That's annoying and slows command startup. Two opt-in surfaces:

1. **Explicit `mm update [--check]`** — user-initiated.
2. **`mm whoami` and `mm status`** — opportunistically check `latest` once per 7 days (cached at `~/.config/mm/last-update-check`), append a one-line nag to stderr if outdated. Configurable off via `MM_UPDATE_CHECK=0`.

Anything more aggressive crosses the "respectful CLI" line.

---

## 5. Release pipeline

A `scripts/release.sh` in the repo:

```sh
#!/bin/bash
set -euo pipefail
VERSION="${1:?Usage: $0 vX.Y.Z}"
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -X main.Version=$VERSION -X main.Commit=$COMMIT -X main.BuildDate=$DATE"

rm -rf dist && mkdir -p dist

for OS in darwin linux; do
  for ARCH in arm64 amd64; do
    echo "build $OS/$ARCH"
    GOOS=$OS GOARCH=$ARCH CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" \
      -o "dist/mm-$OS-$ARCH" ./cmd/mm
    shasum -a 256 "dist/mm-$OS-$ARCH" | awk '{print $1}' > "dist/mm-$OS-$ARCH.sha256"
  done
done

# Sign macOS binaries
for arch in arm64 amd64; do
  ./scripts/sign-darwin.sh "dist/mm-darwin-$arch"
done

# Single SHA256SUMS file
cd dist && shasum -a 256 mm-* > SHA256SUMS && cd ..

# Stage for the hub deploy
DEST="$HOME/Documents/dev/meta-me.uk/static/dist/mm/$VERSION"
mkdir -p "$DEST"
cp dist/mm-* dist/SHA256SUMS "$DEST/"
echo "$VERSION" > "$HOME/Documents/dev/meta-me.uk/static/dist/mm/latest"
echo "staged at $DEST"
echo "next: commit + push the static dir in meta-me.uk, then docker compose build hub-frontend && up -d hub-frontend"
```

Tag the repo (`git tag vX.Y.Z && git push --tags`), run the script, deploy meta-me.uk. Done.

No GitHub Releases for now (the repo is private; Joe is the only consumer of the artifacts). Switch to GitHub Releases when distribution opens up beyond personal use.

---

## 6. Migration from TS mm

The TS `mm` (bun-compiled or node-bundled) and the Go `mm` can coexist during the cutover. Sequence:

1. **Land Go v0.1.0 alongside TS.** Install the Go binary at `~/.mm/bin/mm`. Move existing TS binary to `~/.mm/bin/mm-ts` as the rollback.
2. **Symlinks point to Go.** `~/.local/bin/mm → ~/.mm/bin/mm` and `~/.mm/pi-agent/bin/mm → ~/.mm/bin/mm`.
3. **Verify** — every command tested in `00-audit.md`. Output diffs (TS vs Go) caught here.
4. **Rollback path:** `ln -sf ~/.mm/bin/mm-ts ~/.mm/bin/mm` brings TS back instantly. No reinstall, no auth re-do (the `~/.config/mm/auth.json` shape is identical).
5. **After two weeks of clean Go runs:** delete `~/.mm/bin/mm-ts`. The TS source stays in the repo as the historical reference + the test target for diff checks.

The Go port doesn't break existing tokens, caches, or env vars. `~/.config/mm/auth.json` (audit §3) is shape-compatible. `~/.mm-cli/cards/`, `~/.mm-cli/manifests/` keep being read by the Go binary using the same JSON format.

---

## 7. What the Go binary doesn't ship

- **No node.** Self-contained.
- **No bun.** Self-contained.
- **No npm.** Self-contained.
- **No `~/.mm/pi-agent/`.** That's local-agent territory; mm-cli doesn't touch it.
- **No installer for `tailscale` / `ffmpeg` / `afplay`.** Those are user-managed. mm-cli detects missing binaries and errors with a clear "install X" message.

The install script does NOT install the local-agent. That's a separate `chat.meta-me.uk/install/local-agent` (existing) flow. Two different binaries, two different lifecycles.

#!/bin/bash
# scripts/release.sh — cross-compile mm for darwin/linux × arm64/amd64,
# compute checksums, sign macOS binaries, stage for desk.meta-me.uk deploy.
#
# Usage: scripts/release.sh vX.Y.Z

set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "Usage: $0 vX.Y.Z" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -X 'mm-cli/internal/version.Version=$VERSION' -X 'mm-cli/internal/version.Commit=$COMMIT' -X 'mm-cli/internal/version.BuildDate=$DATE'"

rm -rf dist-go
mkdir -p dist-go

# Explicit target list — windows is amd64-only and needs a .exe suffix, so a
# plain os×arch nested loop doesn't fit.
for TARGET in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  OS="${TARGET%/*}"; ARCH="${TARGET#*/}"
  EXT=""
  OUT="dist-go/mm-$OS-$ARCH$EXT"
  echo "build $OS/$ARCH"
  GOOS=$OS GOARCH=$ARCH CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" \
    -o "$OUT" ./cmd/mm
  shasum -a 256 "$OUT" | awk '{print $1}' > "$OUT.sha256"
done

# Sign macOS binaries with the stable self-signed cert. No-op if the cert
# isn't present (e.g. building on Linux); a warning is printed.
if [ "$(uname -s)" = "Darwin" ]; then
  for ARCH in arm64 amd64; do
    "$ROOT/scripts/sign-darwin.sh" "dist-go/mm-darwin-$ARCH" || true
  done

  # Build, package, and zip the mm-tray menu-bar app for the host architecture
  HOST_ARCH=$(go env GOARCH)
  echo "build tray app for darwin/$HOST_ARCH"
  TRAY_DIR="dist-go/MetaMe Tray.app"
  rm -rf "$TRAY_DIR"
  mkdir -p "$TRAY_DIR/Contents/MacOS"

  # Compile for current macOS architecture (needs CGO for systray)
  CGO_ENABLED=1 GOOS=darwin GOARCH=$HOST_ARCH go build -trimpath -ldflags="$LDFLAGS" \
    -o "$TRAY_DIR/Contents/MacOS/mm-tray" ./cmd/mm-tray

  # Create Info.plist with LSUIElement so it's a menu bar only app
  cat <<EOF > "$TRAY_DIR/Contents/Info.plist"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.1.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>mm-tray</string>
    <key>CFBundleIdentifier</key>
    <string>uk.meta-me.tray</string>
    <key>CFBundleName</key>
    <string>MetaMe Tray</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION#v}</string>
    <key>LSUIElement</key>
    <string>1</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
EOF

  # Sign the executable in the app bundle
  MM_SIGN_IDENTIFIER="uk.meta-me.tray" "$ROOT/scripts/sign-darwin.sh" "$TRAY_DIR/Contents/MacOS/mm-tray" || true

  # Zip the app bundle so it can be distributed easily via download
  echo "zipping tray app to dist-go/MetaMe-Tray-darwin-$HOST_ARCH.zip"
  (cd dist-go && zip -q -r "MetaMe-Tray-darwin-$HOST_ARCH.zip" "MetaMe Tray.app")
  rm -rf "$TRAY_DIR"
fi

# Single SHA256SUMS file the installer can verify against — binaries only, not
# the per-file .sha256 sidecars.
(cd dist-go && shasum -a 256 $(ls mm-* | grep -v '\.sha256$') > SHA256SUMS)

echo ""
echo "Built $(ls dist-go/mm-* | wc -l | tr -d ' ') binaries in dist-go/:"
ls -la dist-go/ | grep -E '^-' | awk '{printf "  %s  %s\n", $5, $NF}'

# Stage for the hub deploy.
DEST="$HOME/Documents/dev/meta-me.uk/static/dist/mm/$VERSION"
if [ -d "$HOME/Documents/dev/meta-me.uk/static/dist" ]; then
  mkdir -p "$DEST"
  cp dist-go/mm-* dist-go/SHA256SUMS "$DEST/"
  if [ -f dist-go/MetaMe-Tray-* ]; then
    cp dist-go/MetaMe-Tray-* "$DEST/"
  fi
  echo "$VERSION" > "$HOME/Documents/dev/meta-me.uk/static/dist/mm/latest"
  echo ""
  echo "Staged at $DEST"
  echo "Next: commit + push the static dir in meta-me.uk, then 'docker compose build meta-me-uk && up -d meta-me-uk'"
else
  echo ""
  echo "(meta-me.uk static dir not found at \$HOME — staging skipped)"
fi

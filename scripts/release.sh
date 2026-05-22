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

for OS in darwin linux; do
  for ARCH in arm64 amd64; do
    echo "build $OS/$ARCH"
    GOOS=$OS GOARCH=$ARCH CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" \
      -o "dist-go/mm-$OS-$ARCH" ./cmd/mm
    shasum -a 256 "dist-go/mm-$OS-$ARCH" | awk '{print $1}' > "dist-go/mm-$OS-$ARCH.sha256"
  done
done

# Sign macOS binaries with the stable self-signed cert. No-op if the cert
# isn't present (e.g. building on Linux); a warning is printed.
if [ "$(uname -s)" = "Darwin" ]; then
  for ARCH in arm64 amd64; do
    "$ROOT/scripts/sign-darwin.sh" "dist-go/mm-darwin-$ARCH" || true
  done
fi

# Single SHA256SUMS file the installer can verify against.
(cd dist-go && shasum -a 256 mm-darwin-* mm-linux-* > SHA256SUMS)

echo ""
echo "Built $(ls dist-go/mm-* | wc -l | tr -d ' ') binaries in dist-go/:"
ls -la dist-go/ | grep -E '^-' | awk '{printf "  %s  %s\n", $5, $NF}'

# Stage for the hub deploy.
DEST="$HOME/Documents/dev/meta-me.uk/static/dist/mm/$VERSION"
if [ -d "$HOME/Documents/dev/meta-me.uk/static/dist" ]; then
  mkdir -p "$DEST"
  cp dist-go/mm-* dist-go/SHA256SUMS "$DEST/"
  echo "$VERSION" > "$HOME/Documents/dev/meta-me.uk/static/dist/mm/latest"
  echo ""
  echo "Staged at $DEST"
  echo "Next: commit + push the static dir in meta-me.uk, then 'docker compose build meta-me-uk && up -d meta-me-uk'"
else
  echo ""
  echo "(meta-me.uk static dir not found at \$HOME — staging skipped)"
fi

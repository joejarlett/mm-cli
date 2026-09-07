#!/bin/bash
# Regenerate cmd/mm-tray/assets/app.icns from app.svg.
#
# The .icns is committed, not built by build-tray.sh: packaging must not need librsvg. Run
# this by hand when app.svg changes and check the result in.
# Needs: rsvg-convert (brew install librsvg), iconutil (Xcode CLT).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
command -v rsvg-convert >/dev/null || { echo "need rsvg-convert: brew install librsvg" >&2; exit 1; }

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
set=$tmp/app.iconset; mkdir -p "$set"
for s in 16 32 128 256 512; do
  rsvg-convert -w $s       -h $s       cmd/mm-tray/assets/app.svg -o "$set/icon_${s}x${s}.png"
  rsvg-convert -w $((s*2)) -h $((s*2)) cmd/mm-tray/assets/app.svg -o "$set/icon_${s}x${s}@2x.png"
done
iconutil -c icns "$set" -o cmd/mm-tray/assets/app.icns
echo "→ cmd/mm-tray/assets/app.icns"

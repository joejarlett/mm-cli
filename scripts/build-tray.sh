#!/bin/bash
# scripts/build-tray.sh — builds and packages mm-tray as a native macOS .app bundle.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "Error: mm-tray GUI packaging is currently only supported on macOS (Darwin)." >&2
  exit 1
fi

COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION=$(git describe --tags --always 2>/dev/null || echo "v1.0.0")

LDFLAGS="-s -w -X 'mm-cli/internal/version.Version=$VERSION' -X 'mm-cli/internal/version.Commit=$COMMIT' -X 'mm-cli/internal/version.BuildDate=$DATE'"

echo "→ Building mm-tray binary..."
mkdir -p dist-go
go build -trimpath -ldflags="$LDFLAGS" -o dist-go/mm-tray ./cmd/mm-tray

echo "→ Creating macOS App Bundle..."
APP_NAME="MetaMe Tray.app"
APP_DIR="dist-go/$APP_NAME"

# Clean up any existing app bundle
rm -rf "$APP_DIR"
mkdir -p "$APP_DIR/Contents/MacOS"

# Copy binary
cp dist-go/mm-tray "$APP_DIR/Contents/MacOS/mm-tray"

# Create Info.plist with LSUIElement set to 1 so there's no Dock icon
cat <<EOF > "$APP_DIR/Contents/Info.plist"
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

# Sign the binary/app bundle
echo "→ Signing app bundle..."
MM_SIGN_IDENTIFIER="uk.meta-me.tray" "$ROOT/scripts/sign-darwin.sh" "$APP_DIR/Contents/MacOS/mm-tray" || true

echo "→ App bundle created at: $APP_DIR"

# Install option
INSTALL_DEST="/Applications/$APP_NAME"
echo ""
echo "Do you want to install this to $INSTALL_DEST? (y/n)"
read -r answer
if [[ "$answer" =~ ^[Yy]$ ]]; then
  echo "→ Copying to $INSTALL_DEST..."
  # Use sudo if we don't have write permissions to /Applications
  if [ -w "/Applications" ]; then
    rm -rf "$INSTALL_DEST"
    cp -R "$APP_DIR" "$INSTALL_DEST"
  else
    echo "Requires administrator privileges to write to /Applications..."
    sudo rm -rf "$INSTALL_DEST"
    sudo cp -R "$APP_DIR" "$INSTALL_DEST"
  fi
  echo "✓ Installed MetaMe Tray to $INSTALL_DEST"
  echo ""
  echo "To run it now: open \"$INSTALL_DEST\""
  echo "To run at startup: Add it to System Settings > General > Login Items."
else
  echo "Skipped installation. App bundle is at $APP_DIR"
fi

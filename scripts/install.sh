#!/bin/bash
# scripts/install.sh — mm CLI installer, served at meta-me.uk/install.sh.
#
# Usage:
#   curl -fsSL https://meta-me.uk/install.sh | bash
#
# What it does:
#   1. Detects OS + arch → mm-<platform> binary name.
#   2. Resolves "latest" via /dist/mm/latest.
#   3. Downloads mm-<platform> + SHA256SUMS, verifies.
#   4. Installs to ~/.mm/bin/mm with atomic rename.
#   5. Symlinks both ~/.local/bin/mm AND ~/.mm/pi-agent/bin/mm
#      so the local agent's bash tool can find it (PATH gap — audit §10).

set -euo pipefail

HUB_URL="${MM_HUB_URL:-https://meta-me.uk}"
INSTALL_DIR="${MM_DIR:-$HOME/.mm}"
BIN_DIR="$INSTALL_DIR/bin"
AGENT_PATH_DIR="$INSTALL_DIR/pi-agent/bin"
USER_PATH_DIR="$HOME/.local/bin"

# ─── platform detection ────────────────────────────────────────────────

uname_os=$(uname -s)
uname_arch=$(uname -m)
case "$uname_os" in
  Darwin) os=darwin ;;
  Linux)  os=linux  ;;
  *)      echo "Unsupported OS: $uname_os" >&2; exit 1 ;;
esac
case "$uname_arch" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64)  arch=amd64 ;;
  *)             echo "Unsupported arch: $uname_arch" >&2; exit 1 ;;
esac
platform="$os-$arch"
echo "→ platform: $platform"

# ─── checksum helper ───────────────────────────────────────────────────

# macOS ships `shasum`; most Linux distros (Arch, Alpine, minimal Debian) ship
# only `sha256sum`. Support either, and refuse to install if neither exists —
# an unverified binary is worse than a failed install.
sha256_of() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    echo "Neither shasum nor sha256sum found — cannot verify download" >&2
    exit 1
  fi
}

# ─── resolve version ───────────────────────────────────────────────────

VERSION="${MM_VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSL "$HUB_URL/dist/mm/latest" | tr -d '[:space:]')
  if [ -z "$VERSION" ]; then
    echo "Could not resolve latest version from $HUB_URL/dist/mm/latest" >&2
    exit 1
  fi
fi
echo "→ version: $VERSION"

# ─── download + verify ─────────────────────────────────────────────────

mkdir -p "$BIN_DIR" "$AGENT_PATH_DIR" "$USER_PATH_DIR"

bin_url="$HUB_URL/dist/mm/$VERSION/mm-$platform"
sums_url="$HUB_URL/dist/mm/$VERSION/SHA256SUMS"
tmp_bin="$BIN_DIR/mm.new"

echo "→ downloading $bin_url"
curl -fsSL "$bin_url" -o "$tmp_bin"

echo "→ verifying checksum"
got=$(sha256_of "$tmp_bin")
want=$(curl -fsSL "$sums_url" | grep "mm-$platform$" | head -1 | awk '{print $1}')
if [ -z "$want" ]; then
  echo "SHA256SUMS missing entry for mm-$platform" >&2
  rm -f "$tmp_bin"
  exit 1
fi
if [ "$got" != "$want" ]; then
  echo "checksum mismatch: want $want, got $got" >&2
  rm -f "$tmp_bin"
  exit 1
fi

chmod +x "$tmp_bin"
mv "$tmp_bin" "$BIN_DIR/mm"

# ─── symlinks ──────────────────────────────────────────────────────────

ln -sf "$BIN_DIR/mm" "$USER_PATH_DIR/mm"
ln -sf "$BIN_DIR/mm" "$AGENT_PATH_DIR/mm"

echo ""
echo "✓ installed $VERSION at $BIN_DIR/mm"
echo "  symlinked at $USER_PATH_DIR/mm and $AGENT_PATH_DIR/mm"
echo ""

# ─── post-install notes ────────────────────────────────────────────────

case ":$PATH:" in
  *":$USER_PATH_DIR:"*) ;;
  *)
    echo "note: $USER_PATH_DIR is not on \$PATH"
    echo "      add this to ~/.zshrc or ~/.bashrc:"
    echo "        export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
    ;;
esac

echo "Next: run \`mm login\` to authenticate."

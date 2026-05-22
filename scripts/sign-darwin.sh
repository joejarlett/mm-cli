#!/bin/bash
# scripts/sign-darwin.sh — sign a macOS mm binary with the stable
# self-signed cert reused from meta-me-local-agent's signing setup.
#
# Why: ad-hoc signing rotates the cdhash on every rebuild, breaking any
# TCC grant a future invocation would need (e.g. if mm gets called from
# a non-Terminal context like a hotkey wrapper). A stable identity +
# identifier keeps the signature constant.
#
# Cert setup is one-time in ~/Documents/dev/meta-me-local-agent/scripts/
# create-signing-cert.sh — same cert serves both binaries.

set -euo pipefail

if [ "$(uname -s)" != "Darwin" ]; then
  exit 0  # not macOS — nothing to sign
fi

BIN="${1:-}"
if [ -z "$BIN" ]; then
  echo "Usage: $0 <binary-path>" >&2
  exit 1
fi
if [ ! -f "$BIN" ]; then
  echo "Not a file: $BIN" >&2
  exit 1
fi

IDENTITY="${MM_SIGN_IDENTITY:-MetaMe Local Agent}"
IDENTIFIER="${MM_SIGN_IDENTIFIER:-uk.meta-me.cli}"

# Probe the keychain. `find-identity -p codesigning` filters on *trusted*
# identities; our self-signed cert isn't in the trust store but `codesign
# --sign` works by common name regardless. So check with `find-certificate`.
if ! security find-certificate -c "$IDENTITY" >/dev/null 2>&1; then
  echo "warning: signing identity '$IDENTITY' not found in keychain — skipping sign" >&2
  echo "         (run ~/Documents/dev/meta-me-local-agent/scripts/create-signing-cert.sh first if you want stable signatures)" >&2
  exit 0
fi

codesign --force --sign "$IDENTITY" --identifier "$IDENTIFIER" "$BIN"
echo "✓ signed $BIN as $IDENTIFIER (identity: $IDENTITY)"

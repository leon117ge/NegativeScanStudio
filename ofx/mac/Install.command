#!/bin/bash
# NegativeScan Studio OFX plugin installer for macOS (Apple Silicon).
# Copies NegativeScanStudio.ofx.bundle to /Library/OFX/Plugins, clears the download
# quarantine flag and re-signs the bundle ad hoc so DaVinci Resolve loads it.
set -euo pipefail
NAME="NegativeScanStudio.ofx.bundle"
DEST_DIR="/Library/OFX/Plugins"
HERE="$(cd "$(dirname "$0")" && pwd)"
SRC="$HERE/$NAME"
DEST="$DEST_DIR/$NAME"
finish() {
  if [ -t 0 ]; then echo; read -r -p "Press Return to close this window." _ || true; fi
}
fail() {
  echo "Install failed: $1"
  finish
  exit 1
}
echo "NegativeScan Studio OFX plugin installer"
echo "----------------------------------------"
[ -d "$SRC/Contents/MacOS" ] || fail "$NAME was not found next to this script (looked in $HERE). Unzip the whole download first."
ARCH="$(uname -m)"
if [ "$ARCH" != "arm64" ]; then
  echo "Warning: this plugin is built for Apple Silicon (arm64). This Mac reports $ARCH, so the plugin will not load here."
fi
echo "Installing to $DEST_DIR. macOS will ask for your administrator password."
sudo -v || fail "administrator access was not granted."
sudo mkdir -p "$DEST_DIR" || fail "could not create $DEST_DIR."
if [ -e "$DEST" ]; then
  echo "Removing the previous version..."
  sudo rm -rf "$DEST" || fail "could not remove the previous version at $DEST."
fi
echo "Copying the plugin..."
sudo ditto "$SRC" "$DEST" || fail "could not copy the plugin to $DEST."
echo "Clearing the download quarantine flag..."
sudo xattr -dr com.apple.quarantine "$DEST" 2>/dev/null || true
if xattr -r "$DEST" 2>/dev/null | grep -q com.apple.quarantine; then
  fail "the quarantine flag is still set on $DEST."
fi
echo "Signing the plugin for this Mac (ad hoc)..."
sudo codesign --force --sign - --timestamp=none "$DEST" >/dev/null 2>&1 || fail "codesign could not sign $DEST."
codesign --verify --strict "$DEST" >/dev/null 2>&1 || fail "the signature check failed for $DEST."
echo
echo "Done. NegativeScan Studio OFX is installed in $DEST_DIR."
echo "Restart DaVinci Resolve, then find it under Effects > OpenFX > NegativeScan Studio."
finish

#!/usr/bin/env bash
# Builds NegativeScanStudio.ofx.bundle for macOS on Apple Silicon (arm64), CPU render path,
# signs it ad hoc and packages it with Install.command into a zip. Runs on macOS with the
# Xcode command line tools. Output: <out>/NegativeScanStudio.ofx.bundle and
# <out>/NegativeScanStudio_OFX_v<version>_macOS_arm64.zip
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/ofx/build-mac}"
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
VERSION="$(sed -n 's/.*"NegativeScan Studio OFX \([0-9][0-9.]*\)".*/\1/p' "$ROOT/ofx/src/plugin.cpp" | head -n 1)"
[ -n "$VERSION" ] || { echo "Could not read the plugin version from plugin.cpp" >&2; exit 1; }
# Same math flags as the Windows build. -ffp-contract=off keeps a*b+c as two rounded
# operations (no fused multiply-add), as on the x64 build. No chip-specific -mcpu.
MAC_FLAGS=(-arch arm64 -mmacosx-version-min=12.0 -std=c++17 -O3 -fno-math-errno -fno-trapping-math -ffp-contract=off)
python3 "$ROOT/ofx/tools/generate.py"
BUNDLE="$OUT/NegativeScanStudio.ofx.bundle"
C="$BUNDLE/Contents"
rm -rf "$BUNDLE"
mkdir -p "$C/MacOS" "$C/Resources/licenses"
"${CXX:-clang++}" "${MAC_FLAGS[@]}" -fvisibility=hidden -Wall -Wextra -Wno-unused-parameter -bundle \
  -I"$ROOT/ofx/vendor/openfx/include" -I"$ROOT/ofx/vendor/opencl" \
  "$ROOT/ofx/src/plugin.cpp" -o "$C/MacOS/NegativeScanStudio.ofx"
sed "s/@VERSION@/$VERSION/g" "$ROOT/ofx/mac/Info.plist.in" > "$C/Info.plist"
if command -v plutil >/dev/null; then plutil -lint "$C/Info.plist"; fi
cp "$ROOT/LICENSE" "$C/Resources/licenses/NegativeScanStudio-LICENSE.txt"
cp "$ROOT/ofx/vendor/openfx/LICENSE.md" "$C/Resources/licenses/OpenFX-BSD-3-Clause.txt"
cp "$ROOT/ofx/vendor/json/LICENSE.MIT" "$C/Resources/licenses/nlohmann-json-MIT.txt"
if command -v codesign >/dev/null; then
  codesign --force --sign - --timestamp=none "$BUNDLE"
  codesign --verify --strict --verbose=2 "$BUNDLE"
else
  echo "codesign not found: bundle left unsigned (not on macOS)" >&2
fi
PKG="NegativeScanStudio_OFX_v${VERSION}_macOS_arm64"
STAGE="$OUT/$PKG"
rm -rf "$STAGE" "$OUT/$PKG.zip"
mkdir -p "$STAGE"
cp -R "$BUNDLE" "$STAGE/"
cp "$ROOT/ofx/mac/Install.command" "$ROOT/ofx/mac/README-macOS.txt" "$STAGE/"
chmod +x "$STAGE/Install.command"
if command -v ditto >/dev/null; then
  (cd "$OUT" && ditto -c -k --keepParent "$PKG" "$PKG.zip")
else
  (cd "$OUT" && zip -qry "$PKG.zip" "$PKG")
fi
echo "$OUT/$PKG.zip"

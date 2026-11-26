#!/usr/bin/env bash
# Builds NegativeScanStudio.ofx.bundle for macOS on Apple Silicon (arm64), signs it ad hoc
# and packages it with Install.command into a zip. Runs on macOS with the Xcode command
# line tools.
#
# Render backends: CPU always. Metal (W2) is added when NSS_METAL=1, or by default when
# ofx/src/metal_render.mm exists (NSS_METAL=auto); NSS_METAL=0 forces a CPU-only build.
#
# Version: the single source is plugin.cpp (label "NegativeScan Studio OFX X.Y.Z" and the
# OfxPlugin version major/minor). The script checks they agree and stamps Info.plist and
# the zip name with that version.
#
# Output: <out>/NegativeScanStudio.ofx.bundle and
#         <out>/NegativeScanStudio_OFX_v<version>_macOS_arm64.zip
# The zip holds NegativeScanStudio.ofx.bundle, Install.command and README-macOS.txt at its root.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/ofx/build-mac}"
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
SRC="$ROOT/ofx/src"

VERSION="$(sed -n 's/.*"NegativeScan Studio OFX \([0-9][0-9.]*\)".*/\1/p' "$SRC/plugin.cpp" | head -n 1)"
[ -n "$VERSION" ] || { echo "Could not read the plugin label version from plugin.cpp" >&2; exit 1; }
MAJMIN="$(sed -n 's/.*OfxPlugin plugin={kOfxImageEffectPluginApi,[0-9]*,"[^"]*",\([0-9]*\),\([0-9]*\),.*/\1 \2/p' "$SRC/plugin.cpp" | head -n 1)"
[ -n "$MAJMIN" ] || { echo "Could not read the OfxPlugin version from plugin.cpp" >&2; exit 1; }
read -r MAJOR MINOR <<<"$MAJMIN"
CALL_VERSION="$MAJOR.$((MINOR / 100)).$((MINOR % 100))"
if [ "$CALL_VERSION" != "$VERSION" ]; then
  echo "Version mismatch in plugin.cpp: label $VERSION, OfxPlugin version $MAJOR/$MINOR ($CALL_VERSION)" >&2
  exit 1
fi
[[ "$VERSION" =~ ^[0-9]+(\.[0-9]+){1,3}$ ]] || { echo "Version $VERSION is not plain digits and dots" >&2; exit 1; }

METAL="${NSS_METAL:-auto}"
if [ "$METAL" = auto ]; then
  if [ -f "$SRC/metal_render.mm" ]; then METAL=1; else METAL=0; fi
fi
if [ "$METAL" = 1 ] && [ ! -f "$SRC/metal_render.mm" ]; then
  echo "NSS_METAL=1 but ofx/src/metal_render.mm is missing (the Metal path is on feat/metal-render)" >&2
  exit 1
fi

# Same math flags as the Windows build. -ffp-contract=off keeps a*b+c as two rounded
# operations (no fused multiply-add), as on the x64 build. No chip-specific -mcpu.
MAC_FLAGS=(-arch arm64 -mmacosx-version-min=13.0 -std=c++17 -O3 -fno-math-errno -fno-trapping-math -ffp-contract=off)
export MACOSX_DEPLOYMENT_TARGET=13.0
WARN=(-Wall -Wextra -Wno-unused-parameter)
INC=(-I"$ROOT/ofx/vendor/openfx/include" -I"$ROOT/ofx/vendor/opencl")
python3 "$ROOT/ofx/tools/generate.py"
BUNDLE="$OUT/NegativeScanStudio.ofx.bundle"
C="$BUNDLE/Contents"
OBJ="$OUT/obj"
rm -rf "$BUNDLE" "$OBJ"
mkdir -p "$C/MacOS" "$C/Resources/licenses" "$OBJ"
CXX="${CXX:-clang++}"
if [ "$METAL" = 1 ]; then
  echo "Building $VERSION with the Metal and CPU render paths"
  "$CXX" "${MAC_FLAGS[@]}" -fvisibility=hidden "${WARN[@]}" -fobjc-arc -DNSS_METAL "${INC[@]}" -c "$SRC/metal_render.mm" -o "$OBJ/metal_render.o"
  "$CXX" "${MAC_FLAGS[@]}" -fvisibility=hidden "${WARN[@]}" -DNSS_METAL "${INC[@]}" -c "$SRC/plugin.cpp" -o "$OBJ/plugin.o"
  "$CXX" -arch arm64 -mmacosx-version-min=13.0 -bundle "$OBJ/plugin.o" "$OBJ/metal_render.o" \
    -framework Metal -framework Foundation -o "$C/MacOS/NegativeScanStudio.ofx"
  ENGINE="Metal + CPU"
else
  echo "Building $VERSION with the CPU render path only"
  "$CXX" "${MAC_FLAGS[@]}" -fvisibility=hidden "${WARN[@]}" "${INC[@]}" -c "$SRC/plugin.cpp" -o "$OBJ/plugin.o"
  "$CXX" -arch arm64 -mmacosx-version-min=13.0 -bundle "$OBJ/plugin.o" -o "$C/MacOS/NegativeScanStudio.ofx"
  ENGINE="CPU"
fi
# Release builds never contain the Metal test hooks (NSS Builder's -DNSS_TEST_HOOKS guard;
# test builds compile their own metal_render object with it).
python3 "$ROOT/ofx/tests/check_release_hooks.py" "$C/MacOS/NegativeScanStudio.ofx"
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
  (cd "$STAGE" && ditto -c -k . "$OUT/$PKG.zip")
else
  (cd "$STAGE" && python3 - "$OUT/$PKG.zip" <<'PY'
import os, sys, zipfile
with zipfile.ZipFile(sys.argv[1], 'w', zipfile.ZIP_DEFLATED) as z:
    for base, dirs, files in os.walk('.'):
        dirs.sort()
        for name in sorted(files):
            path = os.path.join(base, name)
            info = zipfile.ZipInfo.from_file(path, os.path.relpath(path, '.'))
            info.compress_type = zipfile.ZIP_DEFLATED
            with open(path, 'rb') as f:
                z.writestr(info, f.read())
PY
  )
fi
echo "version=$VERSION"
echo "engine=$ENGINE"
echo "zip=$OUT/$PKG.zip"

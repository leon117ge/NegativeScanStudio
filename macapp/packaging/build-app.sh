#!/bin/bash
# Builds NegativeScan Studio.app (macOS arm64, macOS 13 or later) from macapp/.
# usage: bash macapp/packaging/build-app.sh [out-dir]     (default: macapp/build)
# Call it with bash: the GitHub connector does not keep exec bits.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
mod=$(cd "$here/.." && pwd)
out=${1:-$mod/build}
mkdir -p "$out"
out=$(cd "$out" && pwd)
export CGO_ENABLED=1 GOOS=darwin GOARCH=${GOARCH:-arm64}
export MACOSX_DEPLOYMENT_TARGET=13.0
export CGO_CFLAGS="${CGO_CFLAGS:--O2} -mmacosx-version-min=13.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=13.0"
(cd "$mod" && go build -trimpath -buildvcs=false -o "$out/NegativeScanStudio" .)
app=$(python3 "$here/mkbundle.py" "$out/NegativeScanStudio" "$out" | head -1)
# ad-hoc signature (no Developer ID yet); arm64 binaries must be signed to run
codesign --force --deep --sign - "$app" 2>/dev/null || echo "codesign not available: bundle left unsigned"
echo "$app"

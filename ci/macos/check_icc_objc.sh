#!/usr/bin/env bash
# Box-side compile check of the W6 ImageCaptureCore layer without a Mac:
#  1. camera/icc_darwin.m as Objective-C with ARC, -Wall -Wextra -Werror, against the stub
#     headers in ci/macos/icc_stub (macOS 13 SDK signatures, never shipped);
#  2. the darwin/arm64 cgo build of ./camera (Go <-> C types, the .m through cgo) and go vet of
#     ./camera and ./cmd/mactether. Nothing is linked (no Apple frameworks on Linux).
# Needs zig (python3 -m ziglang) and Go. On a Mac, CI compiles and links against the real SDK.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
printf '#!/bin/sh\nexec python3 -m ziglang cc -target aarch64-macos.13.0 "$@"\n' > "$TMP/cc"
chmod +x "$TMP/cc"
STUB="$ROOT/ci/macos/icc_stub"
"$TMP/cc" -x objective-c -fobjc-arc -Wall -Wextra -Wno-unused-parameter -Werror -I"$STUB" -c camera/icc_darwin.m -o "$TMP/icc_darwin.o"
echo "PASS: camera/icc_darwin.m compiles (Objective-C, ARC, -Wall -Wextra -Werror) against the stub SDK headers"
export CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC="$TMP/cc" CXX="$TMP/cc" CGO_CFLAGS="-I$STUB -O2"
go build -a ./camera
go vet ./camera ./cmd/mactether
echo "PASS: darwin/arm64 cgo build of ./camera and go vet of ./camera ./cmd/mactether (no link)"

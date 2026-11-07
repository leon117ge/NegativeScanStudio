#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD="${1:-$ROOT/ofx/test-build}"
SCAN="${2:-/workspace/uploads/0206.tiff}"
mkdir -p "$BUILD"
cd "$ROOT"
# Set NATIVE_CXX to an executable wrapper for 'python3 -m ziglang c++' if needed.
CXX="${NATIVE_CXX:-c++}"
python3 ofx/tools/generate.py
"$CXX" -std=c++17 -O2 -Iofx/vendor/openfx/include ofx/tests/host_test.cpp -o "$BUILD/host_test"
"$CXX" -std=c++17 -O2 ofx/tests/math_runner.cpp -o "$BUILD/math_runner"
go build -o "$BUILD/go_reference" ./cmd/ofxreference
NSS_META_FIXTURE="$BUILD/metadata-fixture.tiff" go test ./hdrscan -run TestScanTIFFMetadataAndHairPreservation -count=1
"$BUILD/host_test" "$BUILD/metadata-fixture.tiff"
python3 ofx/tests/check_metadata.py "$BUILD"
python3 ofx/tests/check_parity.py "$BUILD" "$SCAN"

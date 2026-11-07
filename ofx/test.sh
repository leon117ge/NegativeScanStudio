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
python3 ofx/tools/cuda_source.py
"$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -Iofx/vendor/openfx/include -Iofx/vendor/opencl ofx/tests/host_test.cpp -o "$BUILD/host_test"
"$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math ofx/tests/math_runner.cpp -o "$BUILD/math_runner"
go build -o "$BUILD/go_reference" ./cmd/ofxreference
NSS_META_FIXTURE="$BUILD/metadata-fixture.tiff" go test ./hdrscan -run TestScanTIFFMetadataAndHairPreservation -count=1
"$BUILD/host_test" "$BUILD/metadata-fixture.tiff" "$SCAN"
"$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -Iofx/vendor/openfx/include -Iofx/vendor/opencl ofx/tests/cuda_contract.cpp -o "$BUILD/cuda_contract"
"$BUILD/cuda_contract"
python3 ofx/tests/check_metadata.py "$BUILD" "$SCAN"
python3 ofx/tests/check_parity.py "$BUILD" "$SCAN"
if [[ "${NSS_TEST_OPENCL:-0}" == 1 ]]; then
 "$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -shared -fPIC -Iofx/vendor/openfx/include -Iofx/vendor/opencl ofx/tests/gpu_bridge.cpp -o "$BUILD/gpu_bridge.so"
 python3 ofx/tests/check_opencl.py "$BUILD" "$SCAN"
fi

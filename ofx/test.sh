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
"$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math ofx/tests/tone_test.cpp -o "$BUILD/tone_test"
"$BUILD/tone_test"
go build -o "$BUILD/go_reference" ./cmd/ofxreference
NSS_META_FIXTURE="$BUILD/metadata-fixture.tiff" go test ./hdrscan -run TestScanTIFFMetadataAndHairPreservation -count=1
"$BUILD/host_test" "$BUILD/metadata-fixture.tiff" "$SCAN"
"$CXX" -std=c++17 -O0 -Iofx/vendor/openfx/include -Iofx/vendor/opencl ofx/tests/leveling_metadata_test.cpp -o "$BUILD/leveling_metadata_test"
NSS_LEVELING_FIXTURE="$BUILD/leveling" go test ./hdrscan -run TestScanMetadataUsesLastLevelingNotScene -count=1
"$BUILD/leveling_metadata_test" "$BUILD/leveling-false.tiff" "$BUILD/leveling-true.tiff" "$BUILD/metadata-fixture.tiff"
"$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -Iofx/vendor/openfx/include -Iofx/vendor/opencl ofx/tests/cuda_contract.cpp -o "$BUILD/cuda_contract"
"$BUILD/cuda_contract"
"$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -Iofx/vendor/openfx/include -Iofx/vendor/opencl ofx/tests/overlay_test.cpp -o "$BUILD/overlay_test"
"$BUILD/overlay_test"
# Metal (macOS) contract on any OS: embedded MSL source check, then the exact kernel as host C++
# behind a mocked Metal API. Real Metal runs in .github/workflows/macos-metal.yml.
python3 ofx/tests/check_metal_source.py "$BUILD"
python3 ofx/tests/check_release_hooks.py --source ofx/src/metal_render.mm
"$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -Wno-attributes -Iofx/vendor/openfx/include -Iofx/vendor/opencl -Iofx/tests/metal_stub -I"$BUILD" ofx/tests/metal_contract.cpp -o "$BUILD/metal_contract"
"$BUILD/metal_contract"
# macOS UI variant of the same contract (visible Rendering controls), then per-platform parameter
# definitions: Windows/Linux identical to 0.90.14, macOS differs only in the Rendering group.
"$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -Wno-attributes -DNSS_MAC_RENDER_UI=1 -Iofx/vendor/openfx/include -Iofx/vendor/opencl -Iofx/tests/metal_stub -I"$BUILD" ofx/tests/metal_contract.cpp -o "$BUILD/metal_contract_mac_ui"
"$BUILD/metal_contract_mac_ui"
"$CXX" -std=c++17 -O1 -Iofx/vendor/openfx/include -Iofx/vendor/opencl ofx/tests/param_dump.cpp -o "$BUILD/param_dump"
"$CXX" -std=c++17 -O1 -DNSS_MAC_RENDER_UI=1 -Iofx/vendor/openfx/include -Iofx/vendor/opencl ofx/tests/param_dump.cpp -o "$BUILD/param_dump_mac_ui"
"$BUILD/param_dump" > "$BUILD/params_win_linux.txt"
"$BUILD/param_dump_mac_ui" > "$BUILD/params_mac.txt"
python3 ofx/tests/check_param_defs.py "$BUILD/params_win_linux.txt" "$BUILD/params_mac.txt"
python3 ofx/tests/check_metadata.py "$BUILD" "$SCAN"
python3 ofx/tests/check_parity.py "$BUILD" "$SCAN"
if [[ "${NSS_TEST_OPENCL:-0}" == 1 ]]; then
 "$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -shared -fPIC -Iofx/vendor/openfx/include -Iofx/vendor/opencl ofx/tests/gpu_bridge.cpp -o "$BUILD/gpu_bridge.so"
 python3 ofx/tests/check_opencl.py "$BUILD" "$SCAN"
fi

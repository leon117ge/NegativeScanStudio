#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/ofx/build}"
mkdir -p "$OUT/NegativeScanStudio.ofx.bundle/Contents/Win64"
python3 "$ROOT/ofx/tools/generate.py"
"${CXX:-x86_64-w64-mingw32-g++}" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -Wall -Wextra -Wno-unused-parameter \
 -shared -static -static-libgcc -static-libstdc++ -I"$ROOT/ofx/vendor/openfx/include" -I"$ROOT/ofx/vendor/opencl" \
 "$ROOT/ofx/src/plugin.cpp" -o "$OUT/NegativeScanStudio.ofx.bundle/Contents/Win64/NegativeScanStudio.ofx"
cp "$ROOT/ofx/README.md" "$OUT/README.md"
cp "$ROOT/LICENSE" "$OUT/LICENSE"
mkdir -p "$OUT/licenses"
cp "$ROOT/ofx/vendor/openfx/LICENSE.md" "$OUT/licenses/OpenFX-BSD-3-Clause.txt"
cp "$ROOT/ofx/vendor/json/LICENSE.MIT" "$OUT/licenses/nlohmann-json-MIT.txt"
cp "$ROOT/ofx/vendor/opencl/LICENSE" "$OUT/licenses/OpenCL-Headers-Apache-2.0.txt"
cp "$ROOT/ofx/tools/export_metadata.py" "$OUT/export_metadata.py"
python3 - "$OUT" <<'PY'
import pathlib,sys,zipfile
out=pathlib.Path(sys.argv[1])
with zipfile.ZipFile(out/'NegativeScanStudio_OFX_v0.90.1_Win64.zip','w',zipfile.ZIP_DEFLATED) as z:
 for path in sorted(out.rglob('*')):
  if path.is_file() and path.suffix!='.zip':z.write(path,path.relative_to(out))
print(out/'NegativeScanStudio_OFX_v0.90.1_Win64.zip')
PY

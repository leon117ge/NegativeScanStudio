#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="${1:?absolute build directory}"
SCAN="${2:-/workspace/uploads/0206.tiff}"
CXX="${NATIVE_CXX:-c++}"
mkdir -p "$BUILD/baseline"
cd "$ROOT"
# Immutable pre-optimization reference, not the mutable working tree.
git show fc8dd261eff65732360ab177fa3f37439c1e4719:ofx/src/pipeline.hpp > "$BUILD/baseline/pipeline.hpp"
git show fc8dd261eff65732360ab177fa3f37439c1e4719:ofx/src/dctl_math.hpp > "$BUILD/baseline/dctl_math.hpp"
python3 - "$BUILD" "$SCAN" <<'PY'
import pathlib,sys,numpy as np,tifffile
build=pathlib.Path(sys.argv[1]);a=tifffile.imread(sys.argv[2]);v=np.ones((*a.shape[:2],4),dtype=np.float32);v[:,:,:3]=a/65535.;v.tofile(build/'benchmark-rgba.f32')
for name in ['pipeline.hpp','dctl_math.hpp']:
 p=build/'baseline'/name;p.write_text(p.read_text().replace('namespace nss {','namespace nss_v090 {'))
(build/'dimensions.txt').write_text(f'{a.shape[1]} {a.shape[0]}\n')
PY
read -r WIDTH HEIGHT < "$BUILD/dimensions.txt"
"$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math -I"$BUILD" ofx/tests/benchmark.cpp -o "$BUILD/benchmark"
uptime
"$BUILD/benchmark" "$BUILD/benchmark-rgba.f32" "$WIDTH" "$HEIGHT" 5 > "$BUILD/benchmark-result.json"
python3 - "$BUILD/benchmark-result.json" "$ROOT/docs/data/ofx_0901_cpu_benchmark.json" "$CXX" <<'PY'
import json,sys,platform,pathlib,subprocess
r=json.load(open(sys.argv[1]));r['settings']='Both .5 film base, original gamma/exposure/Cineon, identity camera; new measured 500T Base emulated on old reference with WB=None and manual R+1.57/B+1.00';r['cpu']=platform.processor() or platform.machine();r['hardware']={x['field'].rstrip(':'):x['data'] for x in json.loads(subprocess.check_output(['lscpu','-J'],text=True))['lscpu'] if x['field'] in ['CPU(s):','Model name:','Model:','Hypervisor vendor:']};r['compiler']=subprocess.check_output([sys.argv[3],'--version'],text=True).splitlines()[0];r['warmup']='One full frame for each of old / prepared single / prepared parallel';r['compiler_flags']='-O3 -fno-math-errno -fno-trapping-math (both sides)';r['baseline_commit']='fc8dd261eff65732360ab177fa3f37439c1e4719';r['speedup']=r['before']['median_ms']/r['prepared_parallel']['median_ms'];pathlib.Path(sys.argv[2]).write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r,indent=2))
PY

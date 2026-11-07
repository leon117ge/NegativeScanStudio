#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="${1:?absolute build dir}";SCAN="${2:-/workspace/uploads/0206.tiff}";CXX="${NATIVE_CXX:-c++}"
mkdir -p "$BUILD/old-0901";cd "$ROOT"
git archive ofx-v0.90.1 ofx | tar -x -C "$BUILD/old-0901"
python3 - "$BUILD" "$SCAN" <<'PY2'
import pathlib,sys,numpy as np,tifffile
p=pathlib.Path(sys.argv[1]);a=tifffile.imread(sys.argv[2]);v=np.ones((*a.shape[:2],4),np.float32);v[:,:,:3]=a/65535;v.tofile(p/'host-rgba.f32');(p/'host-dimensions.txt').write_text(f'{a.shape[1]} {a.shape[0]}')
PY2
for variant in old new;do
 if [[ "$variant" == old ]];then HOST="$BUILD/old-0901/ofx/tests/host_test.cpp";EXTRA=();else HOST="$ROOT/ofx/tests/host_test.cpp";EXTRA=(-DNSS_BENCH_CURRENT);fi
 "$CXX" -std=c++17 -O3 -fno-math-errno -fno-trapping-math "${EXTRA[@]}" "-DNSS_HOST_HEADER=\"$HOST\"" -I"$ROOT/ofx/vendor/openfx/include" -I"$ROOT/ofx/vendor/opencl" "$ROOT/ofx/tests/host_benchmark.cpp" -o "$BUILD/host-benchmark-$variant"
done
uptime;nproc
python3 - "$BUILD" "$ROOT" <<'PY2'
import pathlib,json,statistics,subprocess,sys,platform
b=pathlib.Path(sys.argv[1]);root=pathlib.Path(sys.argv[2]);w,h=(b/'host-dimensions.txt').read_text().split();report={}
for mode,count in [('full',1),('tile',2000)]:
 runs={'old':[],'new':[]}
 for index in range(5):
  for variant in ['old','new']:
   r=subprocess.run([str(b/f'host-benchmark-{variant}'),str(b/'host-rgba.f32'),w,h,str(b/'metadata-fixture.tiff'),mode,str(count)],capture_output=True,text=True,check=True)
   value=json.loads(r.stdout);runs[variant].append(value);print(mode,index+1,variant,value['ms_per_render'],flush=True)
 for index in range(5):assert abs(runs['old'][index]['checksum']-runs['new'][index]['checksum'])<1e-3
 report[mode]={k:dict(median_ms=statistics.median(r['ms_per_render'] for r in v),min_ms=min(r['ms_per_render'] for r in v),max_ms=max(r['ms_per_render'] for r in v),runs=v) for k,v in runs.items()}
report['hardware']={v['field'].rstrip(':'):v['data'] for v in json.loads(subprocess.check_output(['lscpu','-J'],text=True))['lscpu'] if v['field'] in ['CPU(s):','Model name:','Model:','Hypervisor vendor:']};report['platform']=platform.platform()
report.update(baseline='ofx-v0.90.1',compiler_flags='-O3 -fno-math-errno -fno-trapping-math (both)',settings='Matched effective gamma0.55/0.635/0.62 x1.15, Exposure0.90, visible balances1.40/0/0.80, pending camera/base metadata, Cineon, picker off',scope='Actual CPU OFX Render under minimal mock suites; includes metadata/parameter/image/preparation/thread launch. Excludes real Resolve scheduling/transfers, GPU, TIFF decode and process startup. One warmup per process; five alternating old/new pairs.',cuda_gpu_executed=False)
(root/'docs/data/ofx_0902_host_benchmark.json').write_text(json.dumps(report,indent=2)+'\n')
PY2

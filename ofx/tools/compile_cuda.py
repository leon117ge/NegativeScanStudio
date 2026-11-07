#!/usr/bin/env python3
"""Offline PTX build. NVRTC does not require a GPU or nvcc. Never needed by users.
Install nvidia-cuda-nvrtc-cu12==12.0.140; optional nvidia-cuda-nvcc-cu12==12.0.140
provides ptxas for offline assembly verification. No toolkit binaries are bundled.
"""
import ctypes as c,pathlib,json,hashlib,subprocess,sys
ROOT=pathlib.Path(__file__).resolve().parents[2]
import nvidia.cuda_nvrtc
lib=c.CDLL(str(pathlib.Path(nvidia.cuda_nvrtc.__file__).parent/'lib/libnvrtc.so.12'))
def ok(result):
 if result:raise RuntimeError('NVRTC status '+str(result))
program=c.c_void_p()
from cuda_source import make_source
source=make_source()
ok(lib.nvrtcCreateProgram(c.byref(program),source.encode(),b'nss_cuda.cu',0,None,None))
options=[b'--gpu-architecture=compute_52',b'--std=c++14',b'--fmad=false',b'--ftz=false',b'--prec-div=true',b'--prec-sqrt=true']
args=(c.c_char_p*len(options))(*options)
try:
 result=lib.nvrtcCompileProgram(program,len(options),args)
 n=c.c_size_t();ok(lib.nvrtcGetProgramLogSize(program,c.byref(n)));log=c.create_string_buffer(n.value);ok(lib.nvrtcGetProgramLog(program,log))
 if log.value:print(log.value.decode())
 ok(result);ok(lib.nvrtcGetPTXSize(program,c.byref(n)));ptx=c.create_string_buffer(n.value);ok(lib.nvrtcGetPTX(program,ptx))
 text=ptx.value.decode();(ROOT/'ofx/src/cuda_kernel.ptx').write_text(text)
 (ROOT/'ofx/src/cuda_source.hpp').write_text('#pragma once\nnamespace nss {inline constexpr const char *cudaPTX=R"NSSPTX('+text+')NSSPTX";}\n')
 report=dict(compiler='NVIDIA NVRTC 12.0.140',target='compute_52',options=[x.decode() for x in options],source_sha256=hashlib.sha256(source.encode()).hexdigest(),ptx_sha256=hashlib.sha256(ptx.value).hexdigest(),ptx_bytes=len(ptx.value),real_gpu_executed=False,real_resolve_tested=False)
 try:
  import nvidia.cuda_nvcc
  assembler=pathlib.Path(nvidia.cuda_nvcc.__file__).parent/'bin/ptxas'
  output=pathlib.Path(sys.argv[1] if len(sys.argv)>1 else '/tmp/nss-cuda.cubin')
  r=subprocess.run([str(assembler),'-arch=sm_52','-v',str(ROOT/'ofx/src/cuda_kernel.ptx'),'-o',str(output)],text=True,capture_output=True,check=True)
  report.update(assembly_verified=True,assembler_output=r.stdout+r.stderr);print(r.stdout+r.stderr)
 except ImportError:report['assembly_verified']=False
 (ROOT/'docs/data/ofx_0902_cuda_build.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,indent=2))
finally:ok(lib.nvrtcDestroyProgram(c.byref(program)))

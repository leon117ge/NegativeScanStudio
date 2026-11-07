#!/usr/bin/env python3
"""Build/check the CUDA source fingerprint without requiring a CUDA toolkit."""
import pathlib,hashlib,json
ROOT=pathlib.Path(__file__).resolve().parents[2]
PREFIX='#define NSS_EXP expf\n#define NSS_EXP2 exp2f\n#define NSS_LOG logf\n#define NSS_LOG2 log2f\n#define NSS_POW powf\n#define NSS_TANH tanhf\n#define NSS_MIN fminf\n#define NSS_MAX fmaxf\n#define inline __device__ __forceinline__\n'
def make_source():return PREFIX+(ROOT/'ofx/src/render_math.h').read_text()+'\n#undef inline\n'+(ROOT/'ofx/src/render_cuda.cu').read_text()
def check():
 report=json.loads((ROOT/'docs/data/ofx_0902_cuda_build.json').read_text())
 source=make_source();text=(ROOT/'ofx/src/cuda_kernel.ptx').read_text()
 assert hashlib.sha256(source.encode()).hexdigest()==report['source_sha256'],'CUDA source changed; regenerate PTX with compile_cuda.py'
 assert hashlib.sha256(text.encode()).hexdigest()==report['ptx_sha256'],'PTX hash mismatch'
 assert (ROOT/'ofx/src/cuda_source.hpp').read_text()=='#pragma once\nnamespace nss {inline constexpr const char *cudaPTX=R"NSSPTX('+text+')NSSPTX";}\n','Embedded PTX mismatch'
 print('PASS: embedded PTX matches committed CUDA source and offline assembly record')
if __name__=='__main__':check()

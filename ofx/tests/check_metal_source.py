#!/usr/bin/env python3
"""Check the embedded Metal source: render_math.h verbatim except the MSL 'thread' address space on
plain pointer parameters, precise math, the render.metal kernel, and no fast-math switches.
Writes the exact embedded source to <build>/nss_render.metal for offline compiles and emulation."""
import difflib,pathlib,re,sys
ROOT=pathlib.Path(__file__).resolve().parents[2]
build=pathlib.Path(sys.argv[1] if len(sys.argv)>1 else ROOT/'ofx/test-build');build.mkdir(parents=True,exist_ok=True)
header=(ROOT/'ofx/src/metal_source.hpp').read_text()
m=re.search(r'R"NSSMTL\((.*)\)NSSMTL"',header,re.S);assert m,'metalSource raw string not found'
source=m.group(1)
math=(ROOT/'ofx/src/render_math.h').read_text();kernel=(ROOT/'ofx/src/render.metal').read_text()
assert source.endswith(kernel),'embedded kernel differs from render.metal (run tools/generate.py)'
first=math.splitlines(True)[0];start=source.index(first);prefix=source[:start];msl_math=source[start:len(source)-len(kernel)]
assert msl_math.replace('thread float *','float *')==math,'embedded math differs from render_math.h beyond the thread address space'
changed=[l for l in difflib.ndiff(math.splitlines(),msl_math.splitlines()) if l.startswith(('+ ','- '))]
assert len(changed)==2 and 'nss_tone_color' in changed[0] and changed[1].count('thread float *')==3,changed
for f in ['EXP','EXP2','LOG','LOG2','POW','TANH','MIN','MAX']:
 name={'MIN':'fmin','MAX':'fmax'}.get(f,f.lower())
 assert f'#define NSS_{f} precise::{name}\n' in prefix,f'NSS_{f} must use metal::precise'
assert '#define NSS_PARAMS constant const float *' in prefix
assert not re.search(r'fast_math|fast::|#pragma\s+METAL',source),'no fast-math switches in the kernel source'
assert 'kernel void nss_render_metal(' in kernel
(build/'nss_render.metal').write_text(source)
print(f'PASS: embedded Metal source = render_math.h + thread address space on 3 parameters ({len(source)} bytes); precise math; wrote {build/"nss_render.metal"}')

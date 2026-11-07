#!/usr/bin/env python3
"""Compare OFX C++ math to independent existing Go DCTL evaluator, not a render proxy."""
import sys,pathlib,json,subprocess,random
import numpy as np,tifffile
root=pathlib.Path(__file__).resolve().parents[2];build=pathlib.Path(sys.argv[1]);scan=sys.argv[2]
a=tifffile.imread(scan)
# Fixed 16x16 grid of real scan samples including film edge, plus boundaries/random.
y=np.linspace(0,a.shape[0]-1,16).astype(int);x=np.linspace(0,a.shape[1]-1,16).astype(int)
pixels=(a[y[:,None],x[None,:]].reshape(-1,3)/65535).tolist()
rng=random.Random(90)
pixels+=[[0,0,0],[1,1,1],[-.05,.000001,1.2],[.02740667,.02740668,.02740669]]
pixels+=[[rng.uniform(.001,.8) for _ in range(3)] for _ in range(100)]
cases=[]
for stage in range(3):
 for mode in range(5):
  for contrast in range(4):
   for gray in range(2):
    cases.append(dict(stage=stage,pixels=pixels,density={},cross={},positive={'out_mode':mode,'contrast':contrast,'gray_on':gray}))
# Additional non-default density/crosstalk controls; matrix values match slider DCTL.
for mode in range(5):
 for inp in range(2):
  for trans in range(2):
   cases.append(dict(stage=2,pixels=pixels,density={'base_r':.5,'base_g':.5,'base_b':.5,'in_di':inp,'out_trans':trans,'cast_on':1,'cast_r':.039,'cast_b':-.083,'blk_r':.0001,'ev_stops':-.2,'clamp01':0},cross={'strength':.5,'mask_on':1,'bd_r':.01,'bd_g':.02,'bd_b':.03,'m00':1.6,'m01':-.8,'m02':.2,'m10':-.2,'m11':1.5,'m12':-.3,'clamp01':1},positive={'out_mode':mode,'film_wb':0,'gam_r':.45,'gam_g':.54,'gam_b':.53,'expo':-.5,'bal_r':.1}))
text=''.join(json.dumps(c)+'\n' for c in cases)
def run(exe):
 r=subprocess.run([str(build/exe)],input=text,text=True,capture_output=True,check=True)
 return np.array([json.loads(s) for s in r.stdout.splitlines()],dtype=np.float32).astype(np.float64)
a=run('math_runner');b=run('go_reference');diff=np.abs(a-b)
idx=np.unravel_index(np.argmax(diff),diff.shape);maximum=float(diff[idx]);print('cases',len(cases),'pixels/case',len(pixels),'channel comparisons',diff.size,'max_abs',maximum,'at',idx)
report={'cases':len(cases),'pixels_per_case':len(pixels),'channel_comparisons':diff.size,'max_absolute_error':maximum,'worst_index':list(map(int,idx)),'absolute_plus_relative_tolerance':'1e-5 * max(1, abs(reference))','passed':bool(np.all(diff <= 1e-5*np.maximum(1,np.abs(b)))),'real_scan_patch_max_abs':float(diff[:,:256,:].max()),'max_scaled_error':float((diff/np.maximum(1,np.abs(b))).max()),'scan':'0206.tiff','note':'CPU math vs independent Go Develop DCTL port, no actual Resolve/DCTL GPU execution'}
(root/'docs/data/ofx_parity_result.json').write_text(json.dumps(report,indent=2)+'\n')
if not report['passed']:
 print('worst',a[idx],b[idx],cases[idx[0]]['positive']);sys.exit(1)

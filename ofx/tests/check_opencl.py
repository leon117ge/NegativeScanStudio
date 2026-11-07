#!/usr/bin/env python3
"""Real OpenCL buffers/host queue -> actual OFX Render entry -> compare optimized CPU.
PoCL is a CPU OpenCL implementation, NOT evidence of Resolve/NVIDIA/AMD performance.
"""
import ctypes,json,pathlib,sys,subprocess,itertools
import numpy as np,pyopencl as cl,tifffile
build=pathlib.Path(sys.argv[1]);root=pathlib.Path(__file__).resolve().parents[2]
ctx=cl.create_some_context(interactive=False);queue=cl.CommandQueue(ctx)
lib=ctypes.CDLL(str(build/'gpu_bridge.so'))
lib.nss_test_gpu.argtypes=[ctypes.c_void_p]*3+[ctypes.c_int]*3+[ctypes.c_char_p];lib.nss_test_gpu.restype=ctypes.c_int
lib.nss_test_cpu.argtypes=[ctypes.c_void_p]*2+[ctypes.c_int]*3+[ctypes.c_char_p];lib.nss_test_cpu.restype=ctypes.c_int
lib.nss_test_gpu_status.restype=ctypes.c_char_p
rng=np.random.default_rng(901)
a=tifffile.imread(sys.argv[2]);ys=np.linspace(0,a.shape[0]-1,16).astype(int);xs=np.linspace(0,a.shape[1]-1,16).astype(int)
pixels=(a[ys[:,None],xs[None,:]].reshape(-1,3)/65535).astype(np.float32)
pixels=np.vstack([pixels,rng.uniform(.001,1,(124,3)).astype(np.float32),[[0,0,0],[1,1,1],[-.01,.02,.9],[.5,.000001,.000001]]]).astype(np.float32)
cases=[]
for stage,mode,contrast,gray in itertools.product([0,1],range(5),range(4),range(2)):
 for premult in [0,1]:
  cases.append(dict(stage=stage,pixels=pixels.tolist(),density={'base_r':.5,'base_g':.5,'base_b':.5},positive={'out_mode':mode,'contrast':contrast,'gray_on':gray},premult=premult))
for di,trans in itertools.product([0,1],[0,1]):
 cases.append(dict(stage=1,pixels=pixels.tolist(),camera_on=1,camera=[1.1,-.1,0,0,1.02,-.02,.01,-.01,1],camera_strength=.7,density={'base_r':.6,'base_g':.5,'base_b':.35,'in_di':di,'out_trans':trans,'clamp01':0,'ev_stops':-.5,'blk_r':.001},positive={'film_wb':0,'out_mode':4,'gam_r':.45,'gam_g':.54,'gam_b':.53},premult=1))
# CPU consumes unpremultiplied samples. Alpha zeros use exact zero RGB inputs.
lines=''.join(json.dumps(c)+'\n' for c in cases)
r=subprocess.run([str(build/'math_runner')],input=lines,text=True,capture_output=True,check=True)
cpu=np.array([json.loads(s) for s in r.stdout.splitlines()],dtype=np.float32)
maximum=scaled=0.;count=0
try:
 for index,c in enumerate(cases):
  alpha=np.ones((len(pixels),1),np.float32);alpha[::3]=.5
  data=np.concatenate([pixels.copy(),alpha],axis=1).astype(np.float32)
  if c['premult']:data[:,:3]*=alpha
  src=cl.Buffer(ctx,cl.mem_flags.READ_ONLY|cl.mem_flags.COPY_HOST_PTR,hostbuf=data);dest=cl.Buffer(ctx,cl.mem_flags.READ_WRITE,size=data.nbytes)
  status=lib.nss_test_gpu(queue.int_ptr,src.int_ptr,dest.int_ptr,len(data),1,c['premult'],json.dumps(c).encode())
  if status!=0:raise RuntimeError((status,lib.nss_test_gpu_status().decode()))
  result=np.empty_like(data);cl.enqueue_copy(queue,result,dest).wait()
  expected=cpu[index].copy()
  if c['premult']:expected*=alpha
  delta=np.abs(result[:,:3].astype(np.float64)-expected.astype(np.float64));scale=delta/np.maximum(1,np.abs(expected))
  maximum=max(maximum,float(delta.max()));scaled=max(scaled,float(scale.max()));count+=delta.size
  if not np.all(np.isfinite(result)) or not np.array_equal(result[:,3],alpha[:,0]) or np.any(scale>2e-5):raise AssertionError((index,float(delta.max()),float(scale.max())))
 # Point prepass: real 2D rectangular GPU download vs CPU source sampling.
 for premult,radius,px,py in [(0,0,24,3),(1,3,24,3),(0,32,1,0),(1,0,-100,-100)]:
  data=np.concatenate([pixels.copy(),np.ones((len(pixels),1),np.float32)],axis=1).astype(np.float32)
  data[::3,3]=.5;data[0]=0
  if premult:data[:,:3]*=data[:,3:4]
  c=dict(stage=1,camera_on=1,camera=[1.1,-.1,0,0,1.02,-.02,.01,-.01,1],positive={'film_wb':0},pick=dict(x=10+px+.5,y=20+py+.5,radius=radius))
  encoded=json.dumps(c).encode();expected=np.zeros_like(data)
  assert lib.nss_test_cpu(data.ctypes.data,expected.ctypes.data,48,8,premult,encoded)==0
  src=cl.Buffer(ctx,cl.mem_flags.READ_ONLY|cl.mem_flags.COPY_HOST_PTR,hostbuf=data);dest=cl.Buffer(ctx,cl.mem_flags.READ_WRITE,size=data.nbytes)
  assert lib.nss_test_gpu(queue.int_ptr,src.int_ptr,dest.int_ptr,48,8,premult,encoded)==0,lib.nss_test_gpu_status()
  result=np.empty_like(data);cl.enqueue_copy(queue,result,dest).wait()
  delta=np.abs(result[:,:3].astype(np.float64)-expected[:,:3].astype(np.float64));scale=delta/np.maximum(1,np.abs(expected[:,:3]))
  assert np.all(np.isfinite(result)) and np.all(scale<2e-5),(delta.max(),scale.max())
  maximum=max(maximum,float(delta.max()));scaled=max(scaled,float(scale.max()));count+=delta.size
  if radius==0 and px>=0:
   picked=result[py*48+px,:3];assert np.max(picked)-np.min(picked)<2e-5,picked
 # Missing host queue must fail safely, never treat device buffers as CPU memory.
 last_success=lib.nss_test_gpu_status().decode();assert lib.nss_test_gpu(None,src.int_ptr,dest.int_ptr,48,8,0,b'{}')==1002
 print('PASS OpenCL actual OFX entry:',len(cases),'cases',count,'channel comparisons','max_abs',maximum,'scaled',scaled,last_success)
 report=dict(cases=len(cases),viewer_pick_cases=4,missing_queue_status=1002,passed=True,channel_comparisons=count,max_absolute_error=maximum,max_scaled_error=scaled,tolerance='2e-5 * max(1, abs(CPU))',platform=ctx.devices[0].platform.name,device=ctx.devices[0].name,device_type=str(cl.device_type.to_string(ctx.devices[0].type)),path='Actual OFX Render action + runtime-loaded CL bridge + host buffers/queue',real_resolve_tested=False,physical_gpu_tested=False)
 (root/'docs/data/ofx_0901_opencl_result.json').write_text(json.dumps(report,indent=2)+'\n')
finally:queue.finish();lib.nss_test_gpu_close()

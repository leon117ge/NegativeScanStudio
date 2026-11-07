#!/usr/bin/env python3
import json,pathlib,subprocess,sys
import numpy as np,tifffile
build=pathlib.Path(sys.argv[1]);exe=build/'math_runner'
base=dict(schema='nss.scan/1',encoding='linear',film_base=[.48,.5,.46],light_rgb=[1200,2300,3400],camera_applied=False)
def run(p,good):
 r=subprocess.run([str(exe),str(p)],capture_output=True,text=True)
 assert (r.returncode==0)==good,(p,r.returncode,r.stdout,r.stderr)
 if good:assert abs(json.loads(r.stdout)['base'][0]-.48)<1e-6
# Big-endian classic TIFF, UTF-8 path, uncompressed/LZW independence.
p=build/'元数据.tiff';tifffile.imwrite(p,np.zeros((2,2,3),dtype=np.uint16),photometric='rgb',byteorder='>',description=json.dumps(base));run(p,True)
for key,value in [('film_base',[1,2]),('film_base',[.5,.5,-1]),('film_base',[1,2,3,4]),('encoding','cineon'),('schema','other'),('processing','mask already applied'),('camera_matrix',[1,2]),('camera_strength',2)]:
 m=base.copy();m[key]=value
 if key=='camera_strength':m['camera_matrix']=[1,0,0,0,1,0,0,0,1]
 p=build/'invalid.json';p.write_text(json.dumps(m));run(p,False)
p=build/'truncated.tiff';p.write_bytes(b'II*\x00\xff\xff\xff\xff');run(p,False)
p=build/'sidecar.json';p.write_text(json.dumps(base));run(p,True)
print('PASS: BE TIFF, Unicode filename, JSON sidecar, malformed/truncated/schema/encoding/base/matrix validation')

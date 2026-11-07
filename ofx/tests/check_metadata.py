#!/usr/bin/env python3
import json,pathlib,subprocess,sys
import numpy as np,tifffile
from urllib.parse import quote
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
# Classic and BigTIFF, both byte orders; quoted paths and file:// URL decoding.
for big in (False,True):
 for order in ('<','>'):
  p=build/f'扫描 大文件-{big}-{order==">"}.tiff'
  tifffile.imwrite(p,np.zeros((2,2,3),dtype=np.uint16),photometric='rgb',byteorder=order,bigtiff=big,description=json.dumps(base))
  run(p,True);run('  "'+str(p)+'"  ',True);run('file://'+quote(str(p)),True)
p=build/'bom.json';p.write_text(json.dumps(base),encoding='utf-8-sig');run(p,True)
# A sparse >4GiB BigTIFF: tag270 above 32-bit offsets, without reading pixels.
import struct
p=build/'sparse-big.tiff';desc=json.dumps(base).encode()+b'\0';offset=2**32+4096
with p.open('wb') as f:
 f.write(struct.pack('<2sHHHQ',b'II',43,8,0,16));f.write(struct.pack('<QHHQQQ',1,270,2,len(desc),offset,0));f.seek(offset);f.write(desc)
run(p,True);p.unlink()
# User's 156MB historical file really has no ImageDescription (not a load failure).
if len(sys.argv)>2:
 p=pathlib.Path(sys.argv[2])
 with tifffile.TiffFile(p) as t:assert 270 not in t.pages[0].tags
 r=subprocess.run([str(exe),str(p)],capture_output=True,text=True)
 assert r.returncode==2 and r.stderr.strip()=='No NSS info (old scan)',r
print('PASS: classic/BigTIFF LE/BE, >4GiB offset, Unicode/quoted/file URL paths, BOM JSON, old scan, validation')

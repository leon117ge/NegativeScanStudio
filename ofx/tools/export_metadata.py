#!/usr/bin/env python3
"""Export NSS JSON from classic TIFF ImageDescription; standard library only.
Usage: python export_metadata.py scan.tiff [output.nss.json]
Never overwrites an existing sidecar.
"""
import pathlib,struct,json,sys
p=pathlib.Path(sys.argv[1]);target=pathlib.Path(sys.argv[2]) if len(sys.argv)>2 else p.with_suffix(p.suffix+'.nss.json')
with p.open('rb') as f:
 h=f.read(8);order={b'II':'<',b'MM':'>'}.get(h[:2]);assert order and len(h)==8,'Not classic TIFF'
 u16=lambda b:struct.unpack(order+'H',b)[0];u32=lambda b:struct.unpack(order+'I',b)[0]
 assert u16(h[2:4])==42,'BigTIFF is not supported'
 f.seek(u32(h[4:]));n=u16(f.read(2));assert n<=4096,'Invalid IFD'
 for _ in range(n):
  e=f.read(12)
  if u16(e[:2])!=270:continue
  assert u16(e[2:4])==2,'Description must be ASCII'
  length=u32(e[4:8]);assert length<=1048576,'Metadata too large'
  if length<=4:data=e[8:8+length]
  else:f.seek(u32(e[8:12]));data=f.read(length)
  m=json.loads(data.rstrip(b'\0'));assert m['schema']=='nss.scan/1','Not NSS scan metadata'
  with target.open('x',encoding='utf-8') as out:json.dump(m,out,indent=2,ensure_ascii=True);out.write('\n')
  print(target);break
 else:raise SystemExit('No ImageDescription: old scans need manual base values')

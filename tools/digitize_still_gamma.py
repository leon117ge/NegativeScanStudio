#!/usr/bin/env python3
"""Extract Kodak official PDF polyline characteristic curves and fit midscale gamma.
Usage: python tools/digitize_still_gamma.py DIRECTORY_WITH_portra_ultramax_gold_ektar_PDFs
Requires pymupdf, numpy. PDF charts inspected manually; path IDs are edition-specific.
"""
import pathlib,sys,hashlib,csv,json
import pymupdf,numpy as np
root=pathlib.Path(__file__).resolve().parents[1];src=pathlib.Path(sys.argv[1])
config=[
 ('ultramax','UltraMax 400','February 2016 E-7023','https://kodakprofessional.com/sites/default/files/wysiwyg/pro/resources/E7023_max_400.pdf',[35,34,33],81.09,265.904,-4,1,297.352,112.746),
 ('portra','Portra 400','January 2025 E-4050, revised 1-25','https://www.kodakprofessional.com/sites/default/files/2025-07/e4050.pdf',[36,35,34],81.163,265.626,-4,1,287.368,102.853),
 ('gold','Gold 200','June 2023 E-7022, revised 06-23','https://www.kodakprofessional.com/sites/default/files/wysiwyg/pro/resources/E7022%20Gold%20tech%20sheet.pdf',[38,37,36],81.6768,266.18448,-3,1,256.4328,71.92512),
 ('ektar','Ektar 100','January 2025 E-4046, revised 1/25','https://www.kodakprofessional.com/sites/default/files/2025-07/e4046.pdf',[33,32,31],81.1929931640625,265.697998046875,-3,2,292.4079895019531,107.92399597167969)]
result=[]
for key,name,rev,url,ids,x0,x1,h0,h1,y0,y1 in config:
 pdf=src/(key+'.pdf');page=pymupdf.open(pdf)[3];drawings=page.get_drawings();channels=[];rows=[]
 for c,id in enumerate(ids):
  points=[]
  for seg in drawings[id]['items']:
   if seg[0]=='l': points.extend([tuple(seg[1]),tuple(seg[2])])
   elif seg[0]=='c':
    a,b,c_,d_=[np.array(tuple(v)) for v in seg[1:]]
    for t in np.linspace(0,1,101): points.append((1-t)**3*a+3*(1-t)**2*t*b+3*(1-t)*t*t*c_+t**3*d_)
   else: raise ValueError(('Unexpected path',key,id,seg[0]))
  points=np.unique(np.array(points),axis=0);points=points[np.argsort(points[:,0])]
  h=h0+(points[:,0]-x0)*(h1-h0)/(x1-x0);d=(y0-points[:,1])*4/(y0-y1)
  def fit(a,b):
   xx=np.linspace(a,b,61);yy=np.interp(xx,h,d);slope,intercept=np.polyfit(xx,yy,1);return float(slope),float(np.sqrt(np.mean((yy-(slope*xx+intercept))**2)))
  gamma,rms=fit(-1.5,0);channels.append(dict(channel='RGB'[c],gamma=gamma,rms_density=rms,alternate_gamma=[fit(-2,-.5)[0],fit(-1,.5)[0]]))
  rows.extend([[name,'RGB'[c],float(x),float(y),float(a),float(b)] for (x,y),a,b in zip(points,h,d)])
 with (root/f'colorsci/data/{key}_characteristic.csv').open('w') as f:
  w=csv.writer(f,lineterminator="\n");w.writerow(['stock','channel','pdf_x','pdf_y','logH','density']);w.writerows(rows)
 result.append(dict(stock=name,source=url,revision=rev,page=4,pdf_sha256=hashlib.sha256(pdf.read_bytes()).hexdigest(),path_ids_RGB=ids,axis=dict(x0=x0,x1=x1,logH0=h0,logH1=h1,yD0=y0,yD4=y1),midscale_logH=[-1.5,0],channels=channels))
(root/'docs/data/ofx_stock_gamma_readings.json').write_text(json.dumps(result,indent=2)+'\n')
(root/'ofx/src/still_gammas.inc').write_text('// Generated from Kodak official PDF polylines.\n'+',\n'.join('{'+','.join(f'{c["gamma"]:.9f}f' for c in r['channels'])+'}' for r in result)+'\n')
for r in result:print(r['stock'],[round(c['gamma'],6) for c in r['channels']])

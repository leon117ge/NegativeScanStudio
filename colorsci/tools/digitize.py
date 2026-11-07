# Digitise the "Spectral-Dye-Density Curves" chart (page 4) of Kodak
# E-4046 (Ektar 100, rev. 1/25) and E-4050 (Portra 400, rev. 1/25),
# rendered at 300 dpi with pdftoppm. Frame pixel coordinates found from the
# axis lines; x 400..700 nm, y 0..2.5 diffuse spectral density.
import numpy as np, json, sys
from PIL import Image
frames={'e4046':(1490,2255,443,1208),'e4050':(1489,2255,433,1198)}
out={}
for f,(x0,x1,y0,y1) in frames.items():
    a=np.asarray(Image.open(f+'_p4-4.png').convert('L')).astype(int)
    dark=a<110
    def runs(x,lo,hi):
        col=dark[lo:hi,x]; r=[];i=0
        while i<len(col):
            if col[i]:
                j=i
                while j<len(col) and col[j]: j+=1
                r.append((lo+i+lo+j-1)/2.0); i=j
            else: i+=1
        return r
    # seed at 402 nm column: take runs in plot interior
    xs=list(range(x0+22,x1-22))
    seed=runs(x0+22,y0+8,y1-8)
    seed=[s for s in seed]
    print(f,'seed runs',seed,file=sys.stderr)
    # upper = neutral (smaller y), lower = dmin
    seed=[r for r in seed if r>y0+0.3*(y1-y0)]
    cur={'neutral':min(seed),'dmin':max(seed)}
    tr={'neutral':[],'dmin':[]}
    for x in xs:
        rr=runs(x,y0+8,y1-8)
        for k in cur:
            c=[r for r in rr if abs(r-cur[k])<14]
            if c:
                cur[k]=min(c,key=lambda r:abs(r-cur[k]))
            tr[k].append((x,cur[k]))
    res={}
    for k,v in tr.items():
        wl=[400+(x-x0)/(x1-x0)*300 for x,_ in v]
        d=[(y1-y)/(y1-y0)*2.5 for _,y in v]
        grid=np.arange(402,700,2.0)
        res[k]=[round(float(np.interp(g,wl,d)),3) for g in grid]
    res['wl']=[float(g) for g in np.arange(402,700,2.0)]
    out[f]=res
json.dump(out,open('curves.json','w'))
from PIL import ImageDraw
for f,(x0,x1,y0,y1) in frames.items():
    im=Image.open(f+'_p4-4.png').convert('RGB').crop((x0-60,y0-40,x1+40,y1+60))
    dr=ImageDraw.Draw(im)
    for k,col in (('neutral',(255,0,0)),('dmin',(0,160,255))):
        for w,d in zip(out[f]['wl'],out[f][k]):
            x=x0+(w-400)/300*(x1-x0)-(x0-60); y=y1-d/2.5*(y1-y0)-(y0-40)
            dr.ellipse((x-3,y-3,x+3,y+3),fill=col)
    im.save(f+'_digitised_check.png')
for f in out:
    for k in ('neutral','dmin'):
        print(f,k,' '.join('%d:%.2f'%(w,v) for w,v in zip(out[f]['wl'],out[f][k]) if int(w)%25==0 or int(w)%50==0))

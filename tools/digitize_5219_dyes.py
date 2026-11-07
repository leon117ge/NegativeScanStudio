#!/usr/bin/env python3
import numpy as np, csv
from PIL import Image,ImageDraw
from pathlib import Path
import argparse
p = argparse.ArgumentParser(description='Transcribe Kodak p4 dye plot and ISO 1995 Table 4')
p.add_argument('plot_png', help='Original 686 x 730 Kodak dye-density raster')
p.add_argument('--overlay', default='/tmp/5219-digitization-overlay.png')
args = p.parse_args()
out=Path(__file__).resolve().parents[1] / 'colorsci/data'
# Hand-read centreline estimates at 10 nm; snapping to nearest ink is limited to 4px.
vals={
'cyan':[.20,.15,.11,.075,.045,.025,.013,.010,.012,.018,.024,.034,.048,.067,.095,.126,.16,.208,.265,.34,.435,.535,.63,.72,.80,.87,.928,.970,.996,1,.984,.95,.90,.82,.735,.64,.54,.44,.35,.25,.14],
'magenta':[-.025,-.03,-.03,-.02,-.04,-.035,.015,.095,.22,.38,.55,.71,.85,.97,1,.94,.80,.61,.435,.30,.21,.15,.115,.09,.077,.068,.063,.058,.054,.052,.049,.047,.044,.039,.036,.031,.026,.023,.02,.015,.01],
'yellow':[.48,.64,.81,.94,.995,.98,.89,.73,.55,.39,.28,.21,.17,.14,.12,.098,.075,.053,.035,.02,.012,.007,.002,0,-.002,-.005,-.006,-.006,-.007,-.006,-.006,-.005,-.003,-.002,0,0,.002,.002,.002,.002,0],
'midscale_neutral':[1.10,1.26,1.44,1.59,1.65,1.62,1.55,1.47,1.40,1.37,1.39,1.43,1.41,1.40,1.41,1.39,1.32,1.21,1.10,.97,.82,.76,.78,.81,.82,.82,.85,.88,.91,.92,.91,.88,.84,.78,.71,.63,.54,.45,.36,.27,.18],
'minimum_density':[.58,.66,.75,.82,.86,.85,.83,.80,.77,.73,.72,.73,.72,.62,.56,.54,.53,.53,.54,.51,.39,.28,.24,.23,.20,.16,.15,.145,.14,.135,.13,.125,.12,.115,.105,.10,.095,.09,.08,.075,.065]
}
# Corrections after inspecting every vertical ink cluster and overlay.
vals['cyan']=[.20,.159,.117,.078,.046,.025,.014,.014,.014,.018,.025,.034,.046,.064,.088,.124,.163,.210,.263,.339,.436,.537,.634,.724,.804,.878,.936,.979,1.004,1.002,.975,.917,.843,.749,.645,.538,.432,.328,.258,.191,.14]
vals['yellow']=[.48,.638,.792,.920,.993,1,.938,.809,.650,.489,.348,.247,.189,.152,.127,.106,.085,.060,.041,.028,.016,.011,.007,.004,.002,0,-.002,-.002,-.004,-.004,-.004,-.002,0,.002,.002,.004,.005,.005,.005,.005,0]
vals['midscale_neutral']=[1.10,1.254,1.424,1.581,1.648,1.633,1.585,1.507,1.433,1.375,1.389,1.433,1.406,1.399,1.410,1.357,1.261,1.147,1.035,.883,.781,.765,.809,.818,.814,.857,.887,.910,.922,.913,.890,.843,.779,.703,.617,.532,.445,.36,.288,.231,.18]
vals['minimum_density']=[.58,.668,.75,.829,.86,.859,.832,.805,.770,.724,.721,.725,.640,.555,.541,.535,.535,.539,.52,.405,.297,.25,.246,.214,.17,.155,.15,.147,.143,.139,.136,.133,.126,.119,.111,.106,.099,.092,.083,.076,.065]
im=Image.open(args.plot_png).convert('RGB'); a=np.asarray(im).mean(2)
rows=[]; overlay=im.copy(); draw=ImageDraw.Draw(overlay)
for i,nm in enumerate(range(400,801,10)):
 x=96+(nm-400)*566/400; row=[nm,x]
 for k,ds in vals.items():
  yp=589-283*ds[i]; xx=int(round(x)); yy=int(round(yp))
  if i not in [0,40]:
   candidates=[y for y in range(max(80,yy-4),min(644,yy+5)) if a[y,xx]<150]
   if candidates: yy=min(candidates,key=lambda y:abs(y-yp))
  else: yy=yp
  row += [yy,(589-yy)/283]
  draw.ellipse((x-2,yy-2,x+2,yy+2),outline={'cyan':'red','magenta':'lime','yellow':'blue','midscale_neutral':'orange','minimum_density':'magenta'}[k])
 rows.append(row)
with open(out/'5219_spectral_dye_density.csv','w') as f:
 w=csv.writer(f);w.writerow(['wavelength_nm','pixel_x']+[v for k in vals for v in [k+'_pixel_y',k+'_density']]);w.writerows(rows)
overlay.save(args.overlay)
# ANSI/ISO 5-3:1995 Table 4. Values are log10 spectral products, peak=5.
channels={'red':(620,[2.109,4.479,5,4.899,4.578,4.252,3.875,3.491,3.099,2.687,2.269,1.859,1.449,1.054,.654,.254],.260,-.040),'green':(470,[1.152,2.207,3.156,3.804,4.272,4.626,4.872,5,4.995,4.818,4.458,3.915,3.172,2.239,1.070],.106,-.120),'blue':(410,[2.103,4.111,4.632,4.871,5,4.955,4.743,4.343,3.743,2.990,1.852],.250,-.220)}
with open(out/'status_m_log_products.csv','w') as f:
 w=csv.writer(f);w.writerow(['wavelength_nm','red_log10_product','green_log10_product','blue_log10_product'])
 for nm in range(400,801,10):
  row=[nm]
  for start,vs,lo,hi in channels.values():
   end=start+(len(vs)-1)*10
   row.append(vs[0]+lo*(nm-start) if nm<start else vs[-1]+hi*(nm-end) if nm>end else vs[(nm-start)//10])
  w.writerow(row)

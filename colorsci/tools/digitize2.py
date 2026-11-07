# Digitise the "Spectral-Dye-Density Curves" chart (page 4) of
#   Kodak E-7022 KODAK GOLD 200 Film (rev. 06-23), chart beside E7022B/C
#   Kodak E-7023 KODAK ULTRA MAX 400 Film (rev. 2/16), chart E7023C
# rendered at 300 dpi (pdftoppm -r 300). Frame = axis box line centres,
# x 400..700 nm, y 0..2.5 diffuse spectral density. The UltraMax neutral
# curve is dashed: columns without ink are skipped and interpolated.
import numpy as np, json, sys
from PIL import Image, ImageDraw
frames = {'e7022': (1487.5, 2255.5, 230.5, 998.5), 'e7023': (1487.5, 2255.5, 470.5, 1238.5)}
seeds = {'e7022': {'neutral': 1.28, 'dmin': 0.76}, 'e7023': {'neutral': 1.32, 'dmin': 0.80}}
dmax = {'e7022': 2.0, 'e7023': 2.02}
out = json.load(open('curves.json'))
for f, (x0, x1, y0, y1) in frames.items():
    a = np.asarray(Image.open(f + '_p4-4.png').convert('L')).astype(int)
    dark = a < 120
    D2y = lambda d: y1 - d / 2.5 * (y1 - y0)
    def runs(x):
        lo, hi = int(D2y(dmax[f])), int(y1 - 8)
        col = dark[lo:hi, x]; r = []; i = 0
        while i < len(col):
            if col[i]:
                j = i
                while j < len(col) and col[j]: j += 1
                if j - i < 20: r.append(lo + (i + j - 1) / 2.0)
                i = j
            else: i += 1
        return r
    pts = {k: [] for k in seeds[f]}
    for x in range(int(x0) + 20, int(x1) - 6):
        rr = runs(x)
        for k in pts:
            p = pts[k]
            if len(p) >= 2:
                (xa, ya), (xb, yb) = p[-6] if len(p) >= 6 else p[0], p[-1]
                pred = yb + (yb - ya) / max(xb - xa, 1) * (x - xb); gate = 10 + 0.4 * (x - xb)
            elif p: pred, gate = p[-1][1], 12
            else: pred, gate = D2y(seeds[f][k]), 30
            c = [r for r in rr if abs(r - pred) < gate]
            if c: p.append((x, min(c, key=lambda r: abs(r - pred))))
    res = {}
    grid = np.arange(402, 700, 2.0)
    for k, p in pts.items():
        wl = np.array([400 + (x - x0) / (x1 - x0) * 300 for x, _ in p]); d = np.array([(y1 - y) / (y1 - y0) * 2.5 for _, y in p])
        print(f, k, 'points', len(p), 'wl %.0f..%.0f' % (wl[0], wl[-1]), file=sys.stderr)
        res[k] = [round(float(v), 3) for v in np.interp(grid, wl, d)]
    res['wl'] = [float(g) for g in grid]
    out[f] = res
    im = Image.open(f + '_p4-4.png').convert('RGB').crop((int(x0) - 60, int(y0) - 40, int(x1) + 40, int(y1) + 60))
    dr = ImageDraw.Draw(im)
    for k, col in (('neutral', (255, 0, 0)), ('dmin', (0, 160, 255))):
        for w, dd in zip(res['wl'], res[k]):
            x = x0 + (w - 400) / 300 * (x1 - x0) - (x0 - 60); y = y1 - dd / 2.5 * (y1 - y0) - (y0 - 40)
            dr.ellipse((x - 3, y - 3, x + 3, y + 3), fill=col)
    im.save(f + '_digitised_check.png')
    for k in ('neutral', 'dmin'):
        print(f, k, ' '.join('%d:%.2f' % (w, v) for w, v in zip(res['wl'], res[k]) if int(w) % 50 == 0))
json.dump(out, open('curves.json', 'w'))

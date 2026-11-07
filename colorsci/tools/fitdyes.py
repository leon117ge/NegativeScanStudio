#!/usr/bin/env python3
"""Decompose the datasheet 'midscale neutral minus D-min' spectral density of
Kodak Ektar 100 (E-4046, rev 1/25, chart E4046C) and Portra 400 (E-4050,
rev 1/25, chart E4050C), UltraMax 400 (E-7023, rev 2/16, chart E7023C) and
Gold 200 (E-7022, rev 06-23) into yellow/magenta/cyan image-dye curves.

The datasheets publish only the TOTAL spectral density of a midscale neutral
and of D-min, not separate dye curves (unlike Vision3 H-1-5219). So the dye
shapes are modelled as the Vision3 5219 curves (colorsci DyeYellow/Magenta/
Cyan) with a per-dye peak shift and width scale, plus the consumption of the
coloured masking couplers (negative terms with the shapes of the two D-min
absorption bands). The fit is not unique (the magenta-coupler band at ~440
nm is almost collinear with the yellow dye); a weak prior keeps shifts and
widths near the 5219 shapes. Output: dyes.json.
"""
import json, numpy as np
from scipy.optimize import least_squares
cv = json.load(open('curves.json'))
V3 = {  # colorsci/spectral.go, Kodak H-1-5219 p.4
 'Y': ([400,425,445,450,475,500,525,550,575,600,625,650,700,750,800],[0.50,0.86,1.00,1.00,0.83,0.42,0.20,0.12,0.07,0.035,0.02,0.015,0.01,0.01,0.01], 447.5),
 'M': ([400,425,450,475,500,525,540,550,575,600,625,650,700,750,800],[0,0,0,0.15,0.56,0.90,1.00,0.93,0.52,0.20,0.10,0.07,0.05,0.03,0.01], 540),
 'C': ([400,425,450,475,500,525,550,575,600,625,650,675,685,700,725,750,775,800],[0.20,0.10,0.04,0.02,0.02,0.04,0.09,0.20,0.40,0.64,0.85,0.98,1.00,0.98,0.85,0.62,0.40,0.15], 685),
}
def shape(k, wl, s, w):
    x, v, pk = V3[k]
    return np.interp(pk + (wl - pk - s) / w, x, v)
def gauss(wl, mu, sg): return np.exp(-0.5 * ((wl - mu) / sg) ** 2)

out = {}
for f, name in (('e4046', 'Ektar 100'), ('e4050', 'Portra 400'), ('e7023', 'UltraMax 400'), ('e7022', 'Gold 200')):
    wl = np.array(cv[f]['wl']); ne = np.array(cv[f]['neutral']); dm = np.array(cv[f]['dmin'])
    # the trace starts at ~408 nm; extrapolate the first points linearly
    i0 = np.searchsorted(wl, 410)
    sl = (ne[i0 + 3] - ne[i0]) / (wl[i0 + 3] - wl[i0]); ne[:i0] = ne[i0] + sl * (wl[:i0] - wl[i0])
    sl = (dm[i0 + 3] - dm[i0]) / (wl[i0 + 3] - wl[i0]); dm[:i0] = dm[i0] + sl * (wl[:i0] - wl[i0])
    N = ne - dm
    # D-min = floor + magenta-coupler band (~440) + cyan-coupler band(s) (~500-570)
    def rd(p):
        f0, f1, g1, m1, s1, g2, m2, s2, g3, m3, s3 = p
        return f0 + f1 * (wl - 550) / 100 + g1 * gauss(wl, m1, s1) + g2 * gauss(wl, m2, s2) + g3 * gauss(wl, m3, s3) - dm
    pd = least_squares(rd, [0.2, 0, 0.6, 440, 30, 0.3, 510, 15, 0.35, 560, 20],
                       bounds=([0, -1, 0, 415, 10, 0, 495, 8, 0, 540, 8], [1, 1, 2, 465, 60, 2, 525, 40, 2, 585, 40])).x
    Pm = gauss(wl, pd[3], pd[4]); Pc = pd[5] * gauss(wl, pd[6], pd[7]) + pd[8] * gauss(wl, pd[9], pd[10])
    Pc /= Pc.max()
    def model(p, w=wl, pm=Pm, pc=Pc):
        aY, aM, aC, sY, sM, sC, wY, wM, wC, bM, bC = p
        return aY * shape('Y', w, sY, wY) + aM * shape('M', w, sM, wM) + aC * shape('C', w, sC, wC) - bM * pm - bC * pc
    lam = 0.02
    def res(p):
        prior = lam * np.array([p[3] / 10, p[4] / 10, p[5] / 10, (p[6] - 1) / 0.1, (p[7] - 1) / 0.1, (p[8] - 1) / 0.1])
        return np.concatenate([model(p) - N, prior])
    p0 = [1.2, 1.0, 1.0, 0, 0, 0, 1, 1, 1, 0.2, 0.2]
    fit = least_squares(res, p0, bounds=([0, 0, 0, -25, -25, -10, 0.7, 0.7, 0.7, 0, 0], [3, 3, 3, 25, 25, 30, 1.4, 1.4, 1.4, 1.5, 1.5]))
    p = fit.x
    rms = float(np.sqrt(np.mean((model(p) - N) ** 2)))
    g = np.arange(400, 801, 5.0)
    dyes = {k: [round(float(v), 4) for v in shape(k, g, p[3 + i], p[6 + i])] for i, k in enumerate('YMC')}
    out[name] = {'wl': list(g), 'dyes': dyes, 'params': dict(zip('aY aM aC sY sM sC wY wM wC bM bC'.split(), map(float, p))),
                 'dmin_fit': list(map(float, pd)), 'rms_net': rms}
    print(name, 'rms %.3f' % rms, ' '.join('%s=%.3f' % kv for kv in out[name]['params'].items()))
    # plot
    import matplotlib; matplotlib.use('Agg'); import matplotlib.pyplot as plt
    fg, ax = plt.subplots(figsize=(7, 4))
    ax.plot(wl, N, 'k', lw=2, label='datasheet neutral - D-min')
    ax.plot(wl, model(p), 'k--', label='fit (rms %.3f D)' % rms)
    for i, (k, c) in enumerate((('Y', 'goldenrod'), ('M', 'm'), ('C', 'c'))):
        ax.plot(wl, p[i] * shape(k, wl, p[3 + i], p[6 + i]), c, label=k + ' dye (5219 shape, shift %.0f nm, width x%.2f)' % (p[3 + i], p[6 + i]))
    ax.plot(wl, -p[9] * Pm - p[10] * Pc, 'grey', label='mask coupler consumption')
    ax.set_title(name + ': dye decomposition'); ax.legend(fontsize=7); ax.set_xlabel('nm'); fg.tight_layout()
    fg.savefig('./testdata/v066/ektar/dye_fit_%s.png' % f, dpi=110)
json.dump(out, open('dyes.json', 'w'), indent=1)

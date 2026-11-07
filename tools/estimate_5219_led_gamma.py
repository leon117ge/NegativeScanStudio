#!/usr/bin/env python3
"""Reproduce the conditional 5219/LED midscale model; needs numpy/scipy.

Optional --scan samples a local linear TIFF without storing the image in git.
Numbers from graph digitization are estimates, never scanner calibration.
"""
import argparse
import json
from pathlib import Path

import numpy as np
from scipy.optimize import nnls

ROOT = Path(__file__).resolve().parents[1]
WL = np.arange(400., 801.)
GAMMA = np.array([.4482, .5266, .5096])
PEAKS = np.array([650.1, 539.9, 450.7])
FWHM = np.array([22.2, 38.7, 18.6])


def load():
    a = np.genfromtxt(ROOT / 'colorsci/data/5219_spectral_dye_density.csv',
                      delimiter=',', names=True)
    interp = lambda name: np.interp(WL, a['wavelength_nm'], a[name])
    dyes = np.array([interp(k + '_density') for k in ['cyan', 'magenta', 'yellow']]).T
    neutral, minimum = interp('midscale_neutral_density'), interp('minimum_density_density')
    s = np.genfromtxt(ROOT / 'colorsci/data/status_m_log_products.csv', delimiter=',', names=True)
    # Interpolate LOG products, not linear response; table specifies log products.
    status = np.array([10 ** (np.interp(WL, s['wavelength_nm'], s[k + '_log10_product']) - 5)
                       for k in ['red', 'green', 'blue']])
    return dyes, neutral, minimum, status


def led(peaks=PEAKS, widths=FWHM):
    return np.exp(-4 * np.log(2) * ((WL[None, :] - peaks[:, None]) / widths[:, None]) ** 2)


def density(weights, spectrum):
    return -np.log10((weights @ 10 ** -spectrum) / weights.sum(axis=1))


def jacobian(weights, baseline, dyes):
    transmitted = weights * 10 ** -baseline
    return (transmitted @ dyes) / transmitted.sum(axis=1)[:, None]


def predict(dyes, baseline, status, lights, gamma=GAMMA):
    jm = jacobian(status, baseline, dyes)
    jl = jacobian(lights, baseline, dyes)
    rates = np.linalg.solve(jm, gamma)
    return jl @ rates, jm, jl, rates


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--scan')
    p.add_argument('--output')
    args = p.parse_args()
    dyes, neutral, minimum, status = load()
    baseline = neutral + minimum  # Kodak net neutral is D-min-subtracted.
    lights = led()
    gamma, jm, jl, rates = predict(dyes, baseline, status, lights)
    amounts, _ = nnls(dyes, neutral)
    residual = neutral - dyes @ amounts
    # Check analytic derivative against the actual transmittance integral.
    eps = 1e-5
    for w, j in [(status, jm), (lights, jl)]:
        numerical = np.column_stack([
            (density(w, baseline + eps * dyes[:, k]) -
             density(w, baseline - eps * dyes[:, k])) / (2 * eps)
            for k in range(3)])
        np.testing.assert_allclose(j, numerical, atol=1e-8)
    np.testing.assert_allclose(jm @ rates, GAMMA, atol=1e-12)
    rng = np.random.default_rng(5219)
    variants = []
    # Sensitivity scenarios, NOT samples from a known physical distribution.
    for _ in range(1000):
        knots = np.arange(400, 801, 40)
        perturb = np.column_stack([np.interp(WL, knots, rng.uniform(-.02, .02, len(knots)))
                                   for _ in range(3)])
        g = GAMMA + rng.uniform(-.03, .03, 3)
        source = led(PEAKS + rng.uniform(-2, 2, 3), FWHM * rng.uniform(.9, 1.1, 3))
        variants.append(predict(dyes + perturb, baseline, status, source, g)[0])
    variants = np.array(variants)
    result = {
        'gamma_rgb': gamma.tolist(), 'ratio_to_green': (gamma / gamma[1]).tolist(),
        'status_m_jacobian_rows_RGB_columns_CMY': jm.tolist(),
        'led_jacobian_rows_RGB_columns_CMY': jl.tolist(),
        'dye_amount_rates_per_logH_CMY': rates.tolist(),
        'midscale_dye_amounts_nnls_CMY': amounts.tolist(),
        'midscale_reconstruction_rms_D': float(np.sqrt(np.mean(residual ** 2))),
        'midscale_reconstruction_max_abs_D': float(abs(residual).max()),
        'gamma_with_reconstructed_midscale': predict(dyes, dyes @ amounts + minimum, status, lights)[0].tolist(),
        'gamma_omitting_Dmin': predict(dyes, neutral, status, lights)[0].tolist(),
        'gamma_at_half_midscale': predict(dyes, .5 * neutral + minimum, status, lights)[0].tolist(),
        'gamma_at_double_midscale': predict(dyes, 2 * neutral + minimum, status, lights)[0].tolist(),
        'sensitivity_scenarios_min_max': [variants.min(0).tolist(), variants.max(0).tolist()],
        'sensitivity_scenarios_p05_p95_not_confidence_interval': np.quantile(variants,[.05,.95],axis=0).tolist(),
        'sensitivity_ratio_RG_min_max': [float((variants[:,0]/variants[:,1]).min()),float((variants[:,0]/variants[:,1]).max())],
    }
    if args.scan:
        import tifffile
        scan = tifffile.memmap(args.scan)[::8, ::8].astype(float) / 65535
        def regression(roi):
            px = roi.reshape(-1, 3)
            px = px[np.all((px > .0005) & (px < .98), axis=1)]
            d = -np.log10(px / .5)
            return {'n': len(px), 'DR_on_DG': float(np.polyfit(d[:,1], d[:,0],1)[0]),
                    'DB_on_DG': float(np.polyfit(d[:,1],d[:,2],1)[0])}
        result['scan_whole_every8th_pixel'] = regression(scan)
        result['scan_interior_10pct_crop_every8th_pixel'] = regression(scan[52:-52,78:-78])
    text = json.dumps(result, indent=2) + '\n'
    if args.output:
        Path(args.output).write_text(text)
    print(text)


if __name__ == '__main__':
    main()

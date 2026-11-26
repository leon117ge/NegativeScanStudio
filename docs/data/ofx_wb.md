# OFX T/D white balance density compensation (OFX 0.90.1)

Reproduce with `ofx/tools/generate.py`; 10 nm energy sensitivity integration, Planck visible SPD:

`E_c(K) = Σ 10^logS_c(λ) B_λ(K) Δλ`

`ΔlogH_c = log10[E_c(K)/E_c(K0)] − log10[E_G(K)/E_G(K0)]`

`cast_c = −gamma_c ΔlogH_c`, **added** directly to the D of Density In; it does not change the slope. Equivalent to `base_effective = base × 10^cast`. Green anchor=0; no extra global exposure compensation is given to it.

T: 5219 sensitivity + nominal Status M gamma 0.4482/0.5266/0.5096, K0=3200; follows the v0.89 method and does not apply the canceled 400K design. D: official 5207 sensitivity and midscale gamma, K0=5600 (Kodak nominal 5500, endpoint approximation, see [5207 reading notes](5207_digitization.md)). All scene temperatures, including 5600, are blackbody approximations, **not CIE daylight / measured LED spectra**.

| Film | Scene K | cast R / D | cast G / D | cast B / D |
|---|---:|---:|---:|---:|
| T | 3200 | 0.000000 | 0.000000 | 0.000000 |
| T | 3800 | +0.035046 | 0.000000 | -0.069526 |
| T | 4400 | +0.060535 | 0.000000 | -0.121472 |
| T | 5000 | +0.079811 | 0.000000 | -0.161625 |
| T | 5600 | +0.094824 | 0.000000 | -0.193475 |
| D | 3200 | -0.109197 | 0.000000 | +0.199857 |
| D | 3800 | -0.069027 | 0.000000 | +0.127565 |
| D | 4400 | -0.039687 | 0.000000 | +0.073920 |
| D | 5000 | -0.017411 | 0.000000 | +0.032640 |
| D | 5600 | 0.000000 | 0.000000 | 0.000000 |

The data table keeps full precision in [ofx_wb_presets.json](ofx_wb_presets.json). The model sensitivity is that of the taking emulsion layers, and applying the cast on the scan RGB axes is an approximation; no complete independent layer response, actual scanner QE, or actual spectrum/ECN-2 batch calibration was used, and the new LED gamma was not used to replace the sensitivity→density slope here. It can be used as a starting point and verified with a gray card on the same roll; it is not a guarantee of exact cast removal. Portra/Gold/UltraMax default to D, but **share the 5207 color temperature model**; only their gamma comes from their own official curve readings. The offset is not recomputed when the positive gamma is adjusted by hand; the two are independent.

## OFX 0.90.1: writing Film base directly

The cast fields/Apply button are no longer exposed. Choosing T/D or a temperature immediately writes the Film base sliders:

`base_effective_c = baseline_c × 10^(+cast_c)`

The **plus sign** comes from `D = −log10(pixel/base)`; `base × 10^cast` increases D by exactly cast. Using a minus sign would reverse the compensation. All 10 presets have regression tests against the old DCTL additive cast. This equivalence refers to the density branch and is subject to the same epsilon/clamp limits; the transmittance output now also follows base and is not equivalent to the old cast (the old cast had no effect in that branch).

New instance baseline = 0.5/0.5/0.5; after a successful Load, baseline = the file's film base after the pending camera matrix. A hidden persistent parameter stores the baseline; repeated switching/Load does not multiply onto an already compensated value. Hand-editing the sliders allows fine-tuning; the next preset choice resets that fine-tuning from the baseline. The physical light, scan TIFF pixels and the app film base are all unchanged.

| Film | Scene K | Film base R / G / B (baseline 0.5) |
|---|---:|---|
| T | 3200 | 0.500000 / 0.500000 / 0.500000 |
| T | 3800 | 0.542021 / 0.500000 / 0.426034 |
| T | 4400 | 0.574784 / 0.500000 / 0.378005 |
| T | 5000 | 0.600871 / 0.500000 / 0.344623 |
| T | 5600 | 0.622006 / 0.500000 / 0.320254 |
| D | 3200 | 0.388842 / 0.500000 / 0.792186 |
| D | 3800 | 0.426523 / 0.500000 / 0.670710 |
| D | 4400 | 0.456334 / 0.500000 / 0.592775 |
| D | 5000 | 0.480351 / 0.500000 / 0.539026 |
| D | 5600 | 0.500000 / 0.500000 / 0.500000 |

Full values: [ofx_0901_base_presets.json](ofx_0901_base_presets.json). Stock gamma is still a separate parameter. 500T defaults to T and other stocks default to D; all can be overridden; the scene temperature does not change automatically. Positive Out Film white balance is kept; the original Legacy entry is renamed **500T Base**, and as a visible balance loader writes +1.07/0/+0.67, with None resetting to 0; T/D and gamma do not silently change the balance. The old 500T Base was measured under T5600 + LED gamma; 0.90.2 changed it to the user's eye adjustment, keeping the T5600 table unchanged, and the two can be stacked. [Base record](ofx_film_bases.md).

## 500T Base update in OFX 0.90.2

The T/D film base table (including T5600) is unchanged. Positive Out's 500T Base loader changed from the old R+1.57/G0/B+1.00 to **R+1.40/G0/B+0.80 stops**; selecting it immediately writes the visible balance sliders, still with no hidden stacking. The new values are the user's eye-adjustment decision under the **Gamma scale 1.15 / Exposure 0.90** defaults, **not a new measurement/curve fit**; the earlier re-measurement request was canceled. The old measured record is kept in [ofx_500t_base_measurements.json](ofx_500t_base_measurements.json); for the current table/history see [ofx_film_bases.md](ofx_film_bases.md).

## OFX 0.90.3 user eye-adjustment update

500T Base changed to **R+1.07/G0/B+0.67 stops** (previously +1.40/0/+0.80). The loader and the visible balance defaults for new instances were updated together; this is not a new measurement. The T/D film base table, gamma, Gamma scale 1.15 and Exposure 0.90 are unchanged. Existing projects can re-select 500T Base to load the new values.

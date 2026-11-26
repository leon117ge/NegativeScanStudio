# Midscale gamma estimate for 5219 under RGB Light Air

Research date: 2026-11-07, Asia/Shanghai (UTC+8). **This is a conditional spectral model, not a measured calibration of this roll of film / this camera.**

## Conclusion

Under the assumptions below, the effective density slope (D / log10 H) for a neutral exposure is:

| | R | G | B |
|---|---:|---:|---:|
| LED predicted gamma | 0.4533 | 0.5355 | 0.5266 |
| Relative to G | 0.8464 | 1 | 0.9833 |
| Kodak Status M input slope | 0.4482 | 0.5266 | 0.5096 |

Positive Out `gam_r/g/b = 0.45 / 0.54 / 0.53` can be used as a **starting point, still to be verified, for 500T + this light**; it must not be called an exact device parameter, and it should not be used to directly replace the global defaults for all film stocks. This round only records research; template/UI defaults were not changed and no program was released.

Especially important: the slope predictions for this light and for Status M are close, so **the R/G≈0.71 of 0206 cannot be explained by "650 nm makes red density clearly lower"**. The earlier claims, based only on a whole-frame regression of a night scene, that Gamma R is the root cause of the cyan cast, and the promise that knowing the peaks is enough to compute the exact actual gamma, both went beyond the evidence.

## Sources and raw data

1. Kodak H-1-5219, March 2026 / Revised 3-26, page 4 Spectral Dye Density Curves, page 3 sensitometry:
   https://www.kodak.com/content/pdfs/motion/KODAK-VISION3-500T-5219-7219-technical-information.pdf
   PDF SHA256: `6d037d8090373ee50d10bedfff3df14777a575dd107c50edcafc7ec5862d0972`.
   Kodak only publishes curves and does not directly list the gamma numbers in this report. The Status M gamma was read by the project from the straight-line region of the chart; see `5219_digitization.md`, original estimated error ±0.03.
2. ANSI/ISO 5-3:1995 / ANSI/NAPM IT2.18-1996, Table 4 (printed page 8):
   https://law.resource.org/pub/us/cfr/ibr/001/aimm.it2.18.1996.pdf
   SHA256: `6357992b5a498402a23e9ca60c65e02deb58848fb7bb84f24e5f5011972d2b39`.
   The public Status M **log spectral products**, not a standalone detector QE. The table peak is 5.000; this model takes `10^(table-5)` and must not multiply by illuminant A again. Outside the table, the log product is extended using the slope of the table columns.
   A preview of the 2009 edition was also found, but the preview does not contain the full Table 4, so the 1995/1996 public table is explicitly used here; **no claim is made that the full 2009 table was checked**:
   https://cdn.standards.iteh.ai/samples/52915/fed16a10b2884f89840a0daa8c480143/ISO-5-3-2009.pdf
3. Parameters from the user-provided CuteNew RGB Light Air measurement report: 2026-10-14; peaks 650.1/539.9/450.7 nm; FWHM 22.2/38.7/18.6 nm. The executor in this round received these transcribed parameters; the original image attachment path could not be opened on its filesystem, so **the report images were not independently re-checked**. A Gaussian SPD is assumed, and the dominant wavelength is not taken as the peak.

Numeric transcriptions: `colorsci/data/5219_spectral_dye_density.csv` and `status_m_log_products.csv`. Only this project's numeric transcriptions are stored; Kodak images are not redistributed.

## Curve reading method

`pdfimages -png` extracts the page 4 dye plot: 686×730 pixels (`img-004.png` in this run; the xref number inside the PDF is not used as a stable identifier). PNG SHA256: `78ffcdbe3d5ccb0b47947f0de5ba85d511af3a256ecd5ba85fad0863f7758465`.

Pixel coordinates are counted from the top-left corner: on the horizontal axis x=96 is 400 nm and x=662 is 800 nm; on the vertical axis y=589 is D=0 and y=306 is D=1. Conversion:

```
lambda_nm = 400 + (x - 96) * 400 / 566
D = (589 - y) / 283
```

Every 10 nm, five curves (Cyan/Magenta/Yellow, Midscale Neutral, and the dashed Minimum Density) were identified by hand; curve assignment was corrected with vertical ink checks and an overlay image, and the nearby black pixel within ±4 px of the hand-read position was selected. At end points, gaps in the dashed line, crossings and places covered by text, values were hand-read/interpolated following the trend of neighboring lines; these points must not be treated as instrument data. The CSV stores x, y and the converted density together; the engineering chart-reading error is about 0.01–0.02 D (possibly larger where occluded), not a statistical confidence interval.

The dye curves are peak-normalized, with D-mins subtracted as shown in the chart. Magenta has small negative values in the blue band; they are kept rather than clipped to zero: this is the shape of the net density change given by the chart, and must not be interpreted as negative physical absorption of an isolated dye. Minimum Density is kept separately. Following the chart caption, the neutral curve is interpreted as data after D-min subtraction, and the integration baseline is `midscale_neutral + minimum_density`; the sensitivity result without adding D-min is also reported.

Reproducing the transcription (the original image must be obtained first):

```
python3 tools/digitize_5219_dyes.py /path/to/original-dye-plot.png --overlay /tmp/5219-overlay.png
```

The overlay image was checked wavelength by wavelength and was not committed to the repository.

## Transmittance integration and calibration

Let `d_k(lambda)` be the three normalized net density curves C/M/Y, and `a_k` their amplitude coordinates. Light source:

```
S_c(lambda) = exp[-4 ln(2) ((lambda-peak_c)/FWHM_c)^2]
D_c(a) = -log10[ integral S_c 10^(-D0 - sum a_k d_k) dlambda / integral S_c dlambda ]
J_ck = dD_c/da_k = integral S_c 10^(-Dmid) d_k dlambda / integral S_c 10^(-Dmid) dlambda
```

The calculation uses an equal-step sum over 400–800 nm at 1 nm intervals; density is linearly interpolated, and the Status M **log product** is linearly interpolated. Optical density is not averaged first; the weighting is actually done in transmittance space. The share of Status M input weight outside 400–800 nm, estimated from the table tail slopes, is R=4.58e-7, G=4.49e-13, B=1.32e-7; the Gaussian tails of the LEDs are also negligible. No dye spectra were invented beyond the curves.

An NNLS amplitude fit of the neutral curve to C/M/Y gives `a0 ≈ (0.9014, 1.2429, 1.7040)`; fit RMS=0.0674 D, maximum residual=0.1872 D. **They do not add up exactly**, which suggests the net dye/mask/chart-reading model is limited. The main calculation directly uses the published neutral density spectrum plus D-min as the integration baseline, which is equivalent to treating the residual above as a locally fixed background; it does not pretend that the three dyes fully reconstruct the film.

Solve `q = inverse(J_M) gamma_M` from the local Status M Jacobian:

```
J_M (rows R/G/B, columns C/M/Y, D per unit normalized dye amplitude):
[ 0.85637   0.07119   0.00067 ]
[ 0.12544   0.84649   0.11630 ]
[ 0.03295   0.03678   0.88493 ]

q = da/dlog10(H) = [0.48334, 0.47655, 0.53807]

J_LED (same row and column order):
[ 0.86895   0.06980   0.00009 ]
[ 0.11471   0.86825   0.12328 ]
[ 0.02721  -0.01367   0.96636 ]

gamma_LED = J_LED q = [0.45330, 0.53554, 0.52660]
```

Here q is the **amplitude rate of change** along the neutral exposure direction, not the complete chemical response to three independent layer exposures. Applying J_LED directly in the DCTL as a 3×3 inverse crosstalk matrix for an arbitrary image is not valid; it is a local forward dye→measurement matrix, and after the existing MaskCrosstalk processing the gamma in this report also cannot be used as-is without conversion.

In the red channel, about `0.86895×0.48334=0.4200` D/logH comes from C, and about `0.06980×0.47655=0.0333` more comes from M. Even with narrow bands, dye crosstalk cannot be ignored. The tiny negative M term in blue comes from the retained net density curve and does not represent a negative sensor response.

## Checks and uncertainty

Reproduce (numpy/scipy; tifffile optional):

```
python3 tools/estimate_5219_led_gamma.py --scan /workspace/uploads/0206.tiff --output docs/data/5219_led_gamma_result.json
```

The script checks the analytic Jacobian against central differences (absolute error <1e-8), and verifies that `J_M q` recovers the input gamma (<1e-12). The JSON stores all results without excessive rounding.

Model variant gamma R/G/B:

- NNLS-reconstructed neutral spectrum as baseline: 0.4537 / 0.5380 / 0.5272.
- Without adding D-min: 0.4537 / 0.5349 / 0.5249.
- Half-amplitude neutral density: 0.4530 / 0.5340 / 0.5251.
- Double-amplitude neutral density: 0.4539 / 0.5394 / 0.5293.

1000 fixed-seed sensitivity scenarios: gamma_M each ±0.03; dye curves smoothly perturbed by ±0.02 D each at 40 nm nodes; LED peak each ±2 nm, FWHM each ±10%. These ranges are **artificial stress-test assumptions, not tolerances from the measurement report**. Overall gamma range across scenarios: R 0.419–0.488, G 0.494–0.578, B 0.487–0.566; R/G 0.739–0.969. About ±0.04 around the nominal result can be taken as a rough engineering sensitivity, **not an error guarantee or confidence interval for the actual scanner**.

Unquantified systematic errors may be larger: non-Gaussian LED side peaks, camera QE/optical filters that are not flat within each band, stray light/black level/nonlinearity, a camera matrix or other density-space transform in the TIFF, actual ECN-2 processing/batch, curve representativeness, and exposure-dependent mask/dye shape changes. Here the scan channel weights are assumed to be the SPD; without the full QE, the result cannot be called the "actual camera gamma". Per-layer independent exposure responses were not computed either; Kodak's single neutral characteristic curve is not enough to constrain that complete model.

## Comparison with 0206

The input is the local `0206.tiff`, 4170×6246×3, uint16, used with the linear encoding given by the user. The previously reported whole-frame slopes R/G=0.709, B/G=0.986 did not come with complete ROI/filtering conditions. This time, every 8th pixel was sampled, on the condition that all three channels are within (0.0005, 0.98), with `D=-log10(pixel/0.5)`, OLS with intercept:

- Whole frame n=407681: DR vs DG=0.6900, DB vs DG=0.9782.
- With about 10% removed around the edges n=261250: 0.7035 / 0.9586.

So the old 0.709/0.986 was not reproduced exactly; sampling and ROI change the answer. A fixed film base constant only changes the intercept of a regression with intercept, not the slope.

The spectral prediction R/G=0.846 is clearly higher than these night-scene regressions; B/G=0.983 is close to the old value / this whole-frame value. **The red direction does not support the earlier explanation; the close match in blue does not constitute a calibration verification either**. Different pixels in a night scene have different colors and light sources, and an OLS slope is not the exposure derivative of a neutral gray scale; it cannot be used to back out per-channel film gamma, nor to assert which of film base/light/matrix is definitely right or wrong.

Recommendation: keep measured film base handling separate, and use 0.45/0.54/0.53 as a sourced experimental starting point; the next step must scan multi-step neutral exposures from the same roll, same process and same equipment, and check density linearity, toe/shoulder, crosstalk and bias. Even if the gamma is reasonable, the color balance of the gray point / scene illumination is a separate calibration, and changing gamma does not guarantee removing the cyan cast.

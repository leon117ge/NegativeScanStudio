# OFX film stock gamma presets (v0.90)

These values are the project's **Status M midscale slope estimates** read from official Kodak curves; they are not a per-channel parameter table published by Kodak, and not a calibration per roll/scan light. The default is still the old 0.55/0.635/0.62; choosing a preset fills the sliders, which can still be edited by hand afterwards.

| Preset | R | G | B | WB default |
|---|---:|---:|---:|---|
| Legacy/manual | 0.5500 | 0.6350 | 0.6200 | Keep |
| Vision3 500T 5219 Status M | 0.4482 | 0.5266 | 0.5096 | T |
| 500T + RGB Light Air LED estimate | 0.4500 | 0.5400 | 0.5300 | T |
| Vision3 250D 5207 Status M | 0.4815 | 0.5602 | 0.5417 | D |
| UltraMax 400 Status M | 0.5186 | 0.5363 | 0.6048 | D |
| Portra 400 Status M | 0.5559 | 0.5496 | 0.6400 | D |
| Gold 200 Status M | 0.5507 | 0.5644 | 0.6104 | D |

For the 5219 source, chart-reading method and ±0.03 engineering uncertainty, see [5219_digitization.md](5219_digitization.md). LED 0.45/0.54/0.53 are rounded trial values from the [spectral model](5219_led_gamma.md), not Status M data and not a device measurement. For the 5207 source, the March 2026 edition and hand-read coordinates, see [5207_digitization.md](5207_digitization.md), about ±0.03.

## Still color negative: reading official vector curves

All of the following are daylight exposure, C-41, Status M; the chart reading verified the labels R/G/B as the lower/middle/upper curves. **The vector path indices are valid only for the PDF edition/hash recorded here**.

- **UltraMax 400** — February 2016 E-7023, PDF page 4. https://kodakprofessional.com/sites/default/files/wysiwyg/pro/resources/E7023_max_400.pdf
  - SHA256 `598f44c39c2cd420554d9e7395ce7213e8bf9f1815fbf550dadaeb625826c77d`; RGB path IDs [35, 34, 33].
- **Portra 400** — January 2025 E-4050, revised 1-25, PDF page 4. https://www.kodakprofessional.com/sites/default/files/2025-07/e4050.pdf
  - SHA256 `70b15171673c5d01505f58ace448995ad72acbe5e401d1966563a61ac0712838`; RGB path IDs [36, 35, 34].
- **Gold 200** — June 2023 E-7022, revised 06-23, PDF page 4. https://www.kodakprofessional.com/sites/default/files/wysiwyg/pro/resources/E7022%20Gold%20tech%20sheet.pdf
  - SHA256 `8f20aa3ca762c898591f30479ead35587adf43eecc22a5a49acc48e54745f062`; RGB path IDs [38, 37, 36].

Reproduce: `python tools/digitize_still_gamma.py DIRECTORY` (requires pymupdf and numpy; the directory contains ultramax.pdf / portra.pdf / gold.pdf). Kodak PDFs/images are not redistributed. The original polyline vertices are extracted; Gold's cubic Bezier segments are expanded to 101 points each; the plot frame/axis ticks are calibrated and converted to logH and density. The original vertices and converted results are in `colorsci/data/*_characteristic.csv`; the exact axis coordinates, SHAs, fit residuals/alternative windows are in [ofx_stock_gamma_readings.json](ofx_stock_gamma_readings.json).

The main slope window is logH=-1.5..0.0 (lux seconds, 61 equally spaced points, linear interpolation within the curve, OLS with intercept); it is not a toe/shoulder fit. Sensitivity to different windows:

| Stock/channel | Main window gamma | -2..-0.5 | -1..0.5 |
|---|---:|---:|---:|
| UltraMax 400 R | 0.5186 | 0.5168 | 0.4946 |
| UltraMax 400 G | 0.5363 | 0.5377 | 0.4974 |
| UltraMax 400 B | 0.6048 | 0.6012 | 0.6052 |
| Portra 400 R | 0.5559 | 0.5410 | 0.5675 |
| Portra 400 G | 0.5496 | 0.5528 | 0.5458 |
| Portra 400 B | 0.6400 | 0.6360 | 0.6440 |
| Gold 200 R | 0.5507 | 0.5411 | 0.5047 |
| Gold 200 G | 0.5644 | 0.5685 | 0.5066 |
| Gold 200 B | 0.6104 | 0.6200 | 0.5663 |

Moving the window toward the highlights, Gold R/G and UltraMax G can change by about 0.04–0.06, so "±0.01 numeric precision" alone must not be quoted as the actual uncertainty. The quantization error of the original vector coordinates is far smaller than the systematic errors of shape/window/production process; the engineering estimate for the main window is about ±0.03, and for Gold/UltraMax including the shoulder it can reach ±0.06 or more; **this is not a confidence interval or batch guarantee**. These curves do not describe the actual process differences of each C-41/ECN-2 run. Many decimals in the raw tables are only for reproducibility and do not represent physical precision.

The Film Stock option fills the gamma and defaults to 500T=T and others=D; the user can override T/D. Choosing a stock does not change the unverified spectral crosstalk matrix; it is not assumed that the Status M slope stays accurate after passing through an arbitrary matrix. The new presets do not stack with the old legacy WB; the gray point can still be used independently.

## Visible scale in OFX 0.90.2

The official curve readings and the preset values on this page are all unchanged; nothing was uniformly multiplied by 1.25. Positive Out's Gamma scale sits below Gamma B, defaults to 1.15, and is a scan look/contrast factor chosen by the user. Actual render gamma = visible R/G/B × scale. Exposure defaults to 0.90. They are not a re-measurement of emulsion gamma, and do not update the T/D integration table. Pure formula verification uses scale 1, or gives the reference the same effective gamma. The old 500T Base measurement corresponds to scale 1; the current 0.90.2 Base 1.40/0/.80 is the user's eye adjustment under the new defaults and was not re-measured.

## OFX 0.90.6: Ektar 100

# NegativeScan Studio OFX 0.90.6

- Add Kodak Ektar 100 to Film stock gamma preset. Selecting it writes R **0.580572786**, G **0.579564388**, B **0.663740443** into the visible gamma sliders and defaults Film type to **D**; manual T/D override remains available.
- Gamma is a project estimate from Kodak E-4046 (January 2025, revised 1/25), page 4, daylight Status M characteristic vector curves; not Kodak-tabulated channel constants. Use the same 61-point, logH -1.5..0, interpolated OLS method as the other still stocks. See docs/data/ofx_stock_gamma_readings.json for full source, PDF SHA-256, axes, residuals and alternate fit windows.
- No Ektar Base preset is added: the existing table has only the calibrated/eye-tuned 500T Base, with no uncalibrated placeholders. Existing Base, gamma presets, Gamma scale **1.15**, Exposure **0.90**, and CPU/CUDA/OpenCL kernels are unchanged.
- Plugin-only release; scanning app stays **v0.91**.


PDF SHA-256: `38a41d47ad006cd563793033f83c8f9ef6cdd3c8300695c31facfc71d368f8db`. RGB paths: 33/32/31. Axis calibration and alternative windows are recorded in the JSON; raw vertices in `colorsci/data/ektar_characteristic.csv`. Estimated engineering uncertainty about +/-0.03, not a confidence interval or batch guarantee. D illumination still uses the existing 5207 approximation, not Ektar spectral calibration.

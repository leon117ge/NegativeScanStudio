# NegativeScan Studio OFX 0.90.12 - Windows x64 (local test build)

## Installation
Quit Resolve, back up your project, and remove the previous bundle. Copy the entire `NegativeScanStudio.ofx.bundle` into `C:\Program Files\Common Files\OFX\Plugins\` (administrator access may be needed). Restart Resolve and find **NegativeScan Studio OFX 0.90.12** under OpenFX > NegativeScan Studio. Do not install duplicate bundles.

Auto uses CUDA or OpenCL buffers when offered by the host. CPU fallback is available. No extra CUDA/OpenCL DLL is bundled or required for CPU use. Keep the whole bundle together. Scanning app binaries are unchanged.

## Controls
All UI labels, statuses, and prompts are in English. The inversion math and all earlier numerical defaults are unchanged from 0.90.6; 0.90.12 adds the optional Tone stage (on by default) after the inversion.

1. **Camera calibration**
   - **X-T3**: Uses the pending X-T3 matrix from loaded `nss.scan/1` metadata, or falls back to the default X-T3 trichrome matrix (`DefaultXT3Trichrome`).
   - **Monochrome (MV-CS240-90UM)**: Applies the fixed neutral-preserving chart matrix fitted from `chart_hik2` against SoftLoaf transmission target No. 000004 (mean ΔE2000 ~2.16; exact row sums = 1.0 so neutral 0.5/0.5/0.5 film bases remain invariant).
   - **Off** (default): True bypass (`camera_on = 0`). Metadata with `camera_applied: true` also keeps calibration Off to avoid double-applying a baked matrix.
   - **Limitation**: Both chart matrices represent camera + narrow-band LED color mapping on a positive Ektachrome transmission target (SoftLoaf No. 000004). Positive-chart mapping is **not** a universal negative dye crosstalk correction and still requires validation on real negative film scans.

2. **Film base**
   - Defaults to **0.50 / 0.50 / 0.50** (and continues auto-reading Resolve 20 source metadata when available).
   - **TIFF or JSON path**, **Load film base from metadata** button, and **Film base status** readout load `film_base` (and any pending `camera_matrix`) without double-transforming `after_profile` references. Selecting Monochrome preserves Monochrome calibration when metadata carries no embedded matrix.

3. **Film & Gamma**
   - **Film stock**: Legacy defaults / manual, Vision3 500T 5219 Status M, Vision3 500T LED estimate, Vision3 250D 5207, UltraMax 400, Portra 400, Gold 200, Ektar 100.
   - **Film balance**: T - tungsten / D - daylight (auto-suggested by stock, manually overridable).
   - **Scene illuminant**: 3200 K – 5600 K Planck approximation.
   - **Exposure**: Default **1.0** stops (`-6.0` to `6.0`).
   - **Gamma scale**: Default **1.0**; scales the three hidden film-stock gammas together (`0.25` to `2.0`).
   - **Contrast**: Off / Low / Medium / High (default **Off**).
   - **Output**: Rec709 / DaVinci Intermediate / Cineon / ACEScct / Linear (default **Cineon**).

4. **Color balance**
   - **Red balance** (default **1.07**), **Green balance** (default **0.00**), **Blue balance** (default **0.67**) for manual channel trim. Changing film stock does not silently zero these sliders.
   - **Balance preset**: `None` plus eight fixed slots. Choosing a filled slot writes its saved R/G/B values into the three sliders above.
   - **Preset name**: English (ASCII) name used when saving. The same name overwrites that preset.
   - **Save** / **Delete**: Save stores the current slider values in `%APPDATA%\NegativeScanStudio\balance_presets.json` (shared across all projects; `NSS_BALANCE_PRESETS` can override the path). Delete removes the selected slot. A missing or corrupt file is treated as empty.
   - **Preset status**: Lists saved slot names, or a short result after Save/Delete. Use this list if the host does not refresh the dropdown labels.

5. **Tone** (new in 0.90.12, on by default)
   - **Tone preset**: `Soft cinematic` (default), `Gentle`, `Strong roll-off`, `Night practicals`, `Off / Classic 0.90.11`, `Custom`. Choosing a preset sets the controls below; editing a control shows `Custom`.
   - **Highlight shoulder**, **Shoulder strength**, **Shoulder start (stops)**, **Shoulder white (stops)**: soft roll-off of bright areas. One gain is applied to R, G and B, so colors keep their hue and nothing clips.
   - **Shadow toe**, **Toe strength**, **Toe start (stops)**, **Toe black (stops)**: lifts deep shadows to a soft neutral floor. The same small amount is added to R, G and B, so hue is kept and the deepest shadows desaturate a little.
   - Stops are relative to 18% gray after Exposure and balance. `Off / Classic 0.90.11` renders exactly like 0.90.11. Works with every Output setting; it is designed for Cineon into a Kodak 2383 LUT.

| Preset | Shoulder on / strength / start / white (stops) | Toe on / strength / start / black (stops) |
|---|---|---|
| **Soft cinematic** (default) | on / 1.00 / +0.75 / +2.20 | on / 1.00 / -2.00 / -4.00 |
| Gentle | on / 0.60 / +1.00 / +2.60 | on / 0.60 / -2.00 / -3.80 |
| Strong roll-off | on / 1.00 / +0.50 / +1.80 | on / 1.00 / -1.75 / -3.60 |
| Night practicals | on / 1.00 / +1.00 / +2.40 | on / 0.50 / -2.50 / -4.00 |
| Off / Classic 0.90.11 | off | off |

Technical parameters (18 matrix coefficients, camera strength, individual channel gammas, Density In constants, black subtraction, white code, gray-pick handle, pipeline stage, and render engine selection) are hidden via OFX `kOfxParamPropSecret` with all parameter IDs and defaults preserved for existing project compatibility.

## Verification and limitations
Native CPU math/parity, Tone math (off = 0.90.11 bit for bit, ramps, hue, no clipping), mock-host lifecycle/UI-visibility/camera-dropdown/preset/tone-preset/metadata/overlay, PoCL OpenCL execution, CUDA Driver contract and offline PTX assembly checks pass in `ofx/test.sh`. Real Windows Resolve, physical NVIDIA/GPU execution, host UI inspection, and real film negative scan acceptance remain untested.

[Plugin changelog](CHANGELOG.md) | [Gamma provenance](../docs/data/ofx_stock_gamma.md) | [Implementation and validation](../docs/OFX.md)

# NegativeScan Studio OFX 0.90.9 - Windows x64 (local test build)

## Installation
Quit Resolve, back up your project, and remove the previous bundle. Copy the entire `NegativeScanStudio.ofx.bundle` into `C:\Program Files\Common Files\OFX\Plugins\` (administrator access may be needed). Restart Resolve and find **NegativeScan Studio OFX 0.90.9** under OpenFX > NegativeScan Studio. Do not install duplicate bundles.

Auto uses CUDA or OpenCL buffers when offered by the host. CPU fallback is available. No extra CUDA/OpenCL DLL is bundled or required for CPU use. Keep the whole bundle together. Scanning app binaries are unchanged.

## Controls
All UI labels, statuses, and prompts are in English. Core rendering math, computation order, CPU/OpenCL/CUDA kernels, and all existing numerical defaults are unchanged from 0.90.6.

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
   - **Exposure**: Default **0.90** stops (`-6.0` to `6.0`).
   - **Contrast**: Off / Low / Medium / High (default **Off**).
   - **Output**: Rec709 / DaVinci Intermediate / Cineon / ACEScct / Linear (default **Cineon**).

4. **Color balance**
   - **Red balance** (default **1.07**), **Green balance** (default **0.00**), **Blue balance** (default **0.67**) for manual channel trim. Changing film stock does not silently zero these sliders.

Technical parameters (18 matrix coefficients, camera strength, individual channel gammas, Gamma scale 1.15, Density In constants, black subtraction, white code, gray-pick handle, pipeline stage, and render engine selection) are hidden via OFX `kOfxParamPropSecret` with all parameter IDs and defaults preserved for existing project compatibility.

## Verification and limitations
Native CPU math/parity, mock-host lifecycle/UI-visibility/camera-dropdown/preset/metadata/overlay, PoCL OpenCL execution, CUDA Driver contract and offline PTX assembly checks pass in `ofx/test.sh`. Real Windows Resolve, physical NVIDIA/GPU execution, host UI inspection, and real film negative scan acceptance remain untested.

[Plugin changelog](CHANGELOG.md) | [Gamma provenance](../docs/data/ofx_stock_gamma.md) | [Implementation and validation](../docs/OFX.md)

# NegativeScan Studio OFX 0.90.6 - Windows x64

## Installation
Quit Resolve, back up your project, and remove the previous bundle. Copy the entire `NegativeScanStudio.ofx.bundle` into `C:\Program Files\Common Files\OFX\Plugins\` (administrator access may be needed). Restart Resolve and find **NegativeScan Studio OFX 0.90.6** under OpenFX > NegativeScan Studio. Do not install duplicate bundles.

Auto uses CUDA or OpenCL buffers when offered by the host. CPU fallback is available. No extra CUDA/OpenCL DLL is bundled or required for CPU use. Keep the whole bundle together. App v0.91 is unchanged.

## Controls
- Film stock gamma preset immediately writes visible Gamma R/G/B. Ektar 100 defaults to daylight D, like 250D, UltraMax 400, Portra 400 and Gold 200. Both 500T options default to T. Manual T/D and gamma overrides remain available. Legacy/manual keeps its original meaning.
- Gamma scale is directly below Gamma B, default **1.15**. Render gamma equals visible RGB gamma multiplied by scale. Positive Out Exposure defaults to **0.90**. Existing stock values are unchanged.
- Film white balance is a visible-slider loader: **None** resets balance to zero; **500T Base** writes **+1.07 / 0 / +0.67 stops**. This is user eye-tuned, not a universal Kodak calibration. Uncalibrated stocks have no placeholder Base; Ektar therefore has no Base entry yet.
- Stock, T/D illumination and Base balance are independent. T/D uses the loaded/default reference without compounding previous results. Daylight still stocks share the existing 5207 illuminant approximation.
- Load scan metadata from TIFF ImageDescription or JSON. App v0.91 after-profile leveling references are not camera-transformed twice; unmarked older metadata retains its previous interpretation. Lamp drive is not film-base RGB. Old incorrect TIFF references require a new scan or manual correction.
- Viewer gray / white pick: arm **Pick gray on viewer**, enable Resolve viewer **Open FX Overlay / FX**, then click or drag a neutral patch. The crosshair and sample box show the target. The first pick visibly resets Base balance; manual fine tuning remains available. Host handle and Source-patch sampling are retained.

## Ektar gamma source
- Add Kodak Ektar 100 to Film stock gamma preset. Selecting it writes R **0.580572786**, G **0.579564388**, B **0.663740443** into the visible gamma sliders and defaults Film type to **D**; manual T/D override remains available.
- Gamma is a project estimate from Kodak E-4046 (January 2025, revised 1/25), page 4, daylight Status M characteristic vector curves; not Kodak-tabulated channel constants. Use the same 61-point, logH -1.5..0, interpolated OLS method as the other still stocks. See docs/data/ofx_stock_gamma_readings.json for full source, PDF SHA-256, axes, residuals and alternate fit windows.
- No Ektar Base preset is added: the existing table has only the calibrated/eye-tuned 500T Base, with no uncalibrated placeholders. Existing Base, gamma presets, Gamma scale **1.15**, Exposure **0.90**, and CPU/CUDA/OpenCL kernels are unchanged.
- Plugin-only release; scanning app stays **v0.91**.


## Verification and limitations
Native CPU, mock-host lifecycle/render/presets/metadata/overlay, CUDA Driver contract and offline PTX assembly checks are available in `ofx/test.sh`. Actual Windows Resolve, a physical GPU, PTX execution, host GPU negotiation and real overlay appearance/input remain untested. Rendering kernels are unchanged from 0.90.5. Check real Ektar scans before production use.

[Plugin changelog](CHANGELOG.md) | [Gamma provenance](../docs/data/ofx_stock_gamma.md) | [Implementation and validation](../docs/OFX.md)

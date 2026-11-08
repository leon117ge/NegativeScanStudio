# NegativeScan Studio OFX — Changelog

Current plugin: **0.90.6** · [Plugin release](https://github.com/leon117ge/NegativeScanStudio/releases/tag/ofx-v0.90.6) · [Scanning app changelog](../docs/CHANGELOG.md) · [Versioning policy](../docs/VERSIONING.md)

Plugin releases are independent from the Windows scanning app.

## 0.90.6 — Ektar 100

- Add Kodak Ektar 100 to Film stock gamma preset. Selecting it writes R **0.580572786**, G **0.579564388**, B **0.663740443** into the visible gamma sliders and defaults Film type to **D**; manual T/D override remains available.
- Gamma is a project estimate from Kodak E-4046 (January 2025, revised 1/25), page 4, daylight Status M characteristic vector curves; not Kodak-tabulated channel constants. Use the same 61-point, logH -1.5..0, interpolated OLS method as the other still stocks. See docs/data/ofx_stock_gamma_readings.json for full source, PDF SHA-256, axes, residuals and alternate fit windows.
- No Ektar Base preset is added: the existing table has only the calibrated/eye-tuned 500T Base, with no uncalibrated placeholders. Existing Base, gamma presets, Gamma scale **1.15**, Exposure **0.90**, and CPU/CUDA/OpenCL kernels are unchanged.
- Plugin-only release; scanning app stays **v0.91**.

## Validation
Native CPU math/parity, mock OFX lifecycle/render/preset/metadata/overlay, CUDA Driver contract and offline PTX assembly checks. Real Windows Resolve, physical GPU/PTX execution, host GPU negotiation and viewer behavior remain untested. Ektar rendering needs user acceptance against real scans; curve extraction is not per-roll calibration.

Source: https://www.kodakprofessional.com/sites/default/files/2025-07/e4046.pdf

## 0.90.5 — app leveling metadata coordinates

- Support app v0.91 `film_base_space: "after_profile"`: load the saved Light-page
  result without applying the camera matrix to it again. TIFF pixels still get
  their pending camera correction. Unmarked v0.90 metadata keeps its old meaning.
- Keep lamp drive separate from Film base. Visible T/D presets continue to fold
  once into the loaded reference (`base * 10^offset`), never a hard-coded 0.5.
- Regression: actual loader actions with mock OFX suites, all ten T/D presets,
  repeated loads, app-produced white/trichrome TIFFs, and legacy metadata.
- No rendering algorithm or GPU kernel changes. Real Windows Resolve/GPU and
  the owner's historical TIFF were not available for acceptance testing.

## 0.90.4 — direct viewer gray picking

- Add OFX OverlayInteractV1 with runtime-loaded OpenGL1.x: cyan/black crosshair and sample box, canonical click/drag, redraw and focus/undo lifecycle. Preserve host GL state and projection.
- Move Viewer gray / white pick first and request it open. Arm **Pick gray on viewer**, select **Open FX Overlay / FX** in Resolve’s viewer, then click/drag a neutral patch. First click enables Auto gray and visibly resets Base balance; subsequent picks retain manual fine tuning. Disarm to prevent accidental re-picking.
- Retain canonical XYAbsolute and host-native handle fallback; no GPU framebuffer sampling. Existing Source-patch sampling works with CPU/CUDA/OpenCL. All numeric defaults/app EXE remain unchanged.
- Add host-side overlay click/drag/lifecycle and mocked GL geometry/state tests, including zoom/PAR/render-scale. Actual Windows Resolve/OpenGL appearance, input dispatch and physical GPU execution remain unverified.

## 0.90.3 — 500T Base eye-tuned patch

- Change only 500T Base loader and new-instance visible balance defaults to **R +1.07 / G 0 / B +0.67 stops** (was +1.40/0/+0.80), as specified by the user. No new measurement is claimed. Existing saved slider values remain; reselect the preset to load the update.
- Gamma presets, Gamma scale1.15, Exposure0.90, T/D tables, CPU/GPU algorithms and app v0.90 EXE are unchanged. CUDA/real Resolve validation limits remain.

## 0.90.2 — CUDA and render setup prerelease

- Keep all stock gamma preset values unchanged. Add visible Gamma scale directly below Gamma B in Positive Out, default **1.15**. Change the plugin Exposure default to **0.90**. These defaults intentionally change rendering; app/DCTL source stays unchanged. The proposed automatic ×1.25 presets are not shipped.
- Change 500T Base from +1.57/0/+1.00 to **+1.40/0/+0.80 stops**, loaded into visible balance sliders. The user eye-tuned this under Gamma scale1.15/Exposure0.90 defaults. Re-measurement was cancelled; retain the old measured record and T5600 offsets.
- Add runtime-loaded CUDA Driver API with embedded PTX, per-host-context JIT/module/function cache, host stream support and no full-frame CPU copy. Optional gray picking downloads only its patch; normal stream rendering remains asynchronous.
- Cache OpenCL programs and kernels per context/device. Reuse exact Prepared constants, throttle same-file metadata mtime checks to250ms, and force immediate reload on Load.
- Request render-window image regions, expand only for enabled gray picking, retain tiles/multi-resolution and disable host frame splitting to avoid nested CPU threading.
- Add actual-renderer/driver diagnostics. CUDA PTX assembles offline and Driver contracts/native CUDA-source math pass tests, but real CUDA/Windows Resolve execution and FPS remain unverified. No additional CPU speedup is claimed beyond measured noise; see [validation and benchmark](../docs/OFX.md).
- App remains v0.90. This release contains only the plugin ZIP.

## 0.90.1 — plugin-only prerelease

- Remove the mask/crosstalk group and stage. Keep camera calibration.
- Merge stock gamma, T/D, illuminant and film-base controls. Presets fill visible sliders, default base is .5/.5/.5, and metadata provides a baseline without repeated compensation.
- Keep Film white balance as a loader. 500T Base writes +1.57/0/+1.00 into visible RGB balance sliders. None resets them to zero. Remove hidden duplicate gains and the old white-code slope. Keep T5600 offsets unchanged.
- Add a host-native viewer gray/white point and sample radius. Sample Source through Density In. OpenCL downloads only the small patch for this optional feature.
- Harden TIFF/JSON loading with short status messages, Unicode/quoted/file URL paths, classic/BigTIFF in either byte order and 64-bit offsets. Read IFD/description without pixels. The old 0206 scan has no tag270.
- Add runtime OpenCL buffers, CPU fallback, per-frame constants and up to eight CPU threads. The matched full-frame Linux CPU benchmark measures 3779.94 to 331.55ms median, not Resolve FPS.
- Pass CPU/metadata/preset tests and actual PoCL CPU OpenCL execution. New 0.90.1 behavior remains unverified in real Windows Resolve or on a physical GPU. See [implementation and verification](../docs/OFX.md).

## 0.90 — initial CPU preview (historical)

- First Windows x64 combined CPU OFX: camera calibration → Density In → mask/crosstalk → Positive Out, with original DCTL defaults.
- T/D illuminant and Kodak stock-gamma presets; TIFF/JSON loading and optional Resolve source-path probing; host-free lifecycle and independent Go math checks.
- Initially shipped with the [app v0.90 release](https://github.com/leon117ge/NegativeScanStudio/releases/tag/v0.90), before independent plugin tags. Its historical ZIP remains there; the current plugin is the separate 0.90.1 release.
- The mask/crosstalk stage and hidden legacy-WB behavior described here were removed/replaced in 0.90.1. See [current usage](README.md), not this historical entry, for installation and parameters.

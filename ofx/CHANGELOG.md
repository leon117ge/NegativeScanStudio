# NegativeScan Studio OFX — Changelog

Current local test plugin: **0.90.12** (not pushed/released) · Last published tag: **0.90.6** ([Plugin release](https://github.com/leon117ge/NegativeScanStudio/releases/tag/ofx-v0.90.6)) · [Scanning app changelog](../docs/CHANGELOG.md) · [Versioning policy](../docs/VERSIONING.md)

Plugin releases are independent from the Windows scanning app.


## 0.90.12 — Tone: highlight shoulder and shadow toe (local test build)

- New open **Tone** group (English only) with `Tone preset`, `Highlight shoulder` (on/off), `Shoulder strength`, `Shoulder start (stops)`, `Shoulder white (stops)`, `Shadow toe` (on/off), `Toe strength`, `Toe start (stops)` and `Toe black (stops)`. Choosing a preset writes the eight controls; editing any of them switches the menu to `Custom`.
- **Default is ON** with `Soft cinematic`, so new instances render soft, non-clipping highlights and lifted, slightly desaturated blacks. Choose `Off / Classic 0.90.11` (or switch both off) for output that is bit-identical to 0.90.11. Existing projects that were saved without these parameters get the new default when opened.
- Presets:

| Preset | Shoulder on / strength / start / white (stops) | Toe on / strength / start / black (stops) |
|---|---|---|
| **Soft cinematic** (default) | on / 1.00 / +0.75 / +2.20 | on / 1.00 / -2.00 / -4.00 |
| Gentle | on / 0.60 / +1.00 / +2.60 | on / 0.60 / -2.00 / -3.80 |
| Strong roll-off | on / 1.00 / +0.50 / +1.80 | on / 1.00 / -1.75 / -3.60 |
| Night practicals | on / 1.00 / +1.00 / +2.40 | on / 0.50 / -2.50 / -4.00 |
| Off / Classic 0.90.11 | off | off |

- Tuned offline on four frames (Hikrobot mono trichrome HDR 0280, X-T3 0206, X-T3 500T F2 and F5) through the repo `Kodak_2383_D65` LUT toward the Silbersalz35 graded reference (Y p0.5/p50/p99.9 = 14/133/198, no clipping). With a per-frame gray-world balance, `Soft cinematic` gives top 202/205/205/209, black 19/25/21/19, mids 137/137/127/100 (0.90.11: top 244/247/235/246, mids 147/137/127/100) and 0% pixels above 250. See `docs/OFX.md`.
- Math (identical in CPU, OpenCL and CUDA; shared `render_math.h`):

- Where: on the scene-linear positive of the Positive stage, after the per-channel inversion and the optional `Contrast` curve, and before the output encoding. The Density In stage and transmittance output are never toned. Stops are `log2(value / 0.18)`, so they move with `Exposure` and the balances.
- Shoulder (highlights): `n = mean(R,G,B)`, `x = log2(n/0.18)`. Above `start` (`xs`), `f = xs + h*(1 - exp(-(x - xs)/h))` with `h = white - xs` (at least 0.05), then `y = x + strength*(f - x)`. R, G and B are multiplied by the same gain `2^(y - x)`, so R:G:B ratios and hue are unchanged. At full strength the mean never goes above `white`, so with the default preset the brightest channel is at most about `+3.8` stops (3x the mean; Cineon about 800 of 1023): nothing clips in Cineon or in a 0..1 LUT input.
- Toe (shadows): `m = min(R,G,B)` after the shoulder, `x = log2(m/0.18)`. Below `start` (`xt`, never above the shoulder start), `g = xt - r*(1 - exp(-(xt - x)/r))` with `r = xt - black` (at least 0.05), and the same offset `strength*(0.18*2^g - m)` is added to R, G and B. Hue (HSV) is unchanged, deep shadows desaturate slightly, and pure black lifts to the neutral floor `strength*0.18*2^black` (default 0.0113 linear, Cineon about 187 of 1023). The darkest channel is used, not the mean, because an offset from the mean makes the weakest channel of saturated dark colors go backwards; with the darkest channel every channel is monotonic along any hue line.
- Both curves have slope 1 at their start point (C1, no kink), are monotonic, and leave everything between the toe start and the shoulder start untouched.
- Output encodings: the tone runs on scene-linear values, so Rec709, DaVinci Intermediate, Cineon, ACEScct and Linear all receive the same toned linear values through their unchanged encoders. With Linear output the shoulder keeps values below `0.18*2^white` times the pixel's max/mean ratio (at most 3x); with Rec709 values above 1.0 are still possible for very bright saturated pixels, exactly as in 0.90.11.
- Density clamp: the 1.856 density cap (`Clamp output to 0 to 1`) is unchanged. No pixel of the four test frames reaches it (the brightest Cineon values come from the positive curve, not the cap), so softening it was not needed.
- Off: when both switches are off (or both strengths are 0) the kernels take the 0.90.11 per-channel code path, bit for bit (checked against a 0.90.11 build on 3.1 million values: identical).
- Constants grew from 40 to 48 floats (`Prepared`, OpenCL `__constant`, CUDA `NssBlock`). PTX regenerated with NVRTC 12.0.140 (`docs/data/ofx_0902_cuda_build.json`). Presets live in `docs/data/ofx_tone_presets.json`; `ofx/tools/generate.py` writes `ofx/src/tone_params.hpp` and the parameter plumbing from it.
- CPU fallback cost on a real 500T frame (0206, single thread): 73 ns/pixel off, 100 ns/pixel with `Soft cinematic`. GPU kernels add the same few operations per pixel; no real GPU timing was done.
- Tests: new `ofx/tests/tone_test.cpp` (off = 0.90.11 path bit for bit for all outputs and contrasts, kernel math vs an independent stops-based reference, 16-bit ramps, hue drift on saturated patches, no Cineon clipping, C1 at both pivots, black floor); host test (version 9012, defaults, visible controls, preset application, `Custom`); PoCL OpenCL parity for every preset and output; mocked CUDA contract with the default tone; Go parity with Tone off.
- Known limit: on a gray input ramp the channels drift apart in the densest highlights (the stock gammas differ per channel), and because the shoulder keeps R:G:B, the weakest channel can dip by up to 0.15 stops (`Soft cinematic`; 0.20 for `Strong roll-off`) above about +5 stops. Luminance stays monotonic.
- Plugin label is **NegativeScan Studio OFX 0.90.12**; version minor is 9012. No other defaults, parameters or inversion math changed. Real Resolve and real GPU testing are still pending.

## 0.90.11 — Exposure default 1.0 (local test build)

- Change the `Exposure` default from `0.90` to **1.0** at the user's request (with Gamma scale 1.0 from 0.90.9, new instances now match the original 0.90.1 defaults). Balances stay `1.07 / 0 / 0.67`. Existing Resolve projects keep their saved values. Core math unchanged.
## 0.90.10 — User-saved Color balance presets (local test build)

- UI only in **Color balance**: add `Balance preset` (fixed `None | Slot 1..8`), `Preset name`, `Save`, `Delete`, and a read-only `Preset status` line under the Red/Green/Blue balance sliders. Saving stores the current visible `positive_bal_r/g/b` values under that English (ASCII) name; the same name overwrites. Delete removes the selected slot. Selecting a slot writes only those three sliders; core render math, kernels, and performance are unchanged. Existing parameter IDs and defaults are preserved; new parameters are appended.
- Presets persist in `%APPDATA%\NegativeScanStudio\balance_presets.json` on Windows (directory is created as needed), so they are shared across all Resolve projects. `NSS_BALANCE_PRESETS` overrides the path (used by tests). A missing or corrupt file is treated as empty and does not fail the plugin.
- Resolve / OFX hosts do not reliably rebuild `kOfxParamTypeChoice` menus after describe, and the specification treats post-describe gaps as an error, so this build uses eight fixed slots instead of a dynamic list. The plugin still tries to update slot labels (`Slot N: name`) on the instance; if the host keeps the original `Slot N` labels, `Preset status` lists the saved names.
- Plugin label is **NegativeScan Studio OFX 0.90.10**; version minor is 9010. Gamma scale default remains 1.0, Exposure 0.90, balances 1.07 / 0 / 0.67.

## 0.90.9 — Gamma scale default 1.0 (local test build)

- Change the `Gamma scale` default from `1.15` to **1.0** at the user's request, so new instances use the raw Film stock preset gammas (Kodak curve fits). Exposure stays `0.90`; balances stay `1.07 / 0 / 0.67` (eye-tuned earlier under scale 1.15, so they may need re-tuning). Existing Resolve projects keep their saved slider value. Plugin version minor is now 9009 (0.90.7 and 0.90.8 still reported 9007). Core math unchanged.

## 0.90.8 — Gamma scale visible again (local test build)

- UI only: `Gamma scale` (default `1.15`) is visible again under `Exposure` in **Film & Gamma**; it scales all three channel gammas together and works with Exposure. Individual R/G/B gammas stay hidden and are still set by the Film stock preset. 14 visible controls. Core math and performance unchanged.

## 0.90.7 — Simplified English UI and Camera calibration dropdown (local test build)

- **UI simplification only; core math, computation order, and CPU/OpenCL/CUDA performance implementations are unchanged.**
- Consolidate the inspector to 13 visible English-only controls across 4 open groups:
  - **Camera calibration**: `Camera calibration` (`X-T3 | Monochrome (MV-CS240-90UM) | Off`, default `Off`).
  - **Film base**: `TIFF or JSON path`, `Load film base from metadata` button, and `Film base status` readout (default `0.50 / 0.50 / 0.50`).
  - **Film & Gamma**: `Film stock`, `Film balance`, `Scene illuminant`, `Exposure` (default `0.90`), `Contrast` (default `Off`), and `Output` (default `Cineon`).
  - **Color balance**: `Red balance` (`1.07`), `Green balance` (`0.00`), `Blue balance` (`0.67`).
- Hide 42 technical controls (18 matrix elements, camera strength/neutralise, individual R/G/B gammas, Gamma scale `1.15`, Film white balance loader, Density In constants, black subtraction, white code, viewer gray pick, pipeline stage, and render engine selector) using OFX `kOfxParamPropSecret` while preserving every parameter ID and numerical default for existing Resolve project compatibility.
- Add `Monochrome (MV-CS240-90UM)` fixed neutral-preserving chart matrix (`[0.942661, 0.195781, -0.138442; -0.056435, 1.036487, 0.019948; 0.014258, -0.089212, 1.074954]`, exact unit row sums `[1, 1, 1]`, SoftLoaf No. 000004 `chart_hik2` fit, mean ΔE2000 ~2.16). `X-T3` uses embedded metadata or `DefaultXT3Trichrome`; `Off` is a true bypass; `camera_applied: true` metadata prevents double-application.
- **Limitation**: Positive-chart color mapping (Ektachrome target SoftLoaf No. 000004) is **not** a universal color-negative dye crosstalk correction.
- Validation: `ofx/test.sh` (CPU/Go parity, mock OFX lifecycle/UI/camera/metadata/overlay, CUDA Driver contract, classic/BigTIFF metadata) and PoCL OpenCL parity (`NSS_TEST_OPENCL=1`). Real Windows Resolve UI/GPU execution remains unverified until tested on a host machine.

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

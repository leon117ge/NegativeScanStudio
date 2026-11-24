# NegativeScan Studio OFX 0.90.14

Current plugin is **0.90.14**. 0.90.12 added the optional Tone stage (highlight shoulder and shadow toe, on by default); 0.90.14 moves highlight/shadow desaturation and highlight cream into a separate Tone Color group. Inversion math and the earlier numeric defaults are unchanged except where a later changelog says so (Gamma scale and Exposure returned to 1.0 in 0.90.9 / 0.90.11). Real Windows Resolve and physical GPU acceptance remain outstanding. [Installation](../ofx/README.md).

## 0.90.14 Tone Color group and stronger cream

Request: switches first, then a preset menu, then sliders, with the color controls fully separate from shoulder/toe; and with Highlight desaturation at 1 the highlights only looked whiter, not cream.

- UI: the color controls are a new, separate **Tone Color** group. Each group is laid out as switches, then its own preset menu, then the sliders:
  - **Tone**: `Highlight shoulder`, `Shadow toe` (switches) -> `Tone preset` -> `Shoulder strength`, `Shoulder start (stops)`, `Shoulder white (stops)`, `Toe strength`, `Toe start (stops)`, `Toe black (stops)`.
  - **Tone Color**: `Highlight desaturation`, `Highlight cream`, `Shadow desaturation` (switches, new) -> `Tone color preset` (new) -> `Highlight desaturation amount`, `Highlight color start (stops)`, `Highlight cream amount`, `Shadow desaturation amount`, `Shadow color start (stops)`.
- `Tone preset` loads only shoulder/toe; `Tone color preset` loads only the color group. Editing a switch or slider sets only its own group's menu to `Custom`. Switch off renders bit for bit like amount 0.
- Tone color presets: `Soft cinematic` (default; 0.60 / +0.75 / 1.00, shadow 0.70 / -2.00), `Gentle` (0.40 / +1.00 / 0.60, 0.40 / -2.25), `Strong` (0.75 / +0.50 / 1.00, 0.80 / -1.75), `Night practicals` (0.50 / +1.00 / 0.80, 0.50 / -2.50), `Off` (all three switches off), `Custom`. Tone presets unchanged except that they no longer carry color values.
- Existing 0.90.12/0.90.13 projects: the new switches default ON, the old single Tone preset index now drives shoulder/toe only, and the new color menu starts at `Soft cinematic`.
- Cream now runs **after** highlight desaturation and is measured on the desaturated pixel, so desaturation can no longer cancel it: with `Highlight desaturation amount` 1 the top highlights become warm cream with cream on and exactly neutral (R = G = B) with cream off.
- Cream is stronger and warmer: R, G, B are multiplied by `(1 + a, 1, 1 - 0.3a)` with `a = 0.40*cream*w_h*max(1 - 2*saturation, 0)^2` (0.90.13: `(1 + a, 1 + a/4, 1 - a)`, `a = 0.15*cream*w_h*(1 - saturation)^2`), then rescaled so Rec709 `Y` is unchanged. It fades out completely at HSV saturation 0.5, so sky and practicals keep their hue (max 15 degrees HSV move for saturation above 0.3, was 19).
- Region weight is now a smoothstep over 1.5 stops past each color start (`u = clamp(t/1.5, 0, 1)`, `w = u^2(3 - 2u)`), still C1 at the start, but it reaches full strength (0.90.13's `t^2/(1+t^2)` never reached 1, so amount 1 never fully desaturated). Applies to highlight and shadow desaturation and to cream.
- New default measured through the repo `Kodak_2383_D65` LUT with per-frame gray-world balance (frames 0280 / 0206 / F2 / F5), top 1% RGB: 210/200/187, 207/203/198, 206/196/190, 213/205/201 (0.90.13: 0206 205/203/199, F5 208/207/204; color off: 0206 203/202/204, F5 206/207/210; reference Silbersalz35 graded 197/191/188). Top 1% saturation 0.110/0.063/0.079/0.056; mid-tone luminance unchanged on 0206/F2/F5. Highlight desaturation 1 with cream on: 0206 211/202/191, F5 217/204/194; with cream off: 0206 203/204/196, F5 206/207/202 (the residual blue dip is the 2383 LUT's own rendering of neutral highlights).
- Defaults otherwise equal 0.90.13 (desaturation 0.60 from +0.75, cream 1.00, shadow 0.70 from -2.00), but the default look is warmer because cream and the region weight changed.
- Tests: `tone_test.cpp` adds desaturation 1 + cream off = neutral and + cream on = warm (R > G > B, (R-B)/R >= 0.05) above the highlight start; all earlier tone checks pass (off = 0.90.11 and color zero = 0.90.12 bit for bit, kernel vs reference worst 7.2e-7, ramps, hue, cream only above start, desaturation only in the tails). Host test checks the two groups, definition order, per-group presets and per-group Custom. PoCL OpenCL runs 9 tone/color preset pairings on every output (344 cases, max abs 8.4e-5). PTX regenerated (NVRTC 12.0.140, 18 registers, no spills). Plugin label **NegativeScan Studio OFX 0.90.14**, version minor 9014.
- Known limits: not yet run in Resolve or on a real GPU. A viewer gray pick on a bright patch looks cream when Highlight cream is on (by design). The 2383 LUT still renders neutral blacks cyan and neutral highlights slightly yellow.

## 0.90.13 Tone color: highlight and shadow desaturation, highlight cream

Request: lower saturation in highlights and shadows and give highlights a cream look, inside the plugin, default on.

| Preset | Highlight desaturation / color start (stops) / cream | Shadow desaturation / color start (stops) |
|---|---|---|
| **Soft cinematic** (default) | 0.60 / +0.75 / 1.00 | 0.70 / -2.00 |
| Gentle | 0.40 / +1.00 / 0.60 | 0.40 / -2.25 |
| Strong roll-off | 0.75 / +0.50 / 1.00 | 0.80 / -1.75 |
| Night practicals | 0.50 / +1.00 / 0.80 | 0.50 / -2.50 |
| Off / Classic 0.90.11 | 0 / +0.75 / 0 | 0 / -2.00 |

- Runs after the 0.90.12 shoulder and toe, on scene-linear values, before the output encoding (all five outputs). Region weight from the RGB mean in stops, `x = log2(mean/0.18)`: highlight weight `w(x - start)`, shadow weight `w(start - x)`, `w(t) = t^2/(1+t^2)` for `t > 0`, else 0. The weight fades in with zero slope (C1); nothing between the shadow and highlight color starts changes. The shadow start is never above the highlight start.
- Desaturation mixes each pixel toward its own Rec709 luminance `Y`: `c' = c + (hd*w_h + sd*w_s)*(Y - c)`. `Y` and HSV hue are unchanged; values stay non-negative.
- Cream multiplies R, G, B by `(1 + a, 1 + a/4, 1 - a)` with `a = 0.15*cream*w_h*(1 - saturation)^2`, then rescales so `Y` is unchanged. Neutral highlights turn warm cream; saturated highlights (sky, practicals) keep their hue (at most about 19 degrees HSV move for saturation above 0.3). Mid-tones and shadows get no tint.
- Luminance is unchanged by the color stage, so luminance ramps behave exactly as in 0.90.12.
- Off: all three strengths 0 gives output bit-identical to 0.90.12 (checked against a d969a3f build on 1.9 million values for every preset and output); all Tone off still equals 0.90.11.

Evidence (repo 2383 LUT, gray-world balanced, frames 0280 / 0206 / F2 / F5; reference Silbersalz35 graded: top 1% saturation 0.050, top 1% RGB 197/191/188, bottom 5% saturation 0.127):

| Frame | Top 1% sat 0.90.12 -> 0.90.13 | Top 1% RGB 0.90.12 -> 0.90.13 | Bottom 5% sat | Y p50 |
|---|---|---|---|---|
| 0280 | 0.117 -> 0.112 | 210/199/187 -> 209/200/186 | 0.746 -> 0.709 | 137 -> 143 |
| 0206 | 0.083 -> 0.064 | 203/202/204 -> 205/203/199 | 0.718 -> 0.713 | 137 -> 137 |
| F2 | 0.071 -> 0.068 | 202/196/194 -> 203/197/191 | 0.502 -> 0.449 | 127 -> 127 |
| F5 | 0.031 -> 0.025 | 206/207/210 -> 208/207/204 | 0.834 -> 0.691 | 100 -> 100 |

Without the gray-world balance (plugin defaults, blue cast on these daylight frames) top 1% saturation drops more: 0.082/0.210/0.333/0.370 -> 0.060/0.123/0.235/0.230. Bottom-5% saturation stays high because the 2383 LUT renders neutral blacks cyan (about 11/16/18); the shadow control only removes scene color.

## 0.90.12 Tone: highlight shoulder and shadow toe

Goal: the "cinematic HDR" look (soft, non-clipping highlights, open shadows with a soft black, smooth roll-off) when the Cineon output is viewed through the Kodak 2383 LUT, without local contrast. Controls, defaults and presets:

| Preset | Shoulder on / strength / start / white (stops) | Toe on / strength / start / black (stops) |
|---|---|---|
| **Soft cinematic** (default) | on / 1.00 / +0.75 / +2.20 | on / 1.00 / -2.00 / -4.00 |
| Gentle | on / 0.60 / +1.00 / +2.60 | on / 0.60 / -2.00 / -3.80 |
| Strong roll-off | on / 1.00 / +0.50 / +1.80 | on / 1.00 / -1.75 / -3.60 |
| Night practicals | on / 1.00 / +1.00 / +2.40 | on / 0.50 / -2.50 / -4.00 |
| Off / Classic 0.90.11 | off | off |

### Math and behavior

- Where: on the scene-linear positive of the Positive stage, after the per-channel inversion and the optional `Contrast` curve, and before the output encoding. The Density In stage and transmittance output are never toned. Stops are `log2(value / 0.18)`, so they move with `Exposure` and the balances.
- Shoulder (highlights): `n = mean(R,G,B)`, `x = log2(n/0.18)`. Above `start` (`xs`), `f = xs + h*(1 - exp(-(x - xs)/h))` with `h = white - xs` (at least 0.05), then `y = x + strength*(f - x)`. R, G and B are multiplied by the same gain `2^(y - x)`, so R:G:B ratios and hue are unchanged. At full strength the mean never goes above `white`, so with the default preset the brightest channel is at most about `+3.8` stops (3x the mean; Cineon about 800 of 1023): nothing clips in Cineon or in a 0..1 LUT input.
- Toe (shadows): `m = min(R,G,B)` after the shoulder, `x = log2(m/0.18)`. Below `start` (`xt`, never above the shoulder start), `g = xt - r*(1 - exp(-(xt - x)/r))` with `r = xt - black` (at least 0.05), and the same offset `strength*(0.18*2^g - m)` is added to R, G and B. Hue (HSV) is unchanged, deep shadows desaturate slightly, and pure black lifts to the neutral floor `strength*0.18*2^black` (default 0.0113 linear, Cineon about 187 of 1023). The darkest channel is used, not the mean, because an offset from the mean makes the weakest channel of saturated dark colors go backwards; with the darkest channel every channel is monotonic along any hue line.
- Both curves have slope 1 at their start point (C1, no kink), are monotonic, and leave everything between the toe start and the shoulder start untouched.
- Output encodings: the tone runs on scene-linear values, so Rec709, DaVinci Intermediate, Cineon, ACEScct and Linear all receive the same toned linear values through their unchanged encoders. With Linear output the shoulder keeps values below `0.18*2^white` times the pixel's max/mean ratio (at most 3x); with Rec709 values above 1.0 are still possible for very bright saturated pixels, exactly as in 0.90.11.
- Density clamp: the 1.856 density cap (`Clamp output to 0 to 1`) is unchanged. No pixel of the four test frames reaches it (the brightest Cineon values come from the positive curve, not the cap), so softening it was not needed.
- Off: when both switches are off (or both strengths are 0) the kernels take the 0.90.11 per-channel code path, bit for bit (checked against a 0.90.11 build on 3.1 million values: identical).

### Evidence

Offline port `cine_proto/ofxport.py` matches the C++ `Prepared` path to at most 3.2e-7 (Cineon) for every preset. Frames through `develop/luts/Kodak_2383_D65.cube`, 8-bit output, Y = Rec709 weights, centre crop. "gray-world" adds only a per-frame Red/Blue balance because the default balances are tuned for tungsten light and these frames are mostly daylight.

| Frame | 0.90.11 as-is Y p0.5/p50/p99.9, clip>250 | 0.90.12 Soft cinematic as-is | 0.90.11 + gray-world | 0.90.12 Soft cinematic + gray-world |
|---|---|---|---|---|
| Hikrobot 0280 (stock not confirmed) | 12/139/242, 5.80% | 19/116/199, 0% | 13/147/244, 0.46% | 19/137/202, 0% |
| X-T3 0206 (500T) | 17/111/242, 0% | 23/111/202, 0% | 19/137/247, 0.15% | 25/137/205, 0% |
| X-T3 500T F2 | 15/107/229, 0% | 22/107/181, 0% | 15/127/235, 0% | 21/127/205, 0% |
| X-T3 500T F5 | 13/85/242, 0.89% | 20/85/181, 0% | 13/100/246, 0% | 19/100/209, 0% |
| Reference: Silbersalz35 graded (phillipreeve.net Part 5) | 14/133/198, 0% | | | |

Mid-tone saturation (gray-world) is unchanged on 0206, F2 and F5 and rises from 0.232 to 0.297 on 0280, whose mid tones are bright enough to reach the shoulder (its p50 drops from 147 to 137).

### Tests

`ofx/tests/tone_test.cpp`: off and zero strength are bit-identical to the 0.90.11 per-channel path for all 5 outputs and 4 contrast settings; the kernel math agrees with the independent stops-based reference in `tone_math.hpp` (worst 7.5e-7); 16-bit ramps are monotonic along fixed-hue lines for every preset (float rounding only) and the mean is monotonic on the real input ramp; Cineon stays below 1.0 whenever the shoulder is on; HSV hue drift on saturated patches is at most 6e-5 degrees; slopes match on both sides of each pivot; pure black lifts to the documented floor. A 0.90.11 build (commit 5f4cfcc) and this build with Tone off give identical bits on 3,111,120 values. PoCL OpenCL runs every preset on every output (264 cases, max scaled error 1.5e-6); the mocked CUDA contract renders the default tone through the native-compiled CUDA source; Go parity runs with Tone off. Not tested: real Resolve, real NVIDIA/AMD GPU timing or execution of the PTX.

Known limit: on a gray input ramp the three channels drift apart in the densest highlights because the stock gammas differ per channel. The shoulder keeps R:G:B, so in that region the weakest channel can dip by up to 0.15 stops (`Soft cinematic`; 0.20 `Strong roll-off`, 0.15 `Night practicals`, 0.01 `Gentle`) while luminance stays monotonic.

## 0.90.10 User-saved Color balance presets

Adds a Save/Delete preset workflow under **Color balance** without changing render math. The inspector gains `Balance preset` (`None` + eight fixed slots), `Preset name`, `Save`, `Delete`, and `Preset status`. Save writes the current visible `positive_bal_r/g/b` values to `%APPDATA%\NegativeScanStudio\balance_presets.json` (or `NSS_BALANCE_PRESETS`) under an ASCII name; the same name overwrites, and Delete clears the selected slot. The file is shared across Resolve projects. A missing or unreadable file is treated as an empty list.

Fixed slots are used because OpenFX treats gaps in `kOfxParamTypeChoice` after describe as an error, and hosts including DaVinci Resolve do not reliably let a plugin rebuild choice options at runtime. Slot labels are updated on the instance when possible (`Slot N: name`); the status line always lists saved names. Selecting a preset only writes the three balance sliders, which then go through the existing pipeline. New parameter IDs are appended; every previous ID and default is unchanged. Plugin version minor is 9010. Mock-host tests cover save/apply/overwrite/delete and corrupt-file recovery. Real Windows Resolve UI behaviour is still untested.

## 0.90.7 Simplified English UI & Camera Calibration Dropdown

- **UI-only scope**: Reduces the Resolve inspector to 13 visible English controls in 4 open groups (`Camera calibration`, `Film base`, `Film & Gamma`, and `Color balance`). `Exposure` (`0.90`), `Contrast` (`Off`), `Output` (`Cineon` default), and `Red/Green/Blue balance` (`1.07 / 0.00 / 0.67`) stay visible. All 42 technical controls (matrix coefficients, individual gammas, `Gamma scale` `1.15`, `Film white balance`, Density In constants, gray pick, stage, render engine) are marked `kOfxParamPropSecret = 1` with unchanged IDs and defaults so existing projects load identically.
- **Camera calibration dropdown (`camera_mode`)**:
  - `X-T3`: Uses pending `camera_matrix` from `nss.scan/1` metadata, or falls back to `DefaultXT3Trichrome` (`[0.86426, 0.10467, -0.01969; -0.05068, 0.98176, 0.01644; 0.11287, -0.14623, 1.04095]`).
  - `Monochrome (MV-CS240-90UM)`: Uses the fixed neutral-preserving matrix fitted from `chart_hik2.tiff` against SoftLoaf Ektachrome transmission target No. 000004 (`[0.942661, 0.195781, -0.138442; -0.056435, 1.036487, 0.019948; 0.014258, -0.089212, 1.074954]`, mean ΔE2000 ~2.16, exact unit row sums `[1, 1, 1]`).
  - `Off` (default): True bypass (`camera_on = 0`). Metadata marked `camera_applied: true` forces `Off` to prevent double-applying a baked matrix.
- **Coordinate consistency & color-science limitation**: Because the monochrome matrix has unit row sums, `[0.5, 0.5, 0.5]` film bases remain invariant across raw and `after_profile` coordinates. Metadata loading preserves a user-selected `Monochrome` mode when no embedded `camera_matrix` is present. Positive-chart color mapping (measured on Ektachrome dyes) is **not** a universal color-negative dye crosstalk correction and still requires validation on real negative film scans.

## 0.90.6 Ektar 100

Ektar 100 appends a stock option without changing previous indices. It writes visible gamma R/G/B 0.580572786/0.579564388/0.663740443, defaults T/D to D, and keeps manual overrides. Values are interpolated OLS slopes from Kodak E-4046, January 2025, page 4 daylight Status M vector paths, using the existing logH -1.5..0 method. [Source, axes, PDF hash, residuals and alternate windows](data/ofx_stock_gamma_readings.json).

No uncalibrated Base placeholders exist, so no Ektar Base is invented. Existing Base, scale 1.15, Exposure 0.90 and all rendering kernels remain unchanged. Scanning app stays v0.91. Full CPU/mock-host suite and offline CUDA assembly checks are run; real Windows Resolve/GPU/overlay and actual Ektar scans need acceptance testing.

## 0.90.5 after-profile metadata

The app writes its last accepted Light-page leveling base, not a new image
measurement. The plugin recognizes the explicit coordinate marker and loads the
reference without applying the camera matrix twice. Older unmarked metadata is
unchanged. Rendering kernels and film presets are unchanged.

## 0.90.4 — Pick gray / white in the viewer

Adds OFX OverlayInteractV1 (OpenGL 1.x, loaded dynamically from the system). The first Viewer gray / white pick group is requested expanded by default. **Select the NSS node/effect first → in the lower-left of the viewer choose Open FX Overlay / FX → enable Pick gray on viewer → click or drag a neutral gray/white area.** Shift + ~ (tilde) toggles overlay visibility. Window / Transform / Wipe viewer modes do not dispatch clicks to this plugin.

The crosshair stays a constant screen-pixel size; the sample box is converted through render scale and pixel aspect ratio. Both the pen and `pick_point` use canonical XYAbsolute and are not multiplied again by viewer zoom or render scale. The first click turns Auto gray on and resets the visible Base / balances to None / 0; later clicks while Auto is already on keep a manual balance. After a point is chosen you can turn Pick gray on viewer off; Auto continues to work on that fixed point every frame. Turning Auto off returns to manual gray codes. There is no Bake and no automatic tracking.

The host OpenGL context is used only to draw the UI: the plugin does not read the framebuffer and does not change the CPU / CUDA / OpenCL image algorithm. It push/pops GL state, does not modify the host matrices or projection, and does not swap buffers. `SlaveToParam` triggers a redraw on parameter changes; a click/drag is one undo group; PenUp / LoseFocus / an exception ends the edit. The native 2D handle and typed canonical X/Y remain as a fallback. Real Resolve drawing, mouse dispatch, HiDPI, proxies, and non-square pixels still need acceptance. All previous numerical defaults and the scanning app EXE were unchanged in this release.

References: [OFX interact API](https://openfx.readthedocs.io/en/latest/Reference/api/file/ofxInteract_8h.html), [Resolve on-screen controls (third-party mirror of the manual)](https://docs.imaster.org/davinci-resolve-21-manual/part-11-color-page-effects/ch.-151-using-open-fx-and-resolve-fx). The Resolve manual confirms OpenFX on-screen controls / FX mode; that is not a substitute for testing this plugin on a real host.

## 0.90.3 — 500T Base only

User eye-tuning set **R +1.07 / G 0 / B +0.67 stops** (previous +1.40 / 0 / +0.80), used both as the 500T Base loader and as the visible balance defaults on new instances. This was not a new measurement. Existing projects keep host-saved slider values; re-selecting 500T Base loads the new numbers. Gamma scale 1.15, Exposure 0.90, stock gammas, the T5600 table, GPU/CPU algorithms, and the scanning app were unchanged in this release. (Gamma scale and Exposure defaults were later restored to 1.0 in 0.90.9 and 0.90.11.)

## 0.90.2 — Default look and a real GPU path

- Film-stock **gamma preset numbers stayed at their 0.90.1 values**; they were not uniformly multiplied by 1.25. Positive Out gained a **Gamma scale** control immediately after Gamma R/G/B, default **1.15**, range 0.25–2. Effective gamma = visible R/G/B × visible scale, preserving channel ratios. Exposure defaulted to **0.90 stops** (previously 1.00). Those two new defaults were a user-chosen scan look / contrast adjustment, not a correction of Kodak-measured gammas. Later releases set both defaults back to 1.0; existing projects keep whatever the host saved.
- New 0.90.2 instances therefore did not match 0.90.1 defaults on purpose. Pure formula / DCTL comparisons needed scale = 1 and Exposure = 1, or the corresponding effective gamma / exposure fed into the reference. Back up old projects: if the host restored Exposure 1 but picked up the new scale of 1.15, the look still changed. Setting scale to 1 recovered the old gammas.
- Example: the 500T LED preset still displayed .45 / .54 / .53; at scale 1.15 the effective values were .5175 / .621 / .6095. The older 500T Base had been measured at scale 1. Under the 0.90.2 defaults the Base was eye-tuned from 1.57 / 0 / 1.00 to 1.40 / 0 / 0.80 (and later to 1.07 / 0 / 0.67 in 0.90.3). That eye-tuning is not a universal stock calibration, and no re-measurement was done.
- On Windows NVIDIA / Resolve, a plugin that only implemented OpenCL could be forced back to CPU when the host was in a CUDA mode, implying a full-frame GPU↔CPU copy. That was a **testable hypothesis, not a measured root cause on the user's machine**. 0.90.2 added a CUDA Driver API / PTX path so the host can pass CUDA device pointers and a stream directly.
- After playback or a picture change, **Show render engine** should report the engine actually used: `CUDA PTX (host stream)`, `OpenCL buffers (host queue)`, or `CPU N threads`, and whether the driver / runtime is available. “A CUDA driver is installed” does not mean Resolve negotiated CUDA. If the line still says CPU, check the host GPU mode, the driver, and stale bundles / plugin caches before promising any FPS figure.

## Processing chain and interaction

Linear camera calibration → Density In → Positive Out. Following feedback, the entire mask/crosstalk parameter group and processing stage were removed; camera calibration remains. The independent DCTL / Go Develop paths were not changed. New OFX Film base defaults to **0.5 / 0.5 / 0.5**. At 0.90.2, Positive gamma sliders defaulted to .55 / .635 / .62, Gamma scale to 1.15, 500T Base loaded into the visible balances (later 1.07 / 0 / 0.67), Exposure +0.90, and Cineon output stayed the default. Back up old projects: a removed stage no longer runs, and an identical look is not promised. Current defaults (Gamma scale 1.0, Exposure 1.0, Tone / Tone Color on) are described in [ofx/README.md](../ofx/README.md) and in the 0.90.9–0.90.14 sections above.

Film base and balance combine stock gamma, T/D, 3200/3800/4400/5000/5600 K, and the film-base sliders. Gamma R/G/B and the Gamma scale that follows Gamma B live in Positive Out. Choosing a stock immediately fills gamma and suggests T/D; choosing type / colour temperature immediately fills film base. There is no Apply or cast field. Manual trimming remains. The scientific model and the full film-base table are in [WB](data/ofx_wb.md); gamma sources and uncertainty are in [stock gamma](data/ofx_stock_gamma.md).

`base = baseline × 10^(+offset)`: the positive sign matches Density In `-log10(pixel / base)`. New instances use baseline 0.5; after Load metadata the imported camera-corrected base becomes the baseline. The baseline is persisted on its own, so switching presets or loading repeatedly does not compound. A manual base trim is reset the next time a preset is chosen. Automatic source-metadata mode applies the same compensation on the render copy and does not write the UI. Hand-editing base, choosing a preset, or Load leaves Auto so a manual override can stick.

Positive Out keeps **Film white balance (None / 500T Base)** as a loader, not a hidden extra gain. 500T Base currently writes the user eye-tuned R +1.07 / G 0 / B +0.67 stops into Red/Green/Blue balance; None resets the three sliders to 0. The old white_code 0.0013 slope was removed. T/D / stock choices update their own base / gamma only and do not clear a manual balance; the two can be combined explicitly. Each stock will have at most one measured Base; unmeasured stocks do not get invented numbers. [Table, conditions, and measurement notes](data/ofx_film_bases.md).

## Viewer gray / white point

1. As in the 0.90.4 section, choose Open FX Overlay, enable Pick gray on viewer, and click or drag an actually neutral gray/white reference. You can also turn Auto gray from point on manually and use the native 2D handle or type canonical X/Y. Do not use an arbitrary coloured object. The default point is still the frame centre from normalised 0.5 / 0.5.
2. `Sample radius (pixels)` is 0–32 and takes a square patch at the **current render resolution**; it is clipped at the image edge and skips transparent or non-finite samples. The requested Source ROI includes the patch and handles non-zero bounds, render scale, PAR, and premultiplication.
3. Each frame reads the sample from SOURCE, undoes Density In, applies the current camera and Density In, and averages to a density code going into Positive Out (×1023). Only the render copy enables `gray_on` / `gray_r/g/b`, using the existing gray-point formula so R and B are neutral relative to G. Film base is not changed, no animation keys are written during render, and the exposure target is unchanged; balances can still be trimmed by hand afterwards.
4. CPU reads the small patch directly. OpenCL does one blocking `clEnqueueReadBufferRect` of the small patch and then the same density math. **This small readback / sync happens only when the picker is on**, and is off by default. The plugin does not download the whole GPU image and does not treat a `cl_mem` as a CPU pointer.
5. The point is a fixed frame coordinate sampled every frame, **not a tracker**. A moving subject, a saturated white, a pure-black or transparent area, or an upstream node that changes the pixels will affect the result. If the point is outside the input bounds or has no valid pixels, the manual gray point is kept and Show render engine shows `pick unavailable`. There is no bake button. A custom GL overlay is implemented and the native point fallback is kept; real Resolve viewer appearance and input still need acceptance.

## Data contract

TIFF IFD0, tag 270 (ImageDescription), ASCII NUL-terminated JSON. Non-ASCII / non-BMP characters use JSON Unicode escapes. No private TIFF tag is required. Original tags such as Make / Model / Software, LZW, and the original pixels are preserved.

```json
{
  "schema": "nss.scan/1",
  "software": "NegativeScan Studio v0.90 trichrome ...",
  "created": "2026-11-07T14:00:00+08:00",
  "roll": "example",
  "mode": "trichrome",
  "light_model": "CuteNewDesign RGB Light Air",
  "light_rgb": [1200, 2300, 3400],
  "film_base": [0.49844, 0.49639, 0.50210],
  "film_base_space": "after_profile",
  "base_source": "light leveling (after profile)",
  "encoding": "linear",
  "camera_matrix": [1,0,0,0,1,0,0,0,1],
  "camera_strength": 1,
  "camera_applied": false,
  "camera_profile": "example profile"
}
```

The example shows app v0.91 coordinates, not a universal film base. `light_rgb` is lamp drive, never the reference. `film_base` is the accepted Light-page leveling result after profile, unchanged by capture or merge. The app's default-on capture option displays that result and timestamp. Off or no matching accepted result omits the base instead of estimating it from image content. Light state and scan presets persist the result.

OFX 0.90.5 recognizes `film_base_space: after_profile` and does not multiply that base by the pending camera matrix again. TIFF pixels still receive their pending matrix. Older unmarked files retain their original pre-profile behavior. Visible base sliders equal the reference times `10^offset` for the current T/D selection. T at 3200 K or D at 5600 K displays the exact loaded reference. Files with `encoding=processed` or nonempty `processing` remain ineligible for automatic linear conversion. Existing v0.90 TIFFs are not automatically repaired.

`created` is the write time (Asia/Shanghai, UTC+8), not the time the film was shot. There is no EXIF original-date recovery, and a Kelvin value is not claimed to imply a device RGB PWM. A scan TIFF and the scene colour temperature of the photograph are independent.

Metadata is written on the original HDR / trichrome output path; crop copies the original tag 270; hair / dust write-back preserves it. Offline Merge does not copy the current lamp state onto an old picture. Trichrome single-channel intermediates are marked `encoding=trichrome-component` and are not treated as finished RGB for automatic loading. A standalone RAW Convert or Develop-preview export has no scan-time lamp state and **must not pretend to carry roll-scan metadata**. Old files are not rewritten in place. A sidecar can be exported on demand with the script in the plugin ZIP; a sidecar is not forced on every scan.

## File loading and a measured root cause

A direct inspection of the user's `/workspace/uploads/0206.tiff` showed 4170×6246, about 156 MB, the IFD after the pixel data, and **no tag 270** (only pixel tags, Software, and similar). The older message “No NSS metadata…” was therefore a correct “this old scan has no information”, not a confirmed Windows path failure. Unknown RGB cannot be recovered from metadata that does not exist; old files need values typed by hand or a sidecar that contains measured values.

Hardening in this era: the path is copied immediately from the OFX string; outer quotes and whitespace are stripped; `file://` and percent-encoded UTF-8 are accepted; Windows uses `_wfopen` with UTF-8→UTF-16 and `_fseeki64`; classic TIFF and BigTIFF, LE/BE, and 64-bit offsets are supported. Only the header, IFD, and a description of at most 1 MiB are seeked/read — **pixels are not read**. A JSON BOM is accepted. Truncation, array, encoding, schema, and matrix checks apply; missing information is negatively cached so an old file is not re-read every frame. The same path's mtime is probed at most every 250 ms, and metadata is re-read only when the path or mtime changes; an explicit Load forces an immediate re-read. The button catches exceptions so they do not cross the C ABI. Narrow-UI status strings include `Loaded: 0.50/0.50/0.50`, `No NSS info (old scan)`, and `File not found`.

Load writes base / camera, establishes a baseline that has not had scene WB applied, then applies the current preset once, and turns Auto off. Failure leaves the previous base unchanged. A pending matrix transforms both the image and an imported film base; a baked matrix is not applied twice. Device `light_rgb` is provenance / PWM only and is never treated as a normalised film base.

## Resolve automatic source-path limits

Standard OFX does not guarantee a source filename. The plugin probes the publicly documented Resolve 20 `OfxImageEffectPropSrcFilePath`, checking render args, the input image, the effect, and the source clip in that order. If it is missing, use an explicit file path and Load. Public reference: [ASWF #142](https://github.com/AcademySoftwareFoundation/openfx/issues/142). The private Resolve SDK is not installed here, so every host version is not guaranteed to expose the property.

When Auto is effective, metadata covers only the render copy and the UI does not jump; missing or damaged metadata falls back to manual, and Load can show the reason. Proxies, compound clips, `%04d` sequences, a different file, or an upstream non-linear / colour-managed transform can make the path disagree with the pixels. Only the same unconverted linear NSS scan is valid — this is not automatic colour management. After changing camera calibration or neutralisation, re-establish the film base. The sidecar export script is currently limited to classic TIFF; the plugin itself supports BigTIFF.

## CUDA / OpenCL / CPU

### CUDA: experimental Windows NVIDIA path

- **nvcuda.dll** is loaded dynamically from the system directory; only the CUDA Driver API is used. The user machine does not need nvcc, NVRTC, or a toolkit install, and no NVIDIA DLL or proprietary SDK headers are bundled.
- The plugin declares OFX CudaRenderSupported / CudaStreamSupported, reads real device pointers when CudaEnabled, and uses the host stream when CudaStream is set. It restores the host context from the source allocation / stream and restores the thread's previous context when the scope ends. It does not create its own GPU context and does not copy full frames to the CPU.
- PTX is JIT-compiled once per instance / host context and the module / function are cached. Each launch passes 40 floats (later builds use more for Tone) as a small parameter block; no device constant buffer is created or uploaded, and mutable kernel parameters are not shared.
- When a stream is provided the plugin only enqueues: it does not finish or deviceSynchronize. Older hosts that omit the stream property wait on the default stream, per the OFX contract. Only the gray picker needs a small pinned-host download and a wait on that stream.
- PTX is generated offline on Linux with official **NVRTC 12.0.140**, targeting compute_52 / PTX 8.0, IEEE division, no FMA / FTZ / fast-math. ptxas assembled it for sm_52 with 16 registers and no stack or spills (later Tone builds regenerated PTX; see the 0.90.12–0.90.14 notes). A CUDA 12-compatible NVIDIA driver and sm_52 or newer are required; actual Windows Driver JIT still needs acceptance.
- Tests cover a mock Driver ABI, opaque device addresses, context switch/restore, host/default streams, module cache, native-compiled CUDA source math, and the optional gray point. **This is not PTX GPU execution or real CUDA math parity.** There is no physical NVIDIA GPU on the box, so no real CUDA performance claim is made.

### OpenCL and CPU

- OpenCL.dll is loaded dynamically from the system directory. The plugin uses the host queue and `cl_mem` buffers; OpenCL images are not used. Programs and kernels are cached per context / device; setting args and enqueueing is guarded by a short mutex. Switching between two real PoCL contexts does not recompile. There is no per-frame or per-tile compile.
- Exact CPU math and up to eight threads remain. Fully identical Pipeline `Prepared` constants are cached per instance and rebuilt only when parameters, metadata, or the gray-point result change; animation values are not guessed from time. Render still reads parameters at the current time so a stale animation cache is avoided.
- SupportsTiles / MultiResolution are on; HostFrameThreading is off, so host tile threads are not multiplied by the internal CPU threads. `clipGetImage` requests the canonical ROI of the render window. With the gray picker off this is not expanded to the full frame; with it on the patch is included. The host may return a larger buffer; the plugin does not assume a tightly packed layout.
- A GPU failure returns OFX GPU render failure (1002) so the host may retry on CPU. CUDA / OpenCL pointers are never treated as CPU pointers. Render engine can be forced to CPU. Automatic fallback depends on the host.
- Metal is not part of the current Windows package and is not implemented. Both CUDA and OpenCL require a real host negotiation; an advertised flag alone does not mean the effect is running on the GPU.

## Validation and performance

- A Go v0.90 writer fixture writes a real TIFF that the current plugin imports. Tests cover classic / BigTIFF LE/BE, a sparse metadata offset above 4 GiB, Unicode / quoted / file URL / BOM paths, the real old 156 MB TIFF, and bad metadata.
- A minimal OFX suite host calls the real Load / Describe / Create / Render / Changed / ROI / Destroy path: negative row stride, non-zero origin, alpha / premultiplication, automatic metadata, immediate stock / gamma / base, repeated Load / switching without compounding, the correct WB sign, picker neutralisation, and out-of-bounds fallback. Native / custom viewer overlay appearance was not tested here.
- The overlay host test calls the real callbacks from the descriptor: Describe / Create / Draw / PenDown / Motion / Up / LoseFocus / Destroy, and checks arming, canonical points, first-click Auto / visible-balance reset, later trims being kept, RoD bounds, GL command geometry / state protection, and render-scale / PAR. GL is a recording mock, not real OpenGL or Resolve.
- CPU versus the independent Go DCTL port: 100 combinations × 360 pixels × RGB = 108,000 values. The old Go hidden WB is off; the same visible manual gains are compared. Maximum absolute difference **9.16e-5** (HDR), **6.10e-5** on a real scan patch, scaled max **1.50e-6**. The test uses `1e-5 * max(1, abs(reference))` and does not claim every HDR absolute error is ≤ 1e-5. [Results](data/ofx_0903_parity_result.json). Later Tone releases have their own tighter kernel-versus-reference figures in the 0.90.12–0.90.14 sections.
- Real OpenCL Render: 164 combinations + 4 two-dimensional picker / prepass combinations (0.90.2 also verified a dual-context program / kernel cache), 193,536 channel comparisons, maximum absolute difference **2.10e-5**, scaled **9.93e-7** versus CPU. [Results](data/ofx_0903_opencl_result.json). Other GPU drivers still need acceptance.
- The Windows x64 MinGW static C++/gcc DLL exports the two OFX entry points and imports only system KERNEL32 / msvcrt. OpenCL.dll is loaded at runtime and is not a hard dependency.

## 0.90.2 actual Render CPU comparison (not CUDA or Resolve FPS)

Conclusion: **there is no extra CPU speedup**. On this round the new build's large-frame median was about 2.8% slower (the five-run ranges overlap) and a single 16×16 micro-tile was about 1.9% slower (about 0.74 µs). Caching changes are not packaged as a CPU throughput win. The main goal was to let an NVIDIA host negotiate CUDA and avoid a CPU fallback and unnecessary full-frame requests. Any real gain has to be confirmed on the user's machine.

The same minimal OFX host actually called Render on the old tag `ofx-v0.90.1` and on 0.90.2; this is not a single math-function microbench. Effective parameters on both sides matched the 0.90.2 defaults: raw gamma .55 / .635 / .62 × 1.15, Exposure 0.90, balance 1.40 / 0 / 0.80, the same pending camera / base metadata, Cineon, picker off. The old build was hand-matched on gamma / balance so a look change is not counted as an optimisation.

Linux 8-vCPU Xeon/KVM, same `-O3` / no-math-errno / no-trapping-math. Each process warmed up once, then five alternating old/new groups. The real 0206 full frame is 6246×4170. Timing includes parameters, metadata, image fetch, prep, and internal thread start; it excludes TIFF decode, process start, real host scheduling, and GPU↔CPU copies.

| CPU Render | 0.90.1 median / range ms | 0.90.2 median / range ms |
|---|---:|---:|
| Full frame | 405.332535 / 402.708040–419.419989 | 416.594263 / 404.228400–470.040237 |
| 16×16 tile (2000 times/group, per call) | 0.039079 / 0.038704–0.039299 | 0.039818 / 0.039739–0.040203 |

All output checksums matched, every output was finite, the fixed-grid reference error was 0, and there were 0 failures. In the small-tile groups the new build built `Prepared` once, parsed metadata once, and probed mtime once; full-frame source requests went from 2000 to 0 (an explicit ROI is requested instead; how a real host allocates that is still untested). On the large frame, CPU arithmetic still dominates: an independent non-comparison profile of eight frames was 4.376 s wall / 28.553 s user / 0.584 s system. A 250 ms metadata throttle or a one-time JIT cache must not be advertised as a multi-fold CPU pixel-math speedup.

Reproduce with `ofx/tests/benchmark_host.sh`; [raw five-run checks and counts](data/ofx_0902_host_benchmark.json). [CUDA offline build / assembly record](data/ofx_0902_cuda_build.json). There is no physical NVIDIA GPU, so CUDA Driver JIT, PTX execution parity, and GPU performance were not measured.

### Historical 0.90 → 0.90.1 CPU benchmark (not an extra 0.90.2 speedup)

0206 full frame 6246×4170 = 26,045,820 pixels, in-memory float RGBA. Linux box, 8 vCPU, Intel Xeon/KVM; same compiler and `-O3 -fno-math-errno -fno-trapping-math`. The comparison commit is `fc8dd26`, single-threaded (the originally shipped 0.90 used O2; both sides are O3 here). Same 0.5 base, original-gamma, Exposure, Cineon, identity camera, old crosstalk off; both sides use the same measured Base (the old reference is equivalent to None + manual R 1.57 / B 1.00). Each path is warmed once, then five alternating groups, including preparation and thread start; a checksum is consumed and every value is compared.

| path | median ms | min–max ms |
|---|---:|---:|
| Original v0.90 single thread | 3779.940 | 3757.330–3808.072 |
| 0.90.1 prepared single thread | 1796.088 | 1780.715–1822.019 |
| 0.90.1 prepared 8 threads | 331.550 | 283.902–336.799 |

This test range is **11.40×**, with bit-exact default full-frame output (max error 0). The optimisation is not a lower resolution or a cached result. Single thread is 2.10×, then 8 threads another 5.42×, which supports a “repeated per-pixel work / single-core arithmetic bound” reading, not a TIFF decode / I/O speedup. The optional picker readback is not included in this default-off bench.

The scope is CPU in-memory render only: no decode, metadata I/O, viewer / host scheduling, or GPU transfer / execution. **This is not an 11.40× end-to-end Windows / Resolve speedup**; real timings await user acceptance. [Raw five runs, errors, and checksums](data/ofx_0901_cpu_benchmark.json), reproduce with `ofx/tests/benchmark.sh`.

Scientific limits: T = 5219 / 3200 K, D = 5207 / 5600 K (other daylight stocks temporarily share 5207); Planck is not a measured LED or CIE D. A Status M curve slope is not equivalent to a scanner RGB gamma; see [5219 LED](data/5219_led_gamma.md). The scanning lamp, camera WB, and the cancelled 400K stash are not changed here.

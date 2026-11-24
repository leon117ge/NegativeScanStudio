# Architecture

NegativeScan Studio is two products that share a repository and a metadata contract, but not a version number.

```text
Camera + RGB light
        │
        ▼
Windows scanning app  ──linear TIFF + nss.scan/1──►  OFX plugin in DaVinci Resolve
        │                                              │
        └── optional in-app Develop preview            └── Cineon (or other) ──► print LUT
```

## Hardware

Primary capture path at v1.13:

- **Hikrobot MV-CS240-90UM** (USB3 monochrome, GMAX2424, 6016×4096, Mono 8/10/12) via `hikcam/` (runtime load of `MvCameraControl.dll`, no link-time dependency). Any USB3 or GigE MVS device is enumerated; a simulator is always offered if the SDK or camera is missing.
- **CuteNewDesign RGB Light Air** via `rgblight/`, USB CDC, RGBAIR API 1.3. Only the documented customer commands are sent (brightness, on/off, mode, focus light, status). Serial-number writes and firmware upgrade are rejected.
- **Fujifilm X-T3** remains supported through Windows PTP/WPD (`camera/`) for Bayer / RAF capture, trichrome with the same RGB light, and the older leveling path.

Details, measured LED peaks, and limits: [HARDWARE.md](HARDWARE.md), [LIGHT_LEVELING.md](LIGHT_LEVELING.md), [mono-trichrome.md](mono-trichrome.md).

## Scanning app — capture pipeline

The Windows GUI (Go, walk) is organised as four modules: Camera & Light, HDR Scan (including **Mono Trichrome** and **Focus Check**), Crop & Merge, and Develop. Convert remains available for already-captured still-stock work. DCTL export was removed in app v0.91; Resolve development is the OFX plugin’s job.

### Mono trichrome HDR (Hikrobot)

Default bracket: **0 / +2 / +3 / +5 EV** from a single shared exposure (microseconds, gain 0 dB) that is linked to Light-page Hikrobot leveling. R, G and B film-base frames all use that shared exposure; HDR brackets are EV offsets from it. Optional per-channel auto-meter exists but is off by default.

For each light colour and each bracket step the camera can average **frames-per-exposure** stills (configured on the Mono page) before the merge. A light-settle check and an R/G/B movement check gate the capture. Stop cancels between steps (`monocap.Options.Ctx`) and restores the light and grab state.

Merge is a native monochrome HDR merge (v1.10+), not a Bayer path. Noise parameters come from the published sensor specifications (12.1 ke−, 2.7 e−) as of v1.11. Black-level subtraction is applied (BlackLevel 32 is the assumed dark offset; it still needs confirmation at long exposures on the real body).

Output is a linear TIFF. When **Film base metadata: Last leveling** is on (the default), the accepted Light-page after-profile reference is written into TIFF ImageDescription (tag 270) as `nss.scan/1` JSON. Capture does not re-measure the scene as film base. Off, unknown, or mismatched lamp-drive values omit `film_base` rather than inventing one.

### RGB light control and leveling

Leveling solves integer lamp codes so a clear-base patch reads near 0.50 per channel after the camera profile (default acceptance 0.49–0.51). The solver uses measured response slopes, a bounded integer search, and a fresh verification set. It must not lower exposure to put a bright channel on target when another channel is already at code 100 and still too dark; the exposure floor comes from the dimmest channel. Failed runs do not replace the last accepted reference.

### Focus Check and LiveAE (v1.13)

Focus Check live view uses free-run, downsampled preview, and `hdrscan.LiveAE`:

- two settle frames are skipped after any hardware exposure/gain write, so in-flight frames cannot oscillate the loop
- `State` is synced to the actual hardware exposure/gain
- exposure locks once converged until the user zooms, pans, switches the light, or scene brightness changes substantially

An optional centre / edge / corner mark on Mono live view and Focus Check is display-only and is never written into TIFFs.

### After capture

Crop, merge, hair and dust tools preserve tag 270. The in-app Develop preview can invert a linear TIFF for a quick look and can apply the embedded 2383 cubes. It is not the Resolve plugin and must not be treated as a bit-exact OFX preview.

## OFX plugin — processing pipeline

The effect is one OpenFX plugin. Current label: **NegativeScan Studio OFX 0.90.14**. There is no mask/crosstalk stage.

Pipeline, in order:

1. **Camera calibration** (optional). `X-T3` uses a pending matrix from metadata or the default X-T3 trichrome matrix. `Monochrome (MV-CS240-90UM)` uses the fixed chart_hik2 matrix. `Off` is a true bypass. Metadata with `camera_applied: true` stays Off so a baked matrix is not applied twice.
2. **Density In.** `density = -log10(pixel / film_base)` (plus the documented black / white-code handling). Film base defaults to 0.50/0.50/0.50 and can be loaded from `nss.scan/1` or a sidecar JSON. An `after_profile` reference is not multiplied by the camera matrix again.
3. **Inversion / Positive Out.** Per-channel film-stock gammas, optional Film balance (T/D) and scene-illuminant offsets, Exposure, and Gamma scale. Visible colour balance sliders (defaults 1.07 / 0 / 0.67, the eye-tuned 500T Base) trim channels. User-saved balance presets live in `%APPDATA%\NegativeScanStudio\balance_presets.json`.
4. **Contrast** (optional: Off / Low / Medium / High).
5. **Tone** (0.90.12, on by default): highlight shoulder (RGB mean, ratio-preserving) and shadow toe (darkest channel, hue-preserving). Switches, then `Tone preset`, then sliders.
6. **Tone Color** (0.90.14, on by default, after Tone): highlight desaturation toward Rec.709 luminance, then highlight cream (warm R/B axis, luminance-preserving, measured on the desaturated pixel), then shadow desaturation. Switches, then `Tone color preset`, then sliders. Both groups off (or all amounts 0) render bit for bit like 0.90.11 inversion + contrast only.
7. **Output encoding:** Rec.709, DaVinci Intermediate, Cineon (default), ACEScct, or Linear.
8. **Print LUT (in Resolve, after the effect, not inside the plugin):** Kodak 2383 for Vision3 500T / 250D; `NSS_StillPaper_v1.cube` for still stocks.

Stops on the Tone / Tone Color controls are `log2(value / 0.18)` on the scene-linear positive, so they move with Exposure and the balances. Region weights for colour reach full strength 1.5 stops past each start (smoothstep).

Rendering: CUDA PTX or OpenCL buffers when the host offers them, otherwise a multithreaded CPU fallback. Technical parameters are hidden with `kOfxParamPropSecret` and remain in the project for compatibility.

## Resolve setup

Recommended starting point (also used in the v1.08 app release notes):

1. Colour management: **DaVinci YRGB** (unmanaged). Timeline: DaVinci Wide Gamut / Intermediate. Output: sRGB (or whatever the delivery expects).
2. Import the **unconverted linear** scan TIFF. Do not bake a display LUT into the media.
3. Apply **NegativeScan Studio OFX**. Typical starting values: film base 0.50 (or Load from metadata), matching film stock, balances at the 500T Base or zero if you will grade from scratch, Output **Cineon**, Tone / Tone Color at Soft cinematic unless you are comparing against 0.90.11.
4. On a node after the plugin, apply the print LUT:
   - Vision3: `Kodak_2383_D65.cube` (or D55/D60).
   - Still films: `NSS_StillPaper_v1.cube`.
5. Copy those `.cube` files into Resolve’s LUT folder, then refresh LUTs:

```text
C:\ProgramData\Blackmagic Design\DaVinci Resolve\Support\LUT\
```

Repo copies: `develop/luts/Kodak_2383_D65.cube` and `/workspace/nss-archive/references/`. The 2383 LUT renders neutral blacks slightly cyan and neutral highlights slightly yellow; that is the LUT, not residual scanner colour, and it is a known limit of the default look.

Enable **Open FX Overlay** in the viewer if you use Pick gray on viewer. Back up the project before replacing plugin bundles. Real Resolve behaviour of this 0.90.14 build has not yet been signed off; see [STATUS.md](STATUS.md).

## Metadata contract

Scan provenance is JSON in TIFF tag 270 (`ImageDescription`), schema `nss.scan/1`. `light_rgb` is the lamp drive value, not normalised film-base RGB. `film_base` is the accepted after-profile leveling result when present. Missing values are omitted, never guessed. Full field rules, loading behaviour, and historical notes: [OFX.md](OFX.md#data-contract).

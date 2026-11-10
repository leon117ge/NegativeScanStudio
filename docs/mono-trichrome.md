# Mono trichrome and focus-shift correction

First version (app v0.97-mono-preview). Green is the focus reference for
every measurement and for every correction.

## Camera

The page talks to a Hikrobot Machine Vision camera through the official
MVS SDK (`MvCameraControl.dll`), loaded at runtime with `LoadLibrary`.
The application starts even when the DLL is missing; Connect then reports
that the SDK is not installed and the built-in simulator stays available.

Intended body: **MV-CS240-90UM** (24 MP USB3 monochrome, Gpixel GMAX2424,
6016 x 4096, rolling shutter, Mono 8/10/10Packed/12/12Packed, exposure
32 us to 10 s, gain 0 to 24 dB). The color sibling is MV-CS240-90UC.
USB3 and GigE devices are both enumerated, so a GigE variant would open
the same way.

Sources used while identifying the model:

- [LumaSpec MV-CS240-90UM](https://lumaspecvision.com/products/lumaspec-mv-cs240-90um)
- [Hikrobot datasheet MV-CS240-90UM/UC (2026-10-15)](https://lumaspec-media2.pages.dev/downloads/hikrobot/MV-CS240-90UMUC_20261015.pdf)
- [Hikrobot CS series product list](https://www.hikrobotics.com/en/machinevision/visionproduct/?id=134&pageNumber=1&pageSize=50&typeId=78)
- MVS client / SDK overview: [https://mvisionpro.com/hikrobot/mvs/](https://mvisionpro.com/hikrobot/mvs/)
- Official SDK download: [https://www.hikrobotics.com/cn/machinevision/service/download/?module=0](https://www.hikrobotics.com/cn/machinevision/service/download/?module=0)

GenICam features written by the settings panel: `ExposureTime`, `Gain`,
`PixelFormat` (Auto prefers Mono12, then Mono10, then Mono8), `BlackLevel`,
`GammaEnable` (default off), `Width` / `Height` / `OffsetX` / `OffsetY`
(or full frame), `TriggerMode` / `TriggerSource` (`Software`), then
`StartGrabbing` + `GetImageBuffer` or `TriggerSoftware`. Native 8/10/12-bit
buffers are unpacked and left-justified into 16-bit linear samples.

## Capture

The RGB Light Air is switched the same way as the existing Fuji trichrome
page (`rgblight.ChannelSwitch.Only`): snapshot the pre-scan state, light
R only, settle, grab; then G; then B; restore the light. The three gray
planes are packed as R, G and B of one linear 16-bit TIFF with `nss.scan/1`
metadata, `mode=mono-trichrome`. Film-base leveling is copied from the
Light page when the saved values match, exactly as white-light and
trichrome captures do.

## Chromatic-aberration focus-shift correction

A lens focused in green light typically leaves red and blue both
laterally magnified (or reduced) and axially defocused.

1. **Translation.** Coarse-to-fine search of the normalized
   cross-correlation between the channel and green, then a 3-point
   parabola for a sub-pixel peak. Reported as `dx`, `dy` in full-resolution
   pixels (positive `dx` means the channel's features sit to the right of
   green).
2. **Scale (lateral CA).** After the translation is known, a search of
   magnification about the image centre in roughly ±0.8 % finds the scale
   `s` that maximises NCC. `s = 1` is no lateral CA.
3. **Blur (longitudinal CA).** After a rough alignment, each plane's PSF
   width is estimated as `1 / sqrt(Tenengrad)`. `BlurRatio` is that width
   over green's. A value above 1 means the channel is softer.
4. **Correction.** Red and blue are resampled (bilinear) with the inverse
   transform: scale about the centre, then the measured translation.
   Green is never warped. If `BlurRatio > 1.05`, a Gaussian unsharp of
   width `sqrt(ratio^2 - 1)` is applied and the amount `k` is raised only
   while Tenengrad keeps increasing, capped at 1.5. Green is never
   sharpened.
5. **Report.** The UI and the TIFF `Processing` field list each channel's
   shift, scale, blur ratio and whether sharpness matching ran.

The estimator downsamples to a long edge of 800 px so a 24 MP frame stays
interactive; shifts are converted back to full-resolution pixels before
the warp.

Synthetic tests (`hdrscan/focusshift_test.go`) build a sharp green pattern,
then a red/blue plane with a known shift, scale and Gaussian blur, and
check that the estimator recovers those parameters and that correcting
them raises NCC substantially without changing green.

## Focus check

Live view from the Hikrobot body if it is connected, otherwise from the
existing tethered camera's JPEG live view. Zoom is Fit / 100 % / 200 % /
400 %, with click-to-centre and drag-to-pan. The RGB light can be set to
green only (trichrome baseline) or white (all three channels).

While the page is visible, `AdjustExposure` looks at the histogram of the
visible (zoomed) region: it drives the 90th percentile toward 0.42 of
full scale and will not let the 99.8th percentile sit above 0.90. Shutter
is preferred; gain rises only after exposure time is maxed. The previous
capture exposure (and the light state) are restored when the page is left.

The sharpness meter is the variance of a 4-neighbour Laplacian plus a
smaller term of mid/high-frequency energy (the band where resolved film
grain lives), with peak-hold.

## Best scan quality (MV-CS240-90UM)

v1.01 adds a preset dropdown on the Mono trichrome page. Selecting
**Best scan quality (MV-CS240-90UM)** fills the camera settings and, once
applied, Capture meters each channel automatically (you do not set
exposure by hand). Values and reasons:

| Setting | Value | Why |
| --- | --- | --- |
| PixelFormat | Mono12 | Highest native bit depth the GMAX2424 offers (4096 codes), unpacked and left-justified to 16-bit linear. |
| Gain | 0 dB | Base / lowest analog noise. Brightness comes from exposure time, not from amplifying readout noise. |
| BlackLevel | 32 | A small pedestal (~0.8 % of 12-bit full scale) so the noise floor is not clipped at code 0. |
| GammaEnable | Off | Linear radiometric response required for HDR merge and film-base arithmetic. |
| Geometry | Full frame 6016 x 4096 | Use every photosite; no ROI crop. |
| Trigger | Software | Grab after the RGB light has settled, not a free-running rolling frame. |
| ExposureAuto / GainAuto | Off | The software meters each R, G and B channel so highlights (film base / rebate) do not clip. |
| DigitalShift | 0 | Do not bit-shift the raw numbers inside the camera. |
| Sharpness / denoise / LUT | Off | Do not process the raw numbers inside the camera. |
| ADCBitDepth | 12 (when exposed) | Match the Mono12 readout. Nodes the firmware does not implement are ignored. |

Starting ExposureTime is 10 ms. At capture time the software probes each
lit channel, drives the film-base median (the user-drawn rectangle if one
exists, otherwise the frame) toward 0.50, and keeps the full-frame P99.5
at or below 0.88 so highlights do not clip.

Light leveling still uses a *fixed* camera exposure (only the RGB Light
Air changes). Capture metering is a separate safety/primary step so a
scan does not clip if the light or the film changed after leveling. The
saved 0.5 film-base metadata comes from the last accepted leveling run.

## HDR (v1.01)

Mono trichrome can capture an HDR bracket per channel, using the same
bracket list (0 / +2 EV or 0 / +2 / +3 EV), merge method, alignment and
LZW options as White light scan and Trichrome. For each of R, G and B
the monochrome camera captures the bracket, the frames are written as
16-bit RGB TIFFs (gray replicated) and merged with the existing
`ProcessN` path, then chromatic-aberration focus-shift correction (green
reference) and the trichrome merge run as before.

## Light leveling with the monochrome camera (v1.01)

The Light page has a **Leveling camera** selector:

- Existing camera (Fujifilm) — previous X-T3 tethered path, including
  the optional CameraCal matrix.
- Hikrobot monochrome — the MV-CS240-90UM shared with Mono trichrome.
- Simulated Hikrobot MV-CS240-90UM — no MVS SDK; used by tests and when
  no body is connected. Simulated frames are scaled by the commanded
  RGB light codes (same LED curve as the existing light simulator) so
  the v0.92 solver (10 rounds, measured-slope secant, neighbour search)
  can converge.

Live view on the Light page shows the selected camera so the usual
orange-rectangle film-base pick still works. Auto leveling measures
each light channel through the mono camera (every pixel belongs to the
lit channel; no Bayer extraction). **Apply camera profile** is disabled
for a monochrome body: there is no CFA and no CameraCal matrix, so each
raw film-base channel is leveled independently to the 0.5 target. The
TIFF check merges the last R/G/B mono frames with the same
`ProcessMonoTrichrome` pipeline used by capture. Accepted after-profile
(here: raw, because the matrix is identity) base values are stored in
the Light page leveling record and flow into Mono trichrome TIFF
metadata the same way as existing trichrome (`nss.scan/1`,
`WithLeveling`).

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

## Chromatic-aberration correction (v1.09)

Green is the geometric reference. R and B are measured against green on a
full-resolution 19 x 13 tile grid (band-pass, NCC search, Lucas-Kanade),
a robust radial lateral-CA model (r and r^3 terms, separate x / y) plus a
smooth local refinement is fitted, and the channel is resampled with a
Kaiser-windowed sinc that never boosts any frequency. Pixels needing less
than 0.12 px are copied unchanged. No sharpening is applied. A centre and
corner guard falls back to identity per channel.

Since v1.10 the report is shown on the page and stored in the TIFF
metadata as `mono_capture.ca_correction`. The `processing` field stays
empty: the OFX plugin only loads scans whose `processing` is empty.

## R/G/B movement check (v1.10)

Before CA correction the global translation of R and B against G is
measured at quarter resolution on a 3 x 3 grid of large tiles in the
centre 60 % of the frame (median of the confident tiles). Above 1.5 px
the page and log show a WARNING and `mono_capture.movement_check` records
it. The scan is not corrected for movement. The lens alone puts a small
constant offset between channels (about 1.0 px B vs G in scan 0281), so
the limit sits above that.

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

## Black level and HDR (v1.10, v1.11)

Every frame still contains the camera BlackLevel pedestal (Best scan
quality: 32 DN at 12-bit = 512 in left-justified 16-bit). Since v1.10 it
is subtracted from every frame before merging, and the Light-page
leveling measures the same black-subtracted values, so the leveled 0.5
film base and the scan agree. Clipping is still judged on the raw code.
There is no dark-frame or flat-field step (removed in v1.11): the noise
model comes from the camera's published specifications instead.

**HDR.** Each channel's bracket is merged on the mono plane in memory
(`hdrscan.MergeMonoNative`):

- the brightness ratio of each frame to the previous bracket step is
  measured on 16 x 16 blocks in the linear range of both (fallback:
  exposure-time ratio) and chained back to frame 0 (v1.11; v1.10 compared
  every frame with frame 0, which leaves little overlap for a +5 EV frame);
- inverse-variance weights from a shot + read + quantisation noise model
  with the MV-CS240-90UM sensor values below;
- a saturation hat on the value predicted from frame 0 and zero weight at
  the raw white code; no clip-mask dilation (no demosaic halo);
- 5-7 sigma ghost rejection against the darker estimate, with the noise
  scale re-measured from the frames (v1.11: a step with too few unclipped
  samples keeps the previous step's scale, and a scale far above the
  model is capped at 50 instead of falling back to 1).

The result is linear, black-subtracted and in frame-0 units.

**Frames per exposure** averages N same-exposure grabs per bracket step
(default 1).

**Light settle check.** After switching the light and the fixed settle
wait, frames are grabbed until two consecutive means agree within 0.3 %
(timeout 3 s or 4 x settle). A light that does not settle is logged as a
warning and the capture continues.

## Sensor model (v1.11)

Published values only; nothing is measured on the user's camera.

| Quantity | Value | Source |
| --- | --- | --- |
| Sensor | Gpixel GMAX2424, 2.4 um pixels, 6016 x 4096, rolling shutter | Hikrobot datasheet MV-CS240-90UM/UC; Hikrobot 2025Q4 catalog |
| Camera dynamic range / SNR | 72 dB / 41 dB | same |
| Full well | 12.1 ke- (12-bit, 1x analog gain) | Gpixel GMAX2424 product page |
| Read noise | 2.7 e- (12-bit) | Gpixel GMAX2424 press release (product page: 1.3 e- at 16x gain, 2.4 e- at 10-bit 8x) |
| Sensor dynamic range / max SNR | 73 dB / 40.8 dB | Gpixel product page |

Links:

- [Hikrobot datasheet MV-CS240-90UM/UC](https://lumaspec-media2.pages.dev/downloads/hikrobot/MV-CS240-90UMUC_20261015.pdf), [LumaSpec product page](https://lumaspecvision.com/products/lumaspec-mv-cs240-90um)
- [Hikrobot Machine Vision Standard Product Catalog 2025Q4](https://www.hikrobotics.com/en2/source/vision/video/2026/1/20/Machine%20Vision%20Standard%20Product%20Catalog_2025Q4.pdf)
- [Gpixel GMAX2424 product page](https://www.gpixel.com/en/pro_details_11311.html), [Gpixel GMAX2424 press release](https://www.gpixel.com/en/details_216.html)

Checks: 20 log10(12100 / 2.7) = 73.0 dB and 20 log10(sqrt(12100)) =
40.8 dB match the sensor sheet. If full well fills the 12-bit range above
the pedestal, one 12-bit step is about 3.0 e- (quantisation 0.86 e- rms);
2.7 e- read plus quantisation gives a 2.83 e- floor, 72.6 dB, which
matches the camera's 72 dB. Assumptions: camera gain 0 dB is the sensor's
1x analog gain, and the body uses the sensor at its 12-bit, 1x settings.
The model replaces the v1.10 estimates (9000 e-, 2.0 e-); the old
`HIK_FULL_WELL_E` / `HIK_READ_NOISE_E` config keys are ignored.

A real-file consistency check: in scan 0281 (v1.08, 0/+2/+3) the
flattest film-base-level areas of R show 1.66 % pixel-to-pixel noise at
0.47 of full scale. Pure shot noise at 12.1 ke- full well would be 1.33 %;
film grain adds the rest. The measurement only shows that the effective
full well is at least about 7,800 e-, so it is consistent with the sheet
but is not a measurement of it.

## Default HDR bracket: 0 / +2 / +3 / +5 EV (v1.11)

The Mono page now starts in HDR mode with 0 / +2 / +3 / +5 EV; 0 / +2 and
0 / +2 / +3 stay selectable (`HIK_MODE`, `HIK_BRACKET` in the config). The
White light and Trichrome pages keep their own list.

Reasoning with the sensor model, film base leveled to 0.5 of full scale
(frame 0 holds about 6,050 e- at the base) and densities up to 2.4 above
base (dense highlights of Vision3 500T and still negatives):

- thin parts of the negative (the positive's shadows, 0-1.2 D) are already
  covered well by 0 / +2 / +3;
- the dense parts (1.5-2.4 D, the positive's highlights) get only 25-190
  e- per pixel in frame 0, so the 2.7 e- read noise and quantisation still
  count; +5 EV puts 32x the light there;
- the bracket contains 0 / +2 / +3, so no density gets worse;
- 0 / +3 / +5 is a little more efficient per second but drops the +2 frame
  and loses about 40 % SNR at 0.45-0.6 D; 0 / +3 / +6 is 1.5x slower.

Per-pixel SNR on the sensor model (ideal inverse-variance merge, same
numbers as the merge test `TestDefaultBracketDenseSNR` within noise):

| D above base | 0 | 0.3 | 0.6 | 0.9 | 1.2 | 1.5 | 1.8 | 2.1 | 2.4 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| single frame | 78 | 55 | 39 | 28 | 19 | 14 | 9 | 6 | 4 |
| 0 / +2 | 78 | 55 | 87 | 62 | 44 | 31 | 22 | 15 | 10 |
| 0 / +2 / +3 | 78 | 55 | 87 | 99 | 70 | 50 | 35 | 25 | 17 |
| **0 / +2 / +3 / +5** | 78 | 55 | 87 | 99 | 70 | **93** | **65** | **46** | **33** |

Density reach for SNR 20: 2.27 D (0 / +2 / +3) to 2.81 D (0 / +2 / +3 /
+5); for SNR 10: 2.82 D to 3.36 D. Engineering range: 12.1 stops per
frame, 17.1 stops with the +5 EV frame.

Capture time (estimate shown on the Mono page): per channel the sum of the
exposures plus one frame readout per grab. Readout is not in the
datasheet; 135 ms is derived from 24.6 MP x 2 bytes at about 365 MB/s
(USB3; the sheet gives 15 fps in Mono8). At the 14.3 ms base exposure of
scan 0281: 0 / +2 about 1.0 s for R, G and B, 0 / +2 / +3 about 1.8 s,
0 / +2 / +3 / +5 about 3.6 s, plus light settling and the merge. The
+5 EV frame is 32x the base exposure (459 ms at 14.3 ms; capped at the
camera's 10 s).

## Comparison with the X-T3 path (v1.11, model)

A noise model of both, not a side-by-side scan of the same frame (no X-T3
scan of the same negative is on hand). X-T3 at ISO 160: read noise 1.815
DN = 3.6 e-, full well about 30,100 e-, 14-bit (Photons to Photos data via
[this summary](https://alpereninyeri.com/araclar/noise-testi/camera/fujifilm-x-t3);
[Optyczne.pl](https://www.optyczne.pl/411.8-Test_aparatu-Fujifilm_X-T3_Zakres_i_dynamika_tonalna.html)
estimates more). X-Trans: 26 MP (6240 x 4160), green on 20 of 36 sites,
red and blue on 8 of 36 each. The X-T3 is modelled at its best: ideal
merge, every channel base leveled to 0.5, no colour crosstalk penalty.
Per-channel SNR is normalised to the mono 24.6 MP grid (SNR per site x
sqrt(site fraction x 26.0 / 24.6)).

| D above base | 0 | 0.6 | 1.2 | 1.8 | 2.4 |
| --- | --- | --- | --- | --- | --- |
| Mono, any channel, 0 / +2 / +3 / +5 | 78 | 87 | 70 | 65 | 33 |
| X-T3 R or B, 0 / +2 / +3 | 59 | 67 | 54 | 27 | 13 |
| X-T3 G, 0 / +2 / +3 | 94 | 105 | 85 | 42 | 21 |
| X-T3 R or B, 0 / +2 | 59 | 67 | 33 | 17 | 8 |

- Red and blue: the mono path is 1.3x better in thin areas and 2.4x
  better in dense areas than X-T3 0 / +2 / +3 (3.9x vs X-T3 0 / +2).
- Green: the X-T3's larger well wins by about 1.2x in thin and mid areas;
  the mono path wins from about 1.3 D up (1.5x at 1.5 D and above).
- Density reach for SNR 20: mono 2.81 D; X-T3 0 / +2 / +3 2.05 D (R, B)
  and 2.44 D (G).
- Resolution: every mono pixel is a measurement for each channel (24.6 M
  samples per channel, nothing interpolated, no colour crosstalk between
  filters). The X-T3 measures red and blue on 5.8 M sites each and green
  on 14.4 M; the rest is demosaiced. The luminance resolution limit is
  set by the lens at f/4: on the mono body (magnification about 0.41) the
  Airy disc on the film is about 18.5 um (3.2 px) and diffraction MTF at
  the sensor's Nyquist (85 lp/mm on the film) is about 0.24; on the X-T3
  (magnification 0.65, same lens and f-number) it is 13.6 um and 0.41 at
  87 lp/mm. So the gain is in colour-channel resolution and dense-area
  noise, not in luminance sharpness; f/2.8 would lower diffraction if
  the EX50 is sharp enough wide open (not tested).

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

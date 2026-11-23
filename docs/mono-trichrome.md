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

## Black level, HDR and calibration (v1.10)

Every frame still contains the camera BlackLevel pedestal (Best scan
quality: 32 DN at 12-bit = 512 in left-justified 16-bit). v1.10 subtracts
it (or the matching dark frame) from every frame before merging, and the
Light-page leveling measures the same black-subtracted values, so the
leveled 0.5 film base and the scan agree. Clipping is still judged on the
raw code.

**HDR.** Each channel's bracket (0 / +2 or 0 / +2 / +3 EV) is merged on
the mono plane in memory (`hdrscan.MergeMonoNative`):

- exposure ratio of each brighter frame to frame 0 measured on 16 x 16
  blocks in the linear range (fallback: exposure-time ratio);
- inverse-variance weights from a shot + read + quantisation noise model
  (config `HIK_FULL_WELL_E`, `HIK_READ_NOISE_E`; defaults 9000 e- and
  2.0 e-, to be replaced by measured values);
- a saturation hat on the value predicted from frame 0 and zero weight at
  the raw white code; no clip-mask dilation (no demosaic halo);
- 5-7 sigma ghost rejection against the darker estimate, with the noise
  scale re-measured from the frames.

The result is linear, black-subtracted and in frame-0 units.

**Frames per exposure** averages N same-exposure grabs per bracket step.

**Light settle check.** After switching the light and the fixed settle
wait, frames are grabbed until two consecutive means agree within 0.3 %
(timeout 3 s or 4 x settle). A light that does not settle is logged as a
warning and the capture continues.

**Dark frames and flat field.** "Capture dark frames..." switches the
light off and averages 16 grabs at the shared exposure and every bracket
exposure. "Capture flat field..." shoots the empty gate under R, G and B
at the scan light values (exposure set to about 45 % of full scale),
subtracts the dark or pedestal and stores a gain map normalised to 1.0 at
the centre median. Files live in `%AppData%/NegativeScanStudio/hikcal`
(`.nsscal`: header JSON + 16-bit pixels). They are used only when "Apply
dark frames" / "Apply flat field" are checked and a frame with the same
exposure (darks), pixel format and size exists; ids go into
`mono_capture.dark_frames` / `flat_field`. Darks do not match a scan made
with per-channel auto-metering (different exposures); the pedestal is then
subtracted and the report says so.

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

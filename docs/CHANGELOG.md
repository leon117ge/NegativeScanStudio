# NegativeScan Studio app — Changelog

Current app: **v1.13** · [App release](https://github.com/leon117ge/NegativeScanStudio/releases/tag/v1.13) · [OFX plugin changelog](../ofx/CHANGELOG.md) · [Versioning policy](VERSIONING.md) · [Status](STATUS.md)

This file tracks the scanning app, not plugin-only releases.

## Unreleased: macOS Apple Silicon core (work in progress, no release yet)

Core packages plus the first increment of the Mac UI (Gio). Capture sequence, timing and image-processing algorithms are unchanged.

- **LibRaw on darwin/arm64.** `rawconv` links a static LibRaw 0.22.1 + zlib 1.3.1 built from pinned, sha256-checked sources by `ci/macos/build_libraw.sh` (same version and flags as the Windows and Linux libraries). CI decodes a public sample RAF on macOS, Linux and Windows and compares the 16-bit output.
- **RGB Light Air on macOS, no driver.** The light (USB 0483:5740, STM32 virtual COM port) uses the built-in macOS CDC-ACM driver and appears as `/dev/cu.usbmodem*`. `rgblight` opens the `cu.*` node (never `tty.*`) at 115200 8N1 raw with DTR/RTS on, exclusive access and the same 20 ms read wait as on Windows, and tags ports by USB VID/PID through IOKit (or `ioreg` without cgo).
- **No select() panic on a high descriptor.** The macOS serial port waits with select(2) (poll(2) does not support device nodes on macOS); a descriptor at or above FD_SETSIZE (1024) now returns an error instead of panicking in `FdSet.Set` (W4 review LOW-6).
- **`cmd/rgblightports`** light tool: `list`, `info`, `status`, `on`/`off r|g|b|all`, `brightness r|g|b|all N`, `setall R G B`, with `-port`, `-raw` (serial log) and `-sim`.
- **Hikrobot MVS cameras on macOS (`hikcam`, darwin with cgo).** The Hikrobot backend now also builds on macOS. It loads Hikrobot's MVS SDK for macOS (MVS 5.0.0 / SDK Runtime 4.7, universal arm64 + x86_64, macOS 11 or later) at run time from `/Library/Frameworks/MvCameraControl.framework/MvCameraControl`, or from the path in `NSS_MVS_LIB`. Nothing links against the SDK and the app ships none of it: without the SDK the camera list is empty with `ErrNoSDK` and a message naming the framework, and the app keeps running. Same `hikcam` interface, node writes and grab loop as Windows (`mvs_darwin.go` mirrors `mvs_windows.go`, and a test fails if they drift); the one deliberate difference is the frame struct, because clang on macOS makes the SDK's pixel-type enum 64-bit. The Windows files (`mvs_windows.*`) are byte-identical and the Windows build is unchanged. CI (`macos-hikcam.yml`, macos-15 and macos-26) tests against `ci/macos/mvs_stub.c`, a stand-in library that simulates one USB3 camera; it is not the Hikrobot SDK. Not yet tried with a real camera on a Mac.
- **X-T3 tether on macOS (ImageCaptureCore), first step.** New transport `camera/icc*.go` (portable PTP container code plus an Objective-C layer, `camera/icc_darwin.m`) behind the same `Transport` interface as Windows WPD, so the unchanged Fuji logic in `camera/fuji.go` runs on top. Commands go through `ICCameraDevice requestSendPTPCommand:outData:completion:`, so the system camera daemon is not fought. New `cmd/mactether` (`list`, `info`, `lv`, `capture`, `-sim`; see [MACTETHER.md](MACTETHER.md)) for a one-time hardware check. A simulated X-T3 behind the same pass-through drives the unit tests and CI. The Windows WPD path, its files and capture timing are unchanged. Not yet run with a real camera.
- **mactether builds with the real macOS SDK.** `camera/icc_darwin.m` no longer calls the ImageCaptureCore control/contents authorization API, which is iOS/iPadOS only and failed the macOS 15 and 26 builds; on macOS `ICCAuthorization` now reports authorized without calling it. The session delegate implements the required `device:didOpenSessionWithError:` and `device:didCloseSessionWithError:`, and opening or closing waits once for whichever of the completion block or the delegate call arrives first. The box check's stub SDK headers now copy the real declarations and availability (iOS-only API unavailable on macOS, required delegate methods required) and self-test that they reject both mistakes. No Windows changes.
- **macOS user interface in Gio (W9, increment 1).** A new front end in `macapp/` (own Go module, never part of the Windows build) rebuilds the window of the Windows app with Gio: same header, four modules in the bottom page bar, segmented sub-pages, dark theme with the orange accent, and cards with the same rows and control order. Ported: Camera & Light > Camera (camera, metering, live view, log) and HDR Scan > White light scan (Capture and merge), wired to `camera` and `hdrscan` exactly as on Windows (connect, live view, status poll, Meter base, Test shot, Capture HDR, merge, Stop). The camera list uses the ImageCaptureCore X-T3 transport. The other pages are placeholders. Native file and folder panels (NSOpenPanel / NSSavePanel), crash log in `~/Library/Logs/NegativeScanStudio`, settings shared with the Windows format, `NegativeScan Studio.app` packaging (macOS 13.0 or later, icon from the Windows icon) and a macOS CI job (macos-15, macos-26) that builds the app, renders the pages through Metal and checks that the main run loop keeps delivering callbacks under Gio (needed by ImageCaptureCore). Camera I/O never runs on the main thread. See [MAC_UI.md](MAC_UI.md). The Windows UI and build are unchanged.
- **macOS Light page (W9, group a).** Camera & Light > Light is ported to the Gio UI: RGB Light Air connect, port list (`/dev/cu.*`, the light listed first) and simulators, channels (Apply / Read state / All off), automatic film-base leveling with the same steps, thresholds, TIFF checks, final state check, `leveling_log.csv` / `leveling_failures.csv` records and frame-0 handoff as Windows (X-T3, Hikrobot MV-CS240-90UM, Hikrobot simulator, simulated light), scan config presets (store, file save / load through the native panels, apply with handoff), the faults pane, the last leveling captures with the film-base rectangle drag, and the 500T illuminant calculator. A test runs a full leveling through the page and checks it step by step against the Windows sequence. The left preview column now fits the window at 1280x800 and above (the window opens at 1280x800). Leveling metadata and the Mono trichrome settings come in later increments. Windows build unchanged.
- **macOS Crop & Merge pages (W9, group b).** Crop, Merge files, Remove hairs and Batch RAW to TIFF are ported to the Gio UI with the same layout, labels, control order and behaviour as Windows. Full-resolution crop, HDR merge, dust removal, hair healing and RAW-to-TIFF conversion stay in the existing Go packages on CPU worker goroutines (progress and cancel; the UI thread is never blocked). File and folder picks use the native panels. "Open in Develop" switches to Convert as on Windows; Convert itself is not ported yet, so the window shows the HDR path and a message. Tests include a golden comparison against the Windows code path. Windows build unchanged.
- **macOS Trichrome and Mono trichrome pages (W9, group c).** HDR Scan > Trichrome (X-T3 and RGB Light Air) and Mono trichrome (Hikrobot MV-CS240-90UM through the W7 backend, or its simulator) are ported to the Gio UI with the same layout, labels, control order and behaviour as Windows: the same light sequencing, exposures, capture and merge flow and outputs, with camera and light I/O on worker goroutines. Without the Hikrobot SDK the camera is shown as unavailable with the reason. Light leveling now takes the Hikrobot exposure from the Mono trichrome page, as on Windows, instead of the best-quality preset. Trichrome and Mono trichrome write the last accepted leveling into the scan metadata. Golden tests compare both pages with the Windows sequence on the simulated light and cameras. The macOS CI job downloads a pinned uncompressed X-T3 RAF (raw.pixls.us, CC0) for the RAW golden tests. Fixed a data race between the camera worker and the UI on the frame-0 lock. Windows build unchanged.
- **macOS Focus check page (W9, group d).** HDR Scan > Focus check is ported to the Gio UI with the same layout, labels, control order and behaviour as Windows: live loupe (Fit, 100 %, 200 %, 400 %, click to zoom, drag to pan), centre marks that follow zoom and pan, green-only or white light, automatic live exposure on the visible region with the capture exposure and light restored on leave, and the sharpness meter with peak hold. The Hikrobot camera (or simulator) is preferred, the X-T3 live view is the fallback. Camera I/O and the focus metric run on worker goroutines. Golden tests compare the focus metric, auto exposure and every camera call with the Windows code path on the same frames, exactly. Leaving Focus check for Mono trichrome now restarts the Mono preview after the restore (on Windows the restore cancels it). Windows build unchanged.
- **Gio unit tests no longer run a real ImageCaptureCore device scan.** `Ctl.Start` used to queue an ICC listing that waits the 5 s timeout in `go test` (no main run loop), so Trichrome connect waits failed on macos-26. Tests now leave `ListDevices` unset, as on Linux; ICC listing is still covered with stubs. Windows build unchanged.
- **Mono trichrome golden test made deterministic (W9).** The Mac Mono trichrome page has a test-only floor for the light settle-check timeout (0 in the app, so the capture uses the Windows timeout unchanged). The golden test raises it on both sides so a loaded machine cannot end the settle check on the wall clock, and it now cuts the recorded calls at the capture's own StopGrab instead of the last one, so a live view restarted after the capture is not counted (the test now always waits for that live view and checks the split-off calls are a live view run). A new `metered-slow-grab` case slows the first grab after each light switch past the 3 s default to prove the result no longer depends on timing. No change to capture behaviour or the Windows app.
- **Convert is not part of the macOS app (user's decision).** The Gio page bar has three modules (Camera & Light, HDR Scan, Crop & Merge); the Convert placeholder tab, the `--develop` / `--convert` switches and the Merge files "Open in Develop" button (it only led to that placeholder) are removed from the Mac app. The merged HDR TIFF is still saved next to frame 0 and can be opened in the Windows app or graded with the OFX plugin. The Windows app keeps Convert; Windows build unchanged.
- **No Develop buttons on the macOS app.** "Send base to Develop" (White light and Trichrome) and "Send to Develop" (500T card) are removed from the Mac pages: on Windows their only consumer is the Develop / Convert hook in `main.go`, which shows a message and saves nothing. The 500T card is now titled "500T illuminant" and its result text no longer says "Send to Develop to apply". `docs/MAC_UI.md` gains a Known follow-ups section. Windows build unchanged.
- **macOS Chart calibration and leveling metadata switch (W9, group f, slice 1).** Camera & Light > Camera > Chart calibration... is ported to the Gio UI as an in-window panel with the same rows, labels, tooltips and control order as the Windows dialog: chart TIFF (or the last Trichrome scan), CGATS reference, automatic grid detection, Rotate 180°, Reset grid, draggable corner markers with the clip overlay, Fit with the same report, and the camera profiles in the shared `camera_profiles.json` (save, replace, active profile, Enable, Strength, delete). Loading, detection, sampling and the fit run on worker goroutines. The Camera page switch "Film base for metadata: Last leveling (after profile)" now works as on Windows. Tests compare the fit report and the saved profile file with a replay of the Windows dialog on synthetic charts. Windows build unchanged. The film-base DB tools and the Fixed frame 0 film-stock comparison follow in slice 2.
- **macOS film-base DB tools (W9, group f, slice 2).** On the Camera page, Film stock, Tol., Record base, Save base to DB, Save last frame 0 to DB and Manage work as on Windows: the save and Manage dialogs are in-window panels with the same rows, cue banners, messages and log lines, Record base shoots the standalone fixed frame 0 and keeps the BASE RAF, and Save last frame 0 to DB takes the last capture's clean frame-0 base. The DB is the same `film_base_db.json` (file name and format as on Windows) in `~/Library/Application Support/NegativeScanStudio`, and a first start imports the Film base presets as on Windows. Tests replay the dialogs against the shared `filmdb` and run Record base and the saves end to end on a real RAW. Windows build unchanged. The Fixed frame 0 film-stock comparison of a metering or capture is not on macOS yet.
- **macOS Fixed frame 0 film-stock comparison (W9, group f, slice 3).** With Frame 0 = Fixed, the selected film stock is checked as on Windows: after Meter base (the line under the base readout and in the log, and the "Frame 0" notice in the window when the check fails), again from the locked probe when the stock is changed after the lock, and as the last line of a capture result with a clean frame 0. "Film stock: None (no DB check)" without a stock. The "not on macOS yet" placeholders are removed. Tests cover pass, fail and no stock at each place on a real RAW. Windows build unchanged.
- **macOS parity audit (W9).** `docs/MAC_PARITY.md` checks every Windows control, message box, tooltip, command-line switch and window-level behaviour outside Convert / Develop against the Mac app: 369 of 396 were already ported, 18 are intentionally different (each with the reason), 7 are ported now and 2 are listed for a decision (dropping files on the window, Enter / Escape in the in-window dialogs). Ported now: `--dust light|strong <tiff> [...]` (output on standard output), the files after `--crop` are added to the Crop page, and the Windows tooltips of the White light, Trichrome, Mono trichrome and Crop result boxes and of the leveling captures. Each has tests. Windows build unchanged.

## v1.13

Focus Check auto-exposure stabilization (regression fix from v1.12). Capture sequence, light settling, HDR merge, CA correction, and image-quality algorithms are unchanged (OFX plugin untouched).

- **Settled Focus Check auto-exposure (`hdrscan.LiveAE`).** Added `LiveAE` to skip two settle frames after any hardware exposure/gain write so in-flight frames from the previous exposure no longer trigger feedback oscillation in the faster v1.12 preview loop, sync `State` to actual hardware exposure/gain (eliminating deadband and quantized-shutter sawtoothing), and lock exposure once converged (`locked`) until the user zooms, pans, switches the light, or scene brightness shifts substantially.
- **Tighter clip reduction and deadband alignment.** When highlights clip and the 90th percentile is also above target, `AdjustExposure` steps toward the darker ratio instead of taking slow 7% steps, and `applyAE` lowers its Hikrobot skip threshold from 4% / 0.15 dB to 1% / 0.05 dB so small valid adjustments are applied rather than blocked.
- **Stable Focus Check label layout.** Kept `Auto exposure` and `Sharpness` labels at a consistent two-line height, skipped redundant `SetText`/`SetToolTipText` layout passes when text/tooltip have not changed, and only updated the sharpness progress bar when its value changes.

Published 2026-11-24 with OFX 0.90.14. Earlier notes called this a local test build.

## v1.12 local test build

UI responsiveness, preview paint smoothness, and layout/text cleanup. Capture sequence, settling, exposure, HDR merge, CA correction, and image-quality algorithms are unchanged (OFX plugin untouched).

- **Non-blocking camera & light lifecycle.** Added `hikcam.Controller` so Hikrobot enumeration, connect/disconnect, settings apply, capture setup, Light Hikrobot leveling/preview, and Focus Check enter/leave/light-switch run off the UI thread. Tab switches, Connect, Apply, and Stop no longer freeze the UI thread, and preview start/stop is serialized so a new grab never starts before the previous loop exits `StopGrab`.
- **Smoother preview painting.** Cached GDI brushes, pens, and shared fonts across Mono, Focus Check, Light, and White Light previews (including center/flatness overlay marks), and moved `walk.NewBitmapFromImage` pixel conversion off the UI thread onto background preview goroutines while keeping frame-drop coalescing and `LivePreviewMaxEdge`.
- **Safe Mono Stop button.** Wired the Mono Trichrome Stop button to cancel cleanly between capture steps (`monocap.Options.Ctx`), restoring the RGB light and camera grab state without changing capture timing or output when Stop is not pressed.
- **UI text & clipping fixes.** Replaced `setStatusWide` on narrow panels with column-aware wrapping (`setMonoStatus` / `setFocusStatus`) and reserved heights; removed redundant parentheticals from Capture buttons (`Capture mono trichrome HDR` / `Capture mono trichrome`, `Capture trichrome HDR` / `Capture trichrome`, `Capture HDR`) and checkboxes (moved details into tooltips); removed the on-page `presetTip` paragraph on Mono; wrapped `camNoteLbl` on Light; shortened default Output boxes; fixed the Frames-per-exposure tooltip and ready-time wording to clarify that light settling is not included.
- Local only; no tag/push/release.

## v1.11 local test build

Monochrome camera (Hikrobot MV-CS240-90UM) defaults from the camera specifications, and a better default HDR bracket. OFX plugin unchanged (0.90.6).

- **Sensor model from published specs.** The mono HDR merge now uses full well 12.1 ke- and read noise 2.7 e- (Gpixel GMAX2424, 12-bit, 1x gain; cross-checked against the Hikrobot sheet's 72 dB / 41 dB). Replaces the v1.10 estimates (9000 e-, 2.0 e-). The `HIK_FULL_WELL_E` / `HIK_READ_NOISE_E` config keys are ignored. Sources and derivation in [mono-trichrome.md](mono-trichrome.md#sensor-model-v111). `mono_capture.noise_model` records the values used.
- **Dark frames and flat field removed.** No calibration buttons, checkboxes or `hikcal` files any more; black-level subtraction stays. `mono_capture` no longer has `dark_frames` / `flat_field`.
- **New default HDR bracket 0 / +2 / +3 / +5 EV**, and the Mono page now starts in HDR mode. On the sensor model the +5 EV frame roughly doubles SNR in the dense parts of the negative (1.5-2.4 D above base: 17 to 33 at 2.4 D, 35 to 65 at 1.8 D) and no density gets worse, because 0 / +2 / +3 is contained. 0 / +2 and 0 / +2 / +3 stay selectable. Mode, bracket and frames per exposure are saved (`HIK_MODE`, `HIK_BRACKET`). White light and Trichrome brackets are unchanged.
- **Capture time.** The ready line shows an estimate (exposures plus about 135 ms readout per grab): about 3.6 s for R, G and B at a 14 ms base, against about 1.8 s for 0 / +2 / +3. The report after a capture shows the measured capture time and the total time.
- **Merge robustness for wide brackets.** Exposure ratios are measured step to step and chained, so the +5 EV frame is checked against the +3 EV frame instead of frame 0. A step with too few unclipped samples keeps the previous noise scale; a scale far above the model is capped instead of falling back to 1 (that fallback rejected normal noise as ghosts).
- **Comparison with the X-T3 path** (model, not a same-frame scan) in [mono-trichrome.md](mono-trichrome.md#comparison-with-the-x-t3-path-v111-model).
- Local only; no tag/push/release.

## v1.10 local test build

Monochrome camera (Hikrobot MV-CS240-90UM) image-quality release. OFX plugin unchanged (0.90.6).

- **OFX metadata fix.** Mono-trichrome scans with CA correction no longer write the CA report into `processing` (the plugin rejected those files as "Not a linear NSS scan"). `processing` stays empty; the CA, HDR, settle and movement reports go into a new non-blocking `mono_capture` object together with pixel format, BlackLevel, black subtracted, frames per exposure and the dark / flat calibration ids. Verified with the plugin's own `metadata.hpp` parser (`ofx/tests/math_runner.cpp`).
- **Black level subtracted.** The BlackLevel pedestal (32 DN at 12-bit = 512 in 16-bit) is removed from every mono frame before merging, before the film-base leveling measurement and in the leveling TIFF check. Leveled bases and scans now agree on a black-free scale; output values are ~512 lower than v1.09 in 16-bit.
- **Native mono HDR merge.** The bracket is merged on the single mono plane (no grey-to-RGB TIFFs, no `mergeRGB`): measured exposure ratios, inverse-variance weights from a shot + read + quantisation noise model (`HIK_FULL_WELL_E`, `HIK_READ_NOISE_E` in the config; defaults 9000 e-, 2.0 e-), a saturation hat driven by the predicted value, raw clip at the white code with no dilation, and 5-7 sigma ghost rejection.
- **Frames per exposure** (default 1): averages N same-exposure grabs per bracket step.
- **Light settle check.** After the fixed settle wait, frames are grabbed until two consecutive means agree within 0.3 % (timeout 3 s or 4x settle); a light that never settles is logged as a warning.
- **R/G/B movement check.** Global R vs G and B vs G translation is measured at quarter resolution before CA correction; above 1.5 px an English warning is logged, shown on the page and stored in the metadata. Nothing is corrected.
- **Dark frames and flat field.** New Mono page tools capture averaged darks (light off, per exposure) and empty-gate R/G/B flats into `<config>/NegativeScanStudio/hikcal`. "Apply dark frames" / "Apply flat field" use them only when present; otherwise the scan is unchanged and the report says which calibration was missing.
- **Startup fix.** The new "Frames per exposure" NumberEdit used `Value: 1` (an int). walk stores NumberEdit.Value as an interface and only accepts float64, so it applied 0, which is below MinValue 1, and the app failed to start with "value out of range". The default is now `1.0`, other NumberEdit defaults are explicit float64, and a config value outside 1-16 is clamped before SetValue.
- Local only; no tag/push/release.

## v0.96 local test build

- Redo Convert grain: Poisson dye-cloud (fast+slow sublayers), transmittance local equalisation (mean/contrast neutral within 0.5%).
- Preview cache: decode/downscale once (~1600px), re-render on slider changes only; tiled+LUT CPU path. No app CUDA this build (CPU fallback).
- Grain strength slider. Sample crops in /workspace/grain_samples/.
- Local only; no tag/push/release.

## v0.95 local test build

- Convert uses full Kodak Status M D-logH CSVs (PCHIP) for Ektar 100 / Portra 400 / Gold 200 / UltraMax 400; Vision3 500T/250D keep logistic mid-slope fallback (no full polyline digitised yet).
- Output name: `<RAFStem>_<Stock>.tiff` (Portra400, Ektar100, …); auto `_2`, `_3` if taken.
- Film grain Off / ISO 100 / ISO 400 in density space (own model; spektrafilm consulted for approach only).
- Local only; no tag/push/release.

## v0.94 local test build

- Convert output matches leveled trichrome scans: film base **0.5/0.5/0.5** linear FS; image = `0.5 * 10^(-(D-Dmin))` (no orange-mask cast).
- Configurable film-base rebate border (default 4% of short edge, optional soft edge).
- Write `nss.scan/1` ImageDescription with `film_base` 0.5 after_profile and stock in `processing`; 16-bit LZW `_RGB.tiff` naming like trichrome.
- H&D logistic with toe/shoulder; mid density 0.75 at 18% grey; per-channel Kodak/OFX gammas.
- Round-trip test with Develop/OFX Density In (base 0.5) + Positive Out (stock gamma, linear): grey ramp monotonic and near-neutral.
- Local only; no tag/push/release.

## v0.93 local test build

- Replace the **Develop** module with **Convert**: digital camera RAW (Fujifilm X-T3 LibRaw / X-Trans) or linear TIFF to a simulated colour-negative 16-bit `_NEG.tiff`.
- Input colour: Chart camera profile via `activeCam()` + `colorsci.ApplyCam` (same CamCal matrix as Light leveling). LibRaw camera matrix stays off.
- Film model: mid-slope H&D curves from existing Kodak PDF gamma tables + dye mixing from `colorsci` spectral dye sets (H-1-5219, E-4046, E-4050, E-7023, E-7022). Orange-mask Dmin when available.
- Stocks: Vision3 500T, Vision3 250D, UltraMax 400, Portra 400, Gold 200, Ektar 100. Exposure EV control. English UI only.
- Inspired by spektrafilm pipeline shape; **no** spektrafilm code or CC BY-SA profiles. See `docs/convert-design.md` and `NOTICE_CONVERT.md`.
- No tag, push, or GitHub release in this step. OFX unchanged.

## v0.92 local test build

- Replace the proportional-only light loop with measured secant slopes, bounded steps, repeated-code detection, and bracketed bisection. Use the known camera matrix with the measured responses.
- Use the same linear conversion, sampled pixels, locked rectangle, and median for control and TIFF verification. Ignore isolated clipped samples when the film base itself is not saturated.
- Default to 10 solve rounds. Migrate the legacy four-round default once without overwriting other chosen limits.
- Measure neighbouring integer codes and compare cached RGB combinations in after-profile space. Track the best measured combination separately from the next trial.
- Require fresh full R/G/B verification and retain the final TIFF check with one corrective pass. A result outside the default 0.49 to 0.51 interval is not LEVELLED.
- Report unreachable bounds and closest measured results. Append failed or stopped attempts to `leveling_failures.csv`.
- Preserve the previous accepted metadata base after a failed run. Only a successful measured run replaces it.
- Add regression tests for clipping bounce, integer limits, nonlinear response, noise, bad starts, unreachable channels, matrix coupling, final drift, identical TIFF statistics, and failed-run logs.
- Build locally for hardware testing. No v0.92 tag, push, or release yet. OFX stays at 0.90.6 with no plugin source changes.

See [leveling algorithm and limits](LIGHT_LEVELING.md) and [verification](QA_v0.92.md).

## v0.91

- Remove the DCTL module and its Film base, White balance, and Mask & Crosstalk pages. Remove generators, app template embedding, export-only settings, and Light/colour-cast write-to-DCTL controls.
- Keep the 500T illuminant calculator as an English-only Develop-preview tool. Remove DCTL-only saved-stock controls and global export state.
- Move camera chart calibration to Camera & Light > Camera. Keep camera-profile fitting and use in leveling; remove calibration DCTL export.
- Send the accepted leveling base to Develop instead of the retired DCTL page. Open merged TIFFs in Develop. Update four-module navigation and command-line page routing.
- Keep camera/light control, auto leveling, scan presets, capture, image tools, and the built-in Develop chain. Keep TIFF tag 270 and the `nss.scan/1` schema.
- Remove the remaining Chinese UI text. Add a repeatable source/embedded-asset language and retired-export check.
- Fix incorrect RGB metadata by using only the accepted Light-page leveling result after the camera profile. Remove normal scan post-merge base measurement. Calibration-only TIFF verification remains part of leveling.
- Add the default-on **Film base for metadata: Last leveling (after profile)** switch and RGB/time display to both capture panels. Send base to Develop uses that same reference. Off or no matching result omits `film_base`.
- Persist the result in light state and named/exported scan presets. `base_source` is `light leveling (after profile)`; the additive `film_base_space: after_profile` marker prevents double camera correction.
- The independent OFX 0.90.5 patch loads after-profile base without transforming it again. It keeps the pending camera matrix for image pixels and preserves legacy metadata behavior. No scan TIFFs are rewritten.

See [verification and limits](QA_v0.91.md). Earlier entries below describe historical releases and do not imply that DCTL export remains available.

## Historical app releases

Compiled from version comments, backup files, and notes in the source (builds before v0.70 were FilmBaseMeter v0.1–v0.67).

- **v0.70** — Develop module: three DCTLs ported to Go, a Resolve-style node chain; Ektar-related data.
- **v0.71** — Develop preview per-frame rotate/flip; default output is display-referred; embedded 2383 (D65) LUT; roll open and film-base estimate.
- **v0.72–v0.74** — Colour / test-parameter and command-line option adjustments.
- **v0.75** — X-T3 measured presets and matrix rows; White LED / Narrowband illuminant choice; QA composite tools.
- **v0.76** — Default-value adjustments such as matrix-row scaling that preserves neutrality.
- **v0.77** — Guided hair / fibre removal.
- **v0.78** — Vision3 500T data and tests, default-value adjustments.
- **v0.79** — Fibre-marking improvements.
- **v0.80** — RGB Light Air settings page; ISO + fixed shutter; sensor-mosaic (photosite-average) merge.
- **v0.81** — Trichrome scanning drives the lamp; base shutter; raw serial log; CFA per-channel scaling.
- **v0.82** — Leveling results linked to the Light page; trichrome film base sent to DCTL; trichrome + HDR scanning.
- **v0.83** — Scan configuration presets (`scanconfig`); HDR sub-page reorganisation; abortable bracketing; RAW cleanup; NegPy-style collapsible cards.
- **v0.84** — Camera + lamp input chart calibration (3×3, neutrality preserved, CIEDE2000).
- **v0.85** — Per-channel leveling targets (profile-matrix film base, TIFF-frame correction).
- **v0.86** — Leveling / Light-page improvements.
- **v0.87** — Trichrome auto-leveling with a hand-selected film-base rectangle.
- **v0.88** — Light page shares the live view for drag-to-select; leveling R/G/B readouts, copy, and CSV log; “Write DCTL” wrote Density_In base and presets. See `NegativeScanStudio_v0.88_notes.txt`.
- **v0.89** — First 500T 3200 K balance path. Approximate density offsets from a 10 nm transcription of Kodak H-1-5219 rev. 3-26 spectral sensitivity, Planck illuminant integration, and ECN-2 gamma. Supported 3200/4300/5500/6500 K and custom K; Light page could write DCTL, save/load film-library entries, and send values to Develop. After leveling, the lamp, film base, and camera WB were left unchanged. Density_In gained default-off `cast_on/cast_r/g/b`, with the same formula in the Go Develop preview. Measured gray-point and residual-cast fitting were left for later.
- **v0.90** — HDR/trichrome scan TIFFs write versioned `nss.scan/1` JSON in ImageDescription / tag 270 (known lamp values, film base, camera matrix/state, lamp model, roll name, version, and so on); hair/dust/crop are preserved. Go TIFF writer and headless round-trip tests passed; Windows EXE cross-compiled. First release that also attached an OFX 0.90 prototype; plugin history is now kept independently in [ofx/CHANGELOG.md](../ofx/CHANGELOG.md). The app remains v0.90; OFX 0.90.1 does not bump the app. The cancelled 400K WIP was not applied.

## Repository notes

The git repository includes the complete source and the icons, resources, and LibRaw static libraries needed to build. Windows executables are attached to GitHub Releases and are not committed as source.

The root `template.dctl` and `template_core.dctl` remain unchanged as inputs to `ofx/tools/generate.py`. They are not app assets or app export features. Removing them would break OFX regeneration, so app-only cleanup does not delete them.

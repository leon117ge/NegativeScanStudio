# macOS user interface (Gio), work in progress

The macOS app is a separate front end in `macapp/`, written with [Gio](https://gioui.org)
(v0.10.3). The Windows app keeps its walk (Win32) UI in the repository root and is not
affected: `macapp/` is its own Go module (`basetool/macapp`, `replace basetool => ../`),
so the Windows build never compiles or links it, and its Gio files carry the build
constraint `darwin || (linux && gio)` (Linux only for development and the headless
screenshots). The core packages (`camera`, `hdrscan`, `rawconv`, `filmdb`, ...) are
used unchanged; full-resolution processing stays in Go on the CPU.

## Layout

The window follows `main.go` of the Windows app: header (icon, title, subtitle, About),
the first three modules in a bottom page bar (Camera & Light, HDR Scan, Crop & Merge)
with the same segmented sub-pages, the same dark colours and orange accent, cards with
the same titles, rows and control order. Ported so far:

| Module | Sub-page | macOS |
|---|---|---|
| Camera & Light | Camera | ported (cards Camera and Meter the clear film base, live view, log; Chart calibration and the leveling-metadata switch since group f, slice 1; the film-base DB tools since slice 2) |
| Camera & Light | Light | ported (RGB Light Air, Channels, Auto film-base leveling, Scan config presets, Faults, last leveling captures, 500T illuminant, log) |
| HDR Scan | White light scan | ported (live view, Capture and merge card) |
| HDR Scan | Trichrome | ported (live view, Trichrome scan card, film-base metadata) |
| HDR Scan | Mono trichrome | ported (Hikrobot live view with centre marks, camera card, Mono trichrome capture card) |
| HDR Scan | Focus check | ported (live loupe with zoom and pan, centre marks, green / white light, auto exposure, sharpness meter with peak hold) |
| Crop & Merge | Crop, Merge files, Remove hairs, Batch RAW to TIFF | ported (crop, merge, dust / hair removal, batch RAW to TIFF) |
| Convert | | not part of the macOS app (user's decision); Windows keeps it |

On the Camera page, Chart calibration and the switch "Film base for metadata: Last
leveling (after profile)" work since group (f), slice 1; Record base, Save base to DB,
Save last frame 0 to DB, Manage and the Film stock / Tol. selectors since slice 2 (the
shared `film_base_db.json`); the Fixed frame 0 film-stock comparison (Meter base, a stock
change after the lock, the capture result) since slice 3.

The camera list shows the Fujifilm cameras that ImageCaptureCore reports (W6
transport: `camera.ListICC`, `camera.OpenICC`, `camera.OpenFuji`), then the simulated
camera. Cameras that do not accept PTP commands are skipped with a log line.

Copied declarations (item lists, `hdr_config.txt` parsing, nav icons) are checked
against the Windows source by tests (`internal/drift`), so the two front ends read and
write the same settings files in `~/Library/Application Support/NegativeScanStudio`.

## Light page (W9 group a)

Camera & Light > Light controls the RGB Light Air over its serial port and runs the
automatic film-base leveling exactly as Windows does. The page logic is
`macapp/hdrctl/light*.go`; the layout is `macapp/lightview.go`.

- Every non-UI declaration the Windows page uses is copied verbatim into
  `hdrctl/copied.go` and checked against the Windows source by
  `TestCopiedLightDeclsMatchWindows`. That covers the light config file, `busyOnceLight`, the X-T3
  leveling camera, the camera-profile plan, the TIFF check, `leveling_log.csv` /
  `leveling_failures.csv` records, the Hikrobot leveling camera, preset summaries and the
  frame-0 handoff. The UI-bound methods (connect, apply, start leveling, results,
  presets) are ported line by line; message boxes become in-window dialogs, `Synchronize`
  becomes `UI.Post`.
- Leveling sequence (unchanged): parameters and camera-profile plan, the
  Rectangle-without-a-pick check, the camera choice (X-T3, Hikrobot, Hikrobot simulator,
  or the synthetic camera that follows the simulated light), the temp folder
  `nss_leveling_*` (deleted afterwards), `lightlevel.Run`, up to two TIFF checks with one
  corrective pass, the closest-reachable rule, the final state read (20) that must match,
  the success or failure CSV row, release of a frame-0 lock and the handoff of light plus
  exposure to the frame-0 lock (X-T3 only), and the limited / too-bright dialogs.
- `TestLevelingMatchesWindowsSequence` runs a full leveling on the simulated light
  through the page and replays the Windows sequence on a fresh simulated light. Every
  step, reading, final value, serial command, log line and the CSV row must be equal.
- Serial ports: `rgblight.ListPorts` / `OpenPort` list and open `/dev/cu.*` on macOS (the
  light is `/dev/cu.usbmodem*`, USB 0483:5740, listed first). The list is built on the
  light worker goroutine because it may call `ioreg`.
- ISO, base shutter and the film-base rectangle are shared with the Camera page, as on
  Windows. A change of the light values drops the leveling handoff and releases a
  frame-0 lock taken with other values.

Differences from Windows, and why:

- Since group (c) the Hikrobot leveling exposure is the Mono trichrome page's Exposure
  field (`monoScanExposureUS`, as on Windows), falling back to the best-quality scan
  preset only without that page; "Auto-meter each channel" is the page's check box. The
  exposure is a plain field in us instead of a spin box.
- The leveling metadata switch on the Camera page works since group (f), slice 1, and
  the Fixed frame 0 film-stock comparison since slice 3. The leveling itself, its
  records and the handoff work.
- The 500T card is titled "500T illuminant" and has no "Send to Develop" button: on
  Windows that button only passes the cast to main.go's Develop / Convert hook, which
  shows a message and stores nothing (`castpage.go`, `main.go` `toDevelop`). Calculate
  and the result text are unchanged, without the closing "Send to Develop to apply".
- Layout: the live view keeps 55 % of the left column and the cards below it (last
  leveling captures, 500T, log) scroll, so the preview stays usable at 1280x800. The
  right column is 480-520 dp as on Windows and never takes more than the page minus
  360 dp. The live view no longer forces a 360x240 minimum (that clipped the controls
  under it). The window opens at 1280x800; the minimum is 1024x640.


## Crop & Merge (W9 group b)

Crop, Merge files, Remove hairs and Batch RAW to TIFF are ported with the same
labels, control order and behaviour as Windows. The page logic is
`macapp/cropctl/` (Crop, Batch RAW to TIFF) and `macapp/hdrctl/merge.go` /
`hair.go` (Merge files, Remove hairs); the layouts are `cropview.go`,
`mergeview.go`, `hairsview.go` and `batchview.go`. Full-resolution work stays
in the existing `cropper`, `hdrscan` and `rawconv` packages, on CPU goroutines.

- Crop: add files or a folder (native panels, TIFF / JPEG / PNG), automatic
  edge detection and levelling (`cropper.Detect` / `AutoRect`), the orange
  frame (move, corners, sides, aspect lock, inset, grid, edges overlay),
  rotation (±5°, 0.05° steps), export next to the source in a `cropped`
  subfolder or a chosen folder. Existing `_crop` files are never overwritten.
  Settings are the same `crop_config.txt` as Windows, checked by
  `TestCopiedCropDeclsMatchWindows`.
- Merge files: frame 0, a brighter frame, an optional third frame, alignment,
  merge method, compression and dust removal (Off / Light / Strong). Output is
  `<frame 0>_HDR.tiff` next to frame 0, with the same `hdrscan.ProcessOptions`
  as Windows (`MergeFilesOptions`). The source files are never changed or
  deleted. Windows' "Open in Develop" button is not on the Mac page (see
  below).
- Remove hairs: open a 16-bit TIFF (Browse..., Last HDR, or a typed path),
  drag a box around a hair, heal with nearby film texture, Show mask, Undo,
  Save into `_HDR.tiff` (in place when the file is already an HDR output,
  otherwise a new `_HDR.tiff`). Copied helpers are checked by
  `TestCopiedHairDeclsMatchWindows`.
- Batch RAW to TIFF: add files or a folder, convert to a linear 16-bit TIFF
  next to the RAW or in `make_tiff/`, LZW, ICC, worker count, Retry failed,
  Clear finished, and a watch folder that waits until a new RAW is stable
  before converting it. An existing TIFF is never overwritten (Skipped).
  Settings are the same `raw_config.txt` as Windows.

Long jobs (export, merge, dust removal, batch conversion, watch folder) run
on worker goroutines with progress; merge and batch conversion can be
stopped. The UI goroutine is never blocked. File and folder picks use the
native panels.

Golden tests compare the Mac page output with the Windows code path on the
same inputs: crop export (byte-identical), merge and dust (pixels equal; the
embedded metadata creation time may differ by a second), hair save
(byte-identical), and batch RAW to TIFF (byte-identical when `NSS_TEST_RAF`
points at a real RAW).

Differences from Windows, and why:

- Drag and drop of files onto the pages is not wired: Gio does not receive
  file drops on macOS. The Windows hint texts that mention dropping files are
  kept verbatim; use Add files, Add folder or Browse... instead.
- Convert is not part of the macOS app, by the user's decision; the Windows
  app keeps it. The Mac page bar has three modules, `--develop` /
  `--convert` are ignored (the app opens on Camera & Light > Camera), and
  the Merge files "Open in Develop" button is removed rather than disabled,
  because on Windows it only opens the merged HDR in Convert. The merged
  `_HDR.tiff` is saved next to frame 0 as before.
- Merge files has a Stop button and a progress bar (Windows has no Stop on
  this page). Cancel is honoured between `ProcessN` stages; any partial
  output is removed and the sources are left unchanged. Dust removal itself
  is not interruptible mid-stage. Crop export has no Stop, as on Windows.
  Hair removal of one box is a short job and has no cancel.
- Adobe DNG Converter is not used: `rawconv` only knows the Windows install
  path, and changing that would change the Windows program. The check box
  stays off and the label is "Not available on macOS".
- Status text for a subfolder conversion uses `make_tiff/` (Windows uses
  `make_tiff\`).
- Yes/No walk dialogs are OK/Cancel in-window dialogs, as on the other Gio
  pages.
- The `--dust` command-line tool is not ported.

## Trichrome and Mono trichrome (W9 group c)

HDR Scan > Trichrome and Mono trichrome are ported with the same layout,
labels, tooltips, control order and behaviour as Windows. The page logic is
`macapp/hdrctl/tri.go` and `mono.go` (with `levelmeta.go`, `hik.go`); the
layouts are `triview.go` and `monoview.go`.

- Trichrome (X-T3 and the RGB Light Air): the shared live view, the status
  lines (camera, frame 0 lock, light now, scan values, output), Mode (one
  shot per channel or HDR bracket per channel), Bracket per channel, Capture
  trichrome / Stop, Meter base, the Film base metadata
  check box with the last leveling line, a progress bar and the result box.
  A scan reads the light (20), switches R, G and B on one at a time with the
  leveling / Apply values (`rgblight.ChannelSwitch`), shoots each channel's
  bracket with `hdrscan.Session.CaptureTrichrome` on the camera worker,
  restores the light and frame 0, and merges with `hdrscan.ProcessTrichrome`
  in the background while the next frame can be scanned. Stop deletes the
  partial RAFs of that frame and restores the light.
- Mono trichrome (Hikrobot MV-CS240-90UM through the W7 `hikcam` backend, or
  the simulator): the letterboxed live view with the centre / flatness marks
  (`focusmark`, display only), the camera card (SDK line, device list and
  Refresh, Connect, Preset, Pixel format, Exposure with the leveled / not
  leveled line, Gain, Black level, Trigger, gamma, full frame, ROI, Apply camera
  settings) and the capture card (light, values, output, CA correction,
  Auto-meter each channel, Mode, HDR bracket, Frames per exposure, Capture /
  Stop, status, progress, report). Capture applies the settings, runs
  `monocap.Capture` (settle check, native mono HDR merge, frame averaging) and
  `hdrscan.ProcessMonoTrichrome` (CA correction, movement check) on the
  Hikrobot worker goroutine. The Exposure field is the shared Hikrobot exposure
  of Light leveling.
- Without `MvCameraControl.framework` the SDK line says why, the device list is
  the simulator only and nothing crashes; leveling with the Hikrobot camera
  reports the same error.
- Camera and light I/O never runs on the main thread: the X-T3 on the camera
  worker, the Hikrobot camera on its `hikcam.Controller`, the light on its
  worker; results come back through `UI.Post`. The Mono and Light pages share
  one Hikrobot camera; starting one page's preview or leveling stops the other
  preview first.
- Film base metadata ("Last leveling"): Trichrome and Mono trichrome write the
  accepted leveling result into the scan metadata when the scan values match
  it, as on Windows.
- Copied declarations are checked by `TestCopiedTrichromeDeclsMatchWindows`.

Golden tests (`hdrctl/tri_test.go`, `mono_test.go`) run the page and the
literal Windows sequence on the same simulated light and camera and compare
every camera call with the light state at that moment, the light commands, the
exposures, the file names (shot time masked) and the merged TIFF byte for byte
(only the metadata creation time masked): Trichrome single and 0 / +2 / +3 EV,
Mono trichrome single, the default 0 / +2 / +3 / +5 EV bracket, two frames per
exposure and Auto-meter each channel.

Differences from Windows, and why:

- The SDK status (a dlopen of the framework) is read on the Hikrobot worker
  when the page starts, not on the UI thread, so a slow load never blocks the
  window.
- The Hikrobot exposure metadata of a Mono capture is computed on the UI
  goroutine before the capture instead of on the worker; the fields and the
  written TIFF are the same (golden test).
- Number edits are text fields with the Windows ranges and decimals applied
  when editing ends.
- The centre-mark arm lengths are in dp, so they scale on Retina screens.
- "Send base to Develop" (Trichrome, and White light under Camera > Meter
  the clear film base) is not on the Mac pages. On Windows its only consumer
  is `hdrui.SendBase`, set in `main.go` to a message box; the base is not
  saved and scan metadata is written by the capture, not by this button
  (`tripage.go` `sendBase`, `hdrpage.go` `sendBase`).

## Focus check (W9 group d)

HDR Scan > Focus check is ported with the same layout, labels, tooltips,
control order and behaviour as Windows (`focuspage.go`). The page logic is
`macapp/hdrctl/focus.go`; the layout is `macapp/focusview.go`.

- Left: the live view (click at Fit zooms to 200 % around that point, drag
  pans when zoomed), the Center mark check box (shared with Mono trichrome)
  and the hint. The marks follow the true sensor centre and the 10 % inset
  through zoom and pan and hide when off-screen; the green crosshair marks the
  exposure and sharpness sample.
- Right, the Focus check card: camera, light, auto exposure, Zoom (Fit,
  100 %, 200 %, 400 %), Green only / White (enabled when the light is
  connected), the sharpness meter (Combined x 20000 on 0..1000), sharpness
  and Laplacian / grain values, Peak hold with Reset peak, the auto-exposure
  note and the log.
- Entering the page saves the capture exposure (the Mono trichrome fields for
  the Hikrobot camera, the X-T3 exposure otherwise) and the light state,
  switches the light to green only at the scan values, applies full-resolution
  live settings and runs the loop: grab, meter rectangle (the visible region,
  at least 100 %), `hdrscan.RegionSamples`, `CropGray16ToFloat64`,
  `MeasureFocus`, `LiveAE` with the same thresholds, peak hold. Leaving
  restores the exposure and the light. The Hikrobot camera (or simulator) is
  preferred; the X-T3 live-view JPEG is the fallback (shutter-only auto
  exposure).
- Threading: setup, the frame loop, the focus metric, auto exposure and the
  restore run on the Hikrobot worker (`hikcam.Controller`), or on a goroutine
  with X-T3 calls on the camera worker; light switching runs on the light
  worker. Frames and labels come back through `UI.Post`.
- Copied declarations (`zoomItems`, `zoomVals`, `imageToGray16`,
  `gray16Pix`, `liveGrabMessage`) are checked by
  `TestCopiedFocusDeclsMatchWindows`.

Golden tests (`hdrctl/focus_test.go`) run the page and the literal Windows
enter / loop / leave sequence on fresh simulators and compare, exactly: the
focus score (Laplacian, Tenengrad, grain, combined), the peak hold, the whole
`LiveAE` state, the meter samples and region, and every camera call with its
values. Hikrobot simulator: 1 to 12 frames at Fit, 100 %, 200 % and 400 %,
with and without a pan. X-T3: 10 live-view JPEG frames at 200 % with the
shutter steps and the restore. The page reads no RAF, so `NSS_TEST_RAF` does
not apply.

Differences from Windows, and why:

- The X-T3 device, the light client and the scan light values are read on the
  UI goroutine and handed to the workers (Windows reads them from the UI
  struct on the worker); the values used are the same.
- After a zoom or pan the last frame is rebuilt on a goroutine and posted
  back (Windows builds the bitmap on the UI thread); a full 6016 x 4096 frame
  would otherwise stall the window. The newest rebuild wins.
- Leaving Focus check for Mono trichrome starts the Mono preview after the
  restore. On Windows `leave()` queues the restore, `startPreview()` follows,
  and the restore's `StopSync` bumps the Hikrobot generation, which (from
  reading the code; not checked on hardware) cancels that preview. The
  Windows code is unchanged.
- The zoomed loupe is drawn with nearest-neighbour scaling (as GDI
  StretchBlt does), Fit with linear filtering as the other Gio views.
- The sharpness bar is drawn as a determinate bar (the Windows ProgressBar).

## Chart calibration and leveling metadata (W9 group f, slice 1)

Camera & Light > Camera > Chart calibration... is ported from the Windows
dialog (`chartcal.go`) with the same title, rows, labels, tooltips and control
order. The logic is `macapp/hdrctl/chartcal.go`; the layout is
`macapp/chartcalview.go`.

- Rows: Open chart TIFF... / Use last scan (the last Trichrome output of this
  session) and the file line; Load reference (CGATS)... and the reference line
  (with the D50 warning); the grid hint; the chart view with the 24 patch
  boxes (green clean, red "CLIP") and the A1 / A6 / D6 / D1 markers, dragged
  with the mouse (14 dp pick radius); Auto-detect, Rotate 180°, Reset grid,
  Fit; the monospaced report; Profile name and Save as camera profile; Active
  camera profile, Enable, Strength (0..1, two decimals), Delete profile and
  Close.
- The Windows code runs unchanged where it matters: `colorsci.DetectCamChart`,
  `SampleCamChart`, `ClipStops`, `FitCamChart` and `ParseCGATS`; the report
  lines, the profile record and `camera_profiles.json` (shared with Windows,
  next to the other settings) are written the same way. `calTip` and
  `cleanName` are verbatim copies checked by
  `TestCopiedChartCalDeclsMatchWindows`.
- Threading: loading the TIFF, the preview, grid detection, patch sampling
  and the fit run on goroutines; results come back through `UI.Post`. A job
  that ends after Close or after a newer job is dropped. Reading the small
  CGATS file stays on the UI goroutine, as on Windows. File picks use the
  native panels.
- The Meter card switch "Film base for metadata: Last leveling (after
  profile)" and its last-leveling line are live (`refreshLevelingUI`): the
  switch writes `use_last_leveling` in `rgb_light.json` (shared with the
  Light, Trichrome and Mono trichrome pages), and with the option on the Base
  R / G / B readouts show the accepted leveling base (or "-" when the light
  values differ), as on Windows. Windows' "Send base to Develop", which also
  follows a matching leveling there, is not on the Mac page (see Layout).

Tests (`hdrctl/chartcal_test.go`, `levelmeta_ui_test.go`,
`chartcal_ui_test.go`): synthetic 16-bit charts (upright, rotated 180°, and one
with a clipped patch) go through the page and through a replay of the Windows
dialog (load, detect, sample, `solve`, `save`); the corners, the detect line,
the whole fit report and `camera_profiles.json` must be equal (the profile time
pinned). Logic tests cover Rotate, Reset grid, marker drags, Save with an
invalid name and with Replace, Enable / Strength / Delete, the quiet reload of
the last reference, and a late job after Close. A test checks that every
user-visible string of the Windows dialog appears in the port. The leveling
switch is compared with `refreshLevelingUI` for six states.

Differences from Windows, and why:

- The modal dialog is an in-window panel over the page (Gio has one window);
  the page underneath does not take input while it is open, like a modal
  dialog. Message boxes are the in-window dialogs.
- Load, detect, sampling and Fit run in the background with a busy line next
  to Fit, and the buttons that start a job are off meanwhile (Windows runs
  them on the UI thread and the window stalls).
- The patch clip overlay after a drag or Rotate appears when the background
  sampling finishes (Windows samples before it repaints).
- The open panels filter on the extensions (`tif`, `tiff`; `txt`, `it8`,
  `cie`, `cgats`); the native panel has no "All files (*.*)" choice.
- Strength is a text field with the NumberEdit range and decimals applied when
  editing ends.

## Film-base DB (W9 group f, slice 2)

Camera & Light > Camera, Meter the clear film base card: Film stock, Tol.,
Record base, Save base to DB, Save last frame 0 to DB and Manage are ported
from `hdrdb.go` with the same labels, tooltips, enable rules
(`updateButtons`), messages and log lines. The logic is
`macapp/hdrctl/filmdb.go`; the two dialogs are `macapp/dbview.go`.

- The DB file is `film_base_db.json` through `filmdb.Path(configDir())`, the
  file name and JSON format of Windows (`filmdb` is shared and unchanged). On
  macOS the folder is `~/Library/Application Support/NegativeScanStudio`
  (`os.UserConfigDir()` + `NegativeScanStudio`, as `configDir()` on Windows),
  so a DB copied between the two machines works on both. `loadDB` runs at
  start as on Windows: the Film base presets of `config.txt` (or the
  default "Session 1") that are not in the DB yet are imported; deleted
  presets stay hidden.
- Save to film-base DB (Record base, Save base to DB, Save last frame 0 to
  DB): the info text, Action (Save as a new film stock / Update an existing
  film stock), New stock name (the roll name, cue "e.g. Portra 400"),
  Existing stock, Update mode (Replace / Average with the stored values
  (weighted)), the Stored line, Light source (cue "optional, e.g. CS-LITE
  5000K"), Save, Cancel. The current stock preselects Update; the name,
  duplicate and "nothing to update" checks keep the panel open, as the
  Windows Save handler does. `filmdb.Put` stores the entry (an unknown
  aperture is `filmdb.ApertureUnknown`), then the stock is selected and
  "Film-base DB: saved ..." (or "...: not saved") is logged.
- Manage ("Film-base DB"): the DB path, Film stock, the stored values,
  conditions, light and crosstalk line, Name, Rename, Delete (Yes / No
  question), Close; renaming the selected stock follows it.
- Record base shoots the standalone fixed frame 0 on the camera worker
  (`Session.RecordBase`, ISO 250 and the base shutter), keeps
  `<roll>_..._BASE.RAF`, shows the RAW and its base, and leaves live view
  paused until the panel closes. Save base to DB reads the locked metering
  probe (`ProbeBase`); the aperture is the one reported at the lock, else the
  camera's. Save last frame 0 to DB uses the base of the last capture's frame
  0 (`setLastF0`: frame 0's exposure and reading at Capture, the merge's
  `Base1F0`), and only a clean, unclipped base.
- `stockNone`, `frame0Base`, `rgbText`, `entryText`, `compareText`,
  `fnText` and the preset import (`Preset`, `loadConfig`, `parsePresets`, ...)
  are verbatim copies in `hdrctl/copied_filmdb.go`, checked by
  `TestCopiedFilmDBDeclsMatchWindows`.

Tests: `hdrctl/filmdb_test.go` replays the dialogs against `filmdb.Put` on a
fresh DB (new, replace, weighted average), checks every message, the file on
disk, the preset import and hiding, Manage rename / delete / close, and the
`setLastF0` reasons; with `NSS_TEST_RAF` a mock camera whose shots are a copy
of the RAW runs Record base, a Fixed lock, Save base to DB, a capture and
merge, and Save last frame 0 to DB end to end. `dbview_ui_test.go` drives the
panels in the headless window (`NSS_SHOTS` keeps the screenshots).

Differences from Windows, and why:

- The two modal dialogs are in-window panels (Gio has one window); the page
  underneath takes no input while one is open. Enter and Escape do not press
  Save / Cancel (the walk DefaultButton / CancelButton); use the buttons.
- Save base to DB reads the probe RAW off the UI goroutine with the controls
  off meanwhile (Windows reads it on the UI thread and the window stalls).

## Fixed frame 0 film-stock comparison (W9 group f, slice 3)

With Frame 0 = Fixed, frame 0's base is compared with the selected film stock
(`filmdb.Compare` per channel in EV against Tol., plus ISO / shutter
mismatches) in the same three places as on Windows (`hdrpage.go`):

- Meter base: after the fixed lock the locked probe's base (`ProbeBase`, on
  the camera worker) is compared; the line ("<stock>: Film base OK vs DB: ..."
  or "<stock>: Film base WARNING vs DB (...): ...") is logged and added under
  the base readout, "Film stock: None (no DB check)" without a stock, "<stock>:
  no base to compare (...)" without a probe base. A failed check or a frame-0
  warning shows the Windows "Frame 0" message as an in-window notice (not
  for a light handoff lock).
- A stock change after a fixed lock (`stockRecheck`) redoes the line from the
  locked probe without a new shot; the base readout without the line is kept
  (`lockBase`) and cleared with the lock.
- Capture result: when the last frame 0 is a clean base (`setLastF0`) and
  Frame 0 is Fixed, the comparison is the last line of the result.

The "not on macOS yet" texts are gone (the film-stock line, the `resultText`
note, and the unused page placeholder: every page is ported).

Tests: `hdrctl/stockcmp_test.go` runs no stock, pass, fail (with the Frame 0
notice) and a missing probe at Meter base and in the recheck on a real RAW
(`NSS_TEST_RAF`), and pass, fail, no stock, Metered frame 0 and an unclean
frame 0 for the capture line; the expected lines come from `compareText` on
the shared `filmdb`. A test checks the Windows strings and calls are in the
port. `stockcmp_ui_test.go` renders the card and the notice (`NSS_SHOTS`).

As on Windows, the capture line needs the merge's frame-0 base (`Base1F0`),
which `hdrscan.ProcessN` measures only with colour options on; neither app's
White light scan sets them, so a normal capture result has no stock line and
Save last frame 0 to DB says the frame 0 cannot be used. Core unchanged.

## Known follow-ups

These are not fixed in the Mac port because the fix would change the Windows
program, which stays byte-identical until the final merge to main.

1. Windows: going from Focus check to Mono trichrome may not restart the
   Mono preview. The cause is that the restore's `StopSync` bumps the
   Hikrobot generation, so the queued preview fails its generation check.
   This is suspected from reading the code and has not been checked on
   hardware. A Windows fix waits on a hardware check by the user. (The Mac
   starts the preview after the restore: `Focus.LeaveThen`.)
2. `camera/sim.go`: `OpenSim` anchors on the first 64 samples, which are
   zeros on the CC0 X-T3 RAF fixture, so `TestSimMeterCaptureMerge` skips in
   CI. The fix is to anchor on the first non-zero window. It is deferred to
   the final merge-to-main version bump, to keep the Windows exe
   byte-identical during the Mac port.

## Threading model

- `main()` gives the main thread to Gio's `app.Main`, which runs the Cocoa
  `NSApplication` run loop. Nothing else runs on, or blocks, the main thread, so blocks
  on the main dispatch queue and CFRunLoop sources (ImageCaptureCore delegate callbacks,
  file panels) are serviced at all times.
- One window goroutine owns all UI state: it lays out frames and runs the functions that
  background work posts with `UI.Post` (a queue plus `Window.Invalidate`).
- Camera I/O runs on one camera worker goroutine (`hdrctl`), the live-view loop and the
  status poll on their own goroutines, merges, crop export, hair load/save and batch RAW
  conversion on their own goroutines. They report back only through `UI.Post`. A
  set-the-dials-by-hand prompt (`Session.Dials`) blocks the capture goroutine, never the
  UI, until the in-window dialog is answered.
- Native file panels (`dialogs_darwin.go`) are opened from a helper goroutine with
  `dispatch_sync` to the main queue; the window keeps drawing while a panel is open.
- `NegativeScanStudio -selftest-runloop` checks this: once the window shows, a worker
  goroutine queues a block on the main dispatch queue and a CFRunLoop timer on the main
  run loop, and both must fire on the main thread within 5 s. CI runs it on macos-15 and
  macos-26 (`macos-gio-ui.yml`).

## Images

Live-view frames are decoded off the UI goroutine and drawn as `paint.ImageOp`s: Gio
uploads each new frame once as a GPU texture (Metal on macOS) and scales it on the GPU.

## Build, bundle, logs

- `bash macapp/packaging/build-app.sh [out-dir]` builds `NegativeScan Studio.app`
  (arm64, `-mmacosx-version-min=13.0`, `LSMinimumSystemVersion` 13.0, version taken from
  `appVersion` in `hdrpage.go`, icon converted from `ico06/app.ico` without Pillow,
  ad-hoc signed). The repository holds no Mac icon files: `mkbundle.py` builds
  `AppIcon.icns` and `Resources/appicon.png` (the header icon) from the PNG entries of
  `ico06/app.ico`; outside a bundle (`go run`, `go test`) the app reads the 256 px entry
  of `ico06/app.ico` from the source tree. Requires the darwin LibRaw from `ci/macos/build_libraw.sh`.
- Crash log: `~/Library/Logs/NegativeScanStudio/crash_log.txt` (fatal errors and
  recovered panics, with stacks). Camera log and settings stay in
  `~/Library/Application Support/NegativeScanStudio`.
- Tests: `cd macapp && go test ./...` on macOS; on Linux add `-tags gio` (needs the
  Wayland/X11/EGL headers). `NSS_SHOTS=<dir>` saves headless screenshots of the pages. `NSS_TEST_RAF` is a list of real RAW files for the batch conversion golden test and the film-base DB camera tests (skipped if unset).

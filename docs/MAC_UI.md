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
the four modules in a bottom page bar (Camera & Light, HDR Scan, Crop & Merge, Convert)
with the same segmented sub-pages, the same dark colours and orange accent, cards with
the same titles, rows and control order. Ported so far:

| Module | Sub-page | macOS |
|---|---|---|
| Camera & Light | Camera | ported (cards Camera and Meter the clear film base, live view, log) |
| Camera & Light | Light | ported (RGB Light Air, Channels, Auto film-base leveling, Scan config presets, Faults, last leveling captures, 500T illuminant, log) |
| HDR Scan | White light scan | ported (live view, Capture and merge card) |
| HDR Scan | Trichrome, Mono trichrome, Focus check | placeholders |
| Crop & Merge | Crop, Merge files, Remove hairs, Batch RAW to TIFF | ported (crop, merge, dust / hair removal, batch RAW to TIFF) |
| Convert | | placeholder |

On the Camera card, Chart calibration, Record base, Save base to DB, Save last frame 0
to DB, Manage and the leveling-metadata switch are shown but not active yet (later
increments). The film stock list is read from the shared `film_base_db.json`; the
Fixed frame 0 film-stock comparison is not on macOS yet.

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

- The Mono trichrome page is not on macOS yet. The Hikrobot leveling exposure comes from
  the shared `HikExposureUS` setting, falling back to the best-quality scan preset.
  "Auto-meter each channel" is read from the settings file. The exposure is a plain
  field in us instead of a spin box.
- Leveling metadata (the Camera card switch "Film base for metadata: Last leveling") and
  the film-stock comparison of frame 0 stay disabled until the film-base DB increment.
  The leveling itself, its records and the handoff work.
- "Send to Develop" on the 500T card shows the same explanation as Windows (Convert does
  not use the Develop cast controls); nothing is sent.
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
  deleted. "Open in Develop" first checks that the HDR TIFF is still there
  (same message as Windows if it is gone).
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
- Convert (Develop) is not on macOS yet (later increment). "Open in Develop"
  still switches to the Convert module, as Windows does, and shows the HDR
  path in the window with a message that Convert is not ported. That is a
  known gap.
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
  Wayland/X11/EGL headers). `NSS_SHOTS=<dir>` saves headless screenshots of the pages. `NSS_TEST_RAF` is a list of real RAW files for the batch conversion golden test (skipped if unset).

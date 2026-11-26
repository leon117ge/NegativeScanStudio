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
| Camera & Light | Light | placeholder |
| HDR Scan | White light scan | ported (live view, Capture and merge card) |
| HDR Scan | Trichrome, Mono trichrome, Focus check | placeholders |
| Crop & Merge | Crop, Merge files, Remove hairs, Batch RAW to TIFF | placeholders |
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

## Threading model

- `main()` gives the main thread to Gio's `app.Main`, which runs the Cocoa
  `NSApplication` run loop. Nothing else runs on, or blocks, the main thread, so blocks
  on the main dispatch queue and CFRunLoop sources (ImageCaptureCore delegate callbacks,
  file panels) are serviced at all times.
- One window goroutine owns all UI state: it lays out frames and runs the functions that
  background work posts with `UI.Post` (a queue plus `Window.Invalidate`).
- Camera I/O runs on one camera worker goroutine (`hdrctl`), the live-view loop and the
  status poll on their own goroutines, merges on a goroutine per merge. They report back
  only through `UI.Post`. A set-the-dials-by-hand prompt (`Session.Dials`) blocks the
  capture goroutine, never the UI, until the in-window dialog is answered.
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
  Wayland/X11/EGL headers). `NSS_SHOTS=<dir>` saves headless screenshots of the pages.

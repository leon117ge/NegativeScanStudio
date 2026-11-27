# mactether: X-T3 tether check on macOS

`mactether` is a small command-line tool for the macOS port (work item W6). It drives the
Fujifilm X-T3 with the same PTP logic as the Windows app (`camera/fuji.go`). On a Mac it talks to
the camera through Apple ImageCaptureCore (`camera/icc*.go`, `camera/icc_darwin.m`) instead of
Windows WPD. The Windows capture path is unchanged.

It is a hardware check, not part of the app: run it once to confirm that list, info, live view and
RAW capture work over USB on your Mac.

## Commands

    mactether [flags] [list | info | lv [FILE] | capture [DIR]]

| Command | What it does |
|---|---|
| `list` (default) | Cameras ImageCaptureCore sees, with USB IDs and UUID. No camera is not an error. Also prints the ImageCaptureCore access line; on macOS it always reads authorized, because macOS has no ImageCaptureCore authorization API. |
| `info` | Opens the camera: model, firmware, serial, vendor operations, exposure (ISO, shutter, f-number) and exposure mode. |
| `lv [FILE]` | Saves one live view JPEG (default `lv.jpg`). With `-n N` it fetches N frames and reports frames per second. |
| `capture [DIR]` | Takes one picture and downloads the RAF into DIR (default `.`), named `mactether_DSCFnnnn.RAF`. The file is deleted from the camera after download, as in the app. |

| Flag | Meaning |
|---|---|
| `-uuid U` | Camera UUID from `list` (default: the first camera that accepts PTP commands). |
| `-sim` | Built-in simulated X-T3 instead of ImageCaptureCore (works on any OS). |
| `-v` | Print the camera log (every PTP transaction) to stderr. |
| `-log DIR` | Also append the camera log to `DIR/camera_log.txt`. |
| `-n N` | Live view: number of frames (default 1). |
| `-prefix P` | Capture: file name prefix (default `mactether_`). |
| `-timeout D` | Time to find and open the camera (default `10s`). |
| `-outheader` | Send PTP data-out phases with a 12-byte container header. Use it only if `info` or `capture` fails with errors on SetDevicePropValue. |

Errors are printed as `error: ...` on stdout and the exit code is 1.

## Getting the binary

- **From CI (easiest).** Each run of the `macOS arm64 core (W5)` workflow uploads
  `w6-mactether-sim-macos-15` and `w6-mactether-sim-macos-26`. Each contains a `mactether`
  binary (arm64, macOS 13 or later, LibRaw linked statically) plus the CI simulator output.
  Unzip it, then in Terminal:

      cd ~/Downloads/w6-mactether-sim-macos-15
      xattr -d com.apple.quarantine mactether 2>/dev/null; chmod +x mactether

- **From source.** Install Go (the version in `go.mod`) and the Xcode command line tools. Build
  LibRaw once with `bash ci/macos/build_libraw.sh`, then run `go build -o mactether ./cmd/mactether`.

## Camera settings (X-T3)

- SETUP > CONNECTION SETTING > CONNECTION MODE: **USB TETHER SHOOTING FIXED**, the setting the
  Windows app and the macOS app ask for. USB TETHER SHOOTING AUTO gives the same USB control while
  the cable is connected; FIXED also keeps the camera in tether mode across an accidental
  disconnect, so it can be connected again without changing menus. With FIXED, pictures are not
  recorded to the memory card at default settings (the program downloads them over USB).
- Exposure mode **M** (shutter dial on T or a speed), so the shutter can be set over USB.
- Focus selector **M** (or AF-S with RELEASE priority).
- With an adapted or manual lens: SETUP > BUTTON/DIAL SETTING > **SHOOT WITHOUT LENS = ON**.
- IMAGE QUALITY: **RAW** (or FINE+RAW). The capture command reports an error when only a JPEG arrives.
- A card in the camera, or SHOOT WITHOUT CARD = ON.

Before you start, quit Image Capture, Photos, X Acquire, Capture One and Lightroom: they open the
camera themselves. ImageCaptureCore on macOS has no authorization API of its own (the
control/contents authorization calls exist only on iOS and iPadOS), so mactether asks for
nothing. If macOS still shows a privacy prompt for Terminal, allow it, note its exact wording
in your report, and run again; if you denied it, allow it in System Settings > Privacy &
Security first.

# NegativeScan Studio

**Two products, one film-scanning workflow:** a Windows capture application and a DaVinci Resolve OpenFX plugin. They share this MIT-licensed repository, but have independent versions and releases.

| Product | Current version | Download / release | Documentation |
|---|---|---|---|
| **NegativeScan Studio** — scanning app | **v0.91** | [App release](https://github.com/leon117ge/NegativeScanStudio/releases/tag/v0.91) · [Windows x64 EXE](https://github.com/leon117ge/NegativeScanStudio/releases/download/v0.91/NegativeScanStudio_v0.91.exe) | [Hardware](docs/HARDWARE.md) · [App changelog](docs/CHANGELOG.md) · [App roadmap](docs/ROADMAP.md) |
| **NegativeScan Studio OFX** — Resolve plugin | **0.90.6** | [Plugin release](https://github.com/leon117ge/NegativeScanStudio/releases/tag/ofx-v0.90.6) · [Windows x64 ZIP](https://github.com/leon117ge/NegativeScanStudio/releases/download/ofx-v0.90.6/NegativeScanStudio_OFX_v0.90.6_Win64.zip) | [Install / usage](ofx/README.md) · [Plugin changelog](ofx/CHANGELOG.md) · [Plugin roadmap](ofx/ROADMAP.md) |

Both current releases are **prereleases**. Download the EXE for scanning, the OFX ZIP for Resolve, or both. The old OFX ZIP on the app v0.90 release is retained as history; use the separate plugin release above for the current plugin.

## 1. NegativeScan Studio — Windows scanning app

Camera-scanning/capture software written in Go with a [walk](https://github.com/lxn/walk) desktop GUI. The current hardware workflow targets a Fujifilm X-T3 and CuteNewDesign RGB Light Air; see [hardware support and limits](docs/HARDWARE.md).

- Tethered camera control (PTP/WPD), live-view film-base selection and RGB-light control.
- Film-base metering and closed-loop light leveling; scanning configuration and film-base presets.
- HDR/bracketed capture, trichrome/RGB-light capture and merge, linear RAW-to-TIFF conversion through LibRaw.
- Crop, merge, hair/dust removal, camera-matrix calibration, Develop preview. The app UI, prompts, and log messages are English only.
- HDR/trichrome TIFFs carry `nss.scan/1` provenance. The default-on **Film base for metadata: Last leveling (after profile)** option writes the accepted Light-page result and displays its RGB and time. It never re-measures the scene as film base.
- The accepted reference survives restart and scan-preset recall. Off, unknown, or mismatched light values omit `film_base`. After-profile metadata requires OFX 0.90.5 or newer; older unmarked scans keep their original interpretation.

**Use:** download `NegativeScanStudio_v0.91.exe` from the app release and run on Windows x64 with the appropriate camera/light setup. This is the capture app, not a Resolve plugin. App and plugin updates are independent.

[Design](docs/DESIGN.md) · [Algorithms](docs/ALGORITHMS.md) · [500T Develop preview](docs/500T_USAGE.md) · [v0.91 verification](docs/QA_v0.91.md)

## 2. NegativeScan Studio OFX — DaVinci Resolve plugin

A separate C++/OpenFX product for developing linear negative scans inside Resolve. Windows x64 is currently available.

- Camera calibration → Density In → Positive Out in one effect. The current plugin has **no mask/crosstalk stage**.
- Unchanged film stock gamma presets, visible Gamma scale default1.15 and Exposure default0.90; T/D film balance and 3200/3800/4400/5000/5600K presets that immediately fill visible film-base sliders.
- A visible RGB balance loader: **500T Base** loads **+1.07 / 0 / +0.67 stops**; manual adjustment remains. These current values are user eye-tuned under Gamma scale1.15/Exposure0.90 defaults, not a universal Kodak constant. [Calibration conditions](docs/data/ofx_film_bases.md).
- Viewer click/drag gray picking with a custom OpenGL crosshair/sample box and canonical 2D point parameter; TIFF/JSON metadata loading with manual override.
- Runtime CUDA/PTX or OpenCL buffer rendering when offered by the host, plus an optimized multithreaded CPU fallback.

**Install:** quit Resolve, unzip the plugin release, and copy the **entire** `NegativeScanStudio.ofx.bundle` folder to:

```text
C:\Program Files\Common Files\OFX\Plugins\
```

Restart Resolve and find **NegativeScan Studio OFX 0.90.6** under OpenFX → NegativeScan Studio. Back up projects and remove duplicate/older bundles before replacing them. See the [installation guide](ofx/README.md).

**Validation limits:** 0.90.6 passes native CPU, mock-host loader/render/overlay, offline PTX assembly, and mocked CUDA contract tests. Real Windows Resolve, physical GPU execution, overlay appearance/input, and host GPU negotiation remain untested for this patch. Rendering kernels are unchanged from 0.90.5. CPU mode is available. [Implementation, tests and limitations](docs/OFX.md).

## How the two products connect

```text
Scanning app → linear TIFF + nss.scan/1 metadata → OFX Load → develop in Resolve
```

1. Capture in the app's HDR/trichrome workflow. Known scan information is embedded as JSON in TIFF **ImageDescription (tag 270)**; it does not require a proprietary TIFF reader.
2. In OFX, choose the scan TIFF or an exported `.nss.json` sidecar and click **Load from file (or Resolve source)**. The plugin fills film base/camera controls and keeps a baseline before scene-illuminant compensation. Load turns Auto off so manual overrides persist.
3. Resolve's optional source-path extension may allow auto-read, but standard OFX does not guarantee a source filename. Explicit file selection is the portable route. The input must still be the same unconverted linear scan.

`light_rgb` is the scanner's drive value, **not** normalized film-base RGB. Old TIFFs without NSS metadata require manual values; unknown scan information is not reconstructed or invented. RAW conversion/other exports do not automatically imply complete scan provenance. See the [metadata contract](docs/OFX.md#数据契约).

App v0.91 removes the DCTL module, generators, template embedding, export settings, and write-to-DCTL controls. Use the independent OFX plugin for Resolve conversion. The two root conversion templates remain only because `ofx/tools/generate.py` uses them to generate plugin C++; the app does not embed or export them. Other historical DCTL sources remain in tags through v0.90. The built-in Develop preview and its existing conversion math remain available without any DCTL files. Legacy mask/crosstalk math in Develop is not a stage in the OFX plugin.

## Repository map

| Path | Product / purpose |
|---|---|
| Root `*.go`, `camera/`, `rgblight/`, `lightlevel/` | Scanning app GUI, tethering and light leveling |
| `hdrscan/`, `rawconv/`, `cropper/`, `basestat/` | App capture/merge, RAW conversion and image tools |
| `filmdb/`, `scanconfig/`, `scanmeta/` | App presets, configuration and scan metadata |
| `ofx/src/`, `ofx/vendor/` | OFX C++/OpenCL implementation and vendored SDK headers |
| `ofx/build.sh`, `ofx/test.sh`, `ofx/tests/` | Plugin build, math/metadata/host tests and benchmark |
| `template.dctl`, `template_core.dctl` | OFX code-generation inputs only; not embedded or exported by the app |
| `colorsci/`, `develop/`, `docs/data/` | Color science, built-in Develop evaluator and calibration/research data |
| `cmd/`, `third_party/` | QA tools and app dependencies, including LibRaw/walk |
| `docs/`, `ofx/README.md` | App/shared docs and plugin installation/usage |

[Release history index](CHANGELOG.md) · [Independent versioning policy](docs/VERSIONING.md)

## Build from source

App (Linux → Windows x64; MinGW, Go/CGO and vendored LibRaw required):

```sh
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ GOOS=windows GOARCH=amd64 \
  go build -trimpath -ldflags "-H windowsgui -s -w" -o NegativeScanStudio.exe .
```

The app uses `rsrc_windows_amd64.syso` and `third_party/libraw/lib/windows_amd64` static libraries. Plugin build:

```sh
./ofx/build.sh /absolute/output/directory
```

Plugin dependencies and optional OpenCL tests are documented in [ofx/README.md](ofx/README.md). Building either product does not require rebuilding the other.

## License

MIT © 2026 Grok Bot. Maintained for leon117ge. Third-party components retain their own licenses: walk (BSD-3), LibRaw (LGPL/CDDL), OpenFX (BSD-3), OpenCL headers (Apache-2.0) and nlohmann/json (MIT); see the corresponding vendored license files.

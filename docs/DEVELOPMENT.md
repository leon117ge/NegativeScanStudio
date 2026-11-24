# Development guide

This document is for people (and agents) who will continue NegativeScan Studio after the 2026-11-24 archive snapshot. Product behaviour and hardware are described in [ARCHITECTURE.md](ARCHITECTURE.md) and [STATUS.md](STATUS.md). Versioning policy is in [VERSIONING.md](VERSIONING.md).

The repository holds **two independently versioned products**:

- the Windows scanning app (Go + walk), current **v1.13**
- the DaVinci Resolve OpenFX plugin (C++), current **0.90.14**

Building one does not require rebuilding the other.

## Toolchain

Work is done on the Linux box and cross-compiled to Windows x64.

| Tool | Used for |
|---|---|
| Go (module path in `go.mod`) + CGO | Scanning app |
| `x86_64-w64-mingw32-gcc` / `x86_64-w64-mingw32-g++` | App CGO and the OFX Windows DLL |
| Host `c++` (or `NATIVE_CXX`) | OFX native tests on Linux |
| Python 3 | `ofx/tools/generate.py`, `ofx/tools/cuda_source.py`, parity / OpenCL checks |
| Optional: PoCL / system OpenCL | `NSS_TEST_OPENCL=1` plugin tests |
| Optional: NVRTC 12.x | Regenerating CUDA PTX (`docs/data/ofx_0902_cuda_build.json` records the last official build: NVRTC 12.0.140) |

The app also needs the vendored LibRaw static library at `third_party/libraw/lib/windows_amd64` and the Windows resource object `rsrc_windows_amd64.syso`. The plugin vendors the OpenFX headers, OpenCL headers, and nlohmann/json under `ofx/vendor/`.

## Build the scanning app EXE

From the repository root:

```sh
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ GOOS=windows GOARCH=amd64 \
  go build -trimpath -ldflags "-H windowsgui -s -w" -o NegativeScanStudio.exe .
```

Ship the binary as `NegativeScanStudio_vX.YY.exe` (plain version in the filename, matching the tag `vX.YY`). Do not commit the EXE to git; attach it to the GitHub release.

Linux unit tests for non-UI packages:

```sh
go test ./...
```

Some packages use simulated cameras and lights (see below). GUI layout and real PTP / MVS / serial I/O need a Windows machine and the hardware.

## Build the OFX ZIP

```sh
./ofx/build.sh /absolute/output/directory
```

The script regenerates generated headers, cross-compiles `NegativeScanStudio.ofx` into `NegativeScanStudio.ofx.bundle/Contents/Win64/`, copies `ofx/README.md`, the license files, and `export_metadata.py`, and writes `NegativeScanStudio_OFX_v0.90.14_Win64.zip` (update the version string in `ofx/build.sh` when you bump the plugin).

Install on Windows: quit Resolve, remove older bundles, copy the entire `NegativeScanStudio.ofx.bundle` folder to:

```text
C:\Program Files\Common Files\OFX\Plugins\
```

## Run plugin tests

```sh
./ofx/test.sh
```

Default scan fixture: `/workspace/uploads/0206.tiff` (also hardlinked at `/workspace/nss-archive/test-frames/xt3-0206/0206.tiff`). Override with the second argument: `./ofx/test.sh /tmp/ofx-test /path/to/scan.tiff`.

The script runs, in order:

1. `ofx/tools/generate.py` and `cuda_source.py`
2. `tone_test` — Tone / Tone Color math (off = 0.90.11 bit for bit; colour-off = 0.90.12 bit for bit)
3. Go metadata writer fixtures and `host_test`
4. Leveling-metadata tests (after-profile vs scene)
5. Mocked CUDA Driver contract and overlay tests
6. Python metadata and CPU parity checks

OpenCL (PoCL or another ICD) is **opt-in**:

```sh
NSS_TEST_OPENCL=1 ./ofx/test.sh
```

That builds `gpu_bridge.so` and runs `ofx/tests/check_opencl.py`. CPU tests alone do not prove GPU behaviour. Real Windows Resolve and a physical NVIDIA GPU remain a manual acceptance step; see [STATUS.md](STATUS.md).

Optional benches: `ofx/tests/benchmark.sh` (CPU math) and `ofx/tests/benchmark_host.sh` (minimal host Render). Those numbers are Linux box timings, not Resolve FPS.

## Simulators

When the real device or vendor SDK is missing, the app still enumerates a simulator:

| Package | Role |
|---|---|
| `camera/sim.go` | Fujifilm X-T3 PTP stand-in for tests |
| `rgblight/sim.go` | CuteNewDesign RGB Light Air protocol stand-in |
| `hikcam/sim.go` | Hikrobot MVS stand-in; always offered if the SDK or camera is absent |

Use these for unit tests and Linux UI-less logic. They are not a substitute for hardware acceptance of exposure, leveling, or live view.

## Repository layout

| Path | Purpose |
|---|---|
| Root `*.go` | Windows GUI pages and glue (walk) |
| `camera/`, `hikcam/`, `rgblight/`, `lightlevel/`, `hikbase/` | Camera and RGB light control, film-base leveling |
| `hdrscan/`, `monocap/`, `focusmark/` | HDR / trichrome / mono capture, LiveAE, focus marks |
| `rawconv/`, `cropper/`, `basestat/` | LibRaw conversion, crop/merge, base statistics |
| `filmdb/`, `scanconfig/`, `scanmeta/` | Presets, config, `nss.scan/1` metadata |
| `colorsci/`, `develop/`, `convert/` | Colour science, in-app Develop preview, Convert page |
| `ofx/src/`, `ofx/vendor/` | Plugin implementation and SDKs |
| `ofx/build.sh`, `ofx/test.sh`, `ofx/tests/`, `ofx/tools/` | Plugin build, tests, code generation |
| `template.dctl`, `template_core.dctl` | Inputs to `ofx/tools/generate.py` only — not app assets |
| `docs/`, `ofx/README.md`, `ofx/CHANGELOG.md` | Documentation |
| `cmd/`, `third_party/` | QA tools and vendored app dependencies |
| `develop/luts/` | Embedded Kodak 2383 cubes for the Develop preview |

A box-side file archive (binaries, test frames, references, WIP patches) lives at `/workspace/nss-archive/` and is described by `/workspace/nss-archive/INDEX.md`. It is not part of the git repository.

## Release and versioning rules

- App tags: `vX.YY` (example: `v1.13`). Plugin tags: `ofx-vX.YY.Z` (example: `ofx-v0.90.14`). The two products do not have to ship together.
- Use **plain versions** in tags, filenames, UI labels, and changelogs. Do not invent marketing names.
- Every released (and every local test) version gets a changelog entry: app in `docs/CHANGELOG.md`, plugin in `ofx/CHANGELOG.md`. Update `README.md` current-version links when you publish.
- Release titles identify the product, e.g. **NegativeScan Studio v1.13** and **NegativeScan Studio OFX 0.90.14**. Attach the EXE or the OFX ZIP, and put the SHA-256 in the release notes.
- Do not rewrite existing release tags or quietly replace their binaries. A docs-only change needs no new binary tag.
- Annotated tags. Prerelease is appropriate until hardware / Resolve acceptance is signed off; do not mark a draft unless the assets are not ready.
- `nss.scan/1` is a metadata schema version, not a product version.

Full policy: [VERSIONING.md](VERSIONING.md).

## Contributing rules for agents

Follow these even if a task is in a hurry.

1. **English only** in app UI, plugin UI, source comments that ship, commit messages, and documentation that you add or revise. Do not add Chinese to app or plugin files. Historical Chinese in older notes may be translated when you touch the file; do not introduce new bilingual sections.
2. Do not invent film-base values, stock Base presets, or “Kodak constants”. New calibrations need a measurement, source, conditions, and uncertainty, recorded under `docs/data/`.
3. Do not force-push, delete branches or tags, or rewrite published release history. Fast-forward only.
4. Do not commit unfinished experiments to `main`. The 2026-11-24 leftovers — uncommitted `ofx/src/metadata.hpp` matrix/metadata parsing, and `stash@{0}` “wip v0.90 400K presets” — stay off `main`. The metadata work lives on `wip/metadata-matrix` and as a patch in `/workspace/nss-archive/wip/`. Leave the stash in place unless a human asks to drop or apply it.
5. Do not delete user uploads (`/workspace/uploads`) or other irreplaceable frames. Prefer hardlink or copy into `/workspace/nss-archive/` when filing things.
6. Keep Tone math changes covered by `tone_test` (including “off = previous version bit for bit” where that is the contract) and run `./ofx/test.sh` before you call a plugin change done. Use `NSS_TEST_OPENCL=1` when the change touches GPU kernels.
7. Plugin label, `ofx/build.sh` zip name, `ofx/README.md`, `ofx/CHANGELOG.md`, and `docs/OFX.md` must agree on the version you are shipping.
8. The GitHub repository stays a normal, writable repo. “Archiving” this project means a snapshot tag and the box file archive, not GitHub’s read-only Archive flag.

See also [CONTRIBUTING.md](../CONTRIBUTING.md).

# macOS (Apple Silicon) build notes

Test builds only, not released yet.

- `ofx/build-mac.sh [out]` (on a Mac with the Xcode command line tools) builds
  `NegativeScanStudio.ofx.bundle` for arm64 with the CPU render path, signs it ad hoc and
  zips it with `Install.command` and `README-macOS.txt` (all at the zip root) as
  `NegativeScanStudio_OFX_v<version>_macOS_arm64.zip`. The version comes from plugin.cpp
  (label and OfxPlugin major/minor must agree; the build stops otherwise) and is stamped
  into Info.plist.
- Render configs: `NSS_METAL=auto` (default) adds the Metal path when
  `ofx/src/metal_render.mm` exists (`-DNSS_METAL`, `-fobjc-arc`, `-framework Metal
  -framework Foundation`); `NSS_METAL=0` builds CPU only; `NSS_METAL=1` requires Metal.
- `Install.command` copies the bundle to `/Library/OFX/Plugins` (sudo), clears the
  `com.apple.quarantine` flag and re-signs the installed bundle ad hoc.
- `ofx/src/platform.hpp` holds the build switches: CUDA and OpenCL are compiled out on
  macOS (`NSS_WITH_CUDA`, `NSS_WITH_OPENCL`); the CPU worker count has no fixed cap on
  macOS (`NSS_CPU_THREAD_CAP` 0 = every hardware thread the OS reports). Windows keeps
  CUDA, OpenCL and the cap of 8; its preprocessed source is identical to main.
- Rendering controls (one mechanism, `ofx/src/platform.hpp`): choice indices are fixed on
  every build, 0 = Auto, 1 = CPU, default 0. Windows/Linux: hidden, `Auto (CUDA/OpenCL if
  offered) | CPU` (0.90.14). macOS with Metal: visible, `Auto (Metal) | CPU`. macOS CPU-only
  (`NSS_METAL=0`): visible, `Auto | CPU` (both render on the CPU; `Show render engine`
  still reports the engine). `NSS_MAC_RENDER_UI` and `NSS_RENDER_AUTO_METAL` let tests emulate
  each build on any OS (`ofx/tests/param_dump.cpp`, `check_param_defs.py`).
- The overlay loads GL from `/System/Library/Frameworks/OpenGL.framework/OpenGL` and
  checks the host context with `CGLGetCurrentContext`.
- Math flags match the Windows build; `-ffp-contract=off` keeps `a*b+c` as two rounded
  operations as on x64 (no fused multiply-add). No chip-specific `-mcpu`.
- CI: `.github/workflows/macos-w1-ofx-cpu.yml` (macos-15 and macos-26), then the shared
  `macos-build-checks.yml` (CJK, versions, docs) on the uploaded zip. It checks that the Windows source is
  unchanged, renders the parity variants with the Windows x64 build on a Windows runner,
  then on a macOS arm64 runner builds and inspects the bundle, runs the native tests,
  runs `Install.command` on a quarantined copy and loads the installed binary, and
  compares the CPU renders with the Windows ones (`ofx/tests/cpu_parity.cpp`,
  `ofx/tests/check_cpu_parity.py`; gate |mac - win| <= 1e-5 + 1e-5 * |win| per value).

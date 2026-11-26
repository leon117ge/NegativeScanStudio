# macOS (Apple Silicon) build notes

Test builds only, not released yet.

- `ofx/build-mac.sh [out]` (on a Mac with the Xcode command line tools) builds
  `NegativeScanStudio.ofx.bundle` for arm64 with the CPU render path, signs it ad hoc and
  zips it with `Install.command` and `README-macOS.txt` as
  `NegativeScanStudio_OFX_v<version>_macOS_arm64.zip`. The version comes from plugin.cpp.
- `Install.command` copies the bundle to `/Library/OFX/Plugins` (sudo), clears the
  `com.apple.quarantine` flag and re-signs the installed bundle ad hoc.
- `ofx/src/platform.hpp` holds the build switches: CUDA and OpenCL are compiled out on
  macOS (`NSS_WITH_CUDA`, `NSS_WITH_OPENCL`); the CPU worker count has no fixed cap on
  macOS (`NSS_CPU_THREAD_CAP` 0 = every hardware thread the OS reports). Windows keeps
  CUDA, OpenCL and the cap of 8; its preprocessed source is identical to main.
- The overlay loads GL from `/System/Library/Frameworks/OpenGL.framework/OpenGL` and
  checks the host context with `CGLGetCurrentContext`.
- Math flags match the Windows build; `-ffp-contract=off` keeps `a*b+c` as two rounded
  operations as on x64 (no fused multiply-add). No chip-specific `-mcpu`.
- CI: `.github/workflows/macos-w1-ofx-cpu.yml`. It checks that the Windows source is
  unchanged, renders the parity variants with the Windows x64 build on a Windows runner,
  then on a macOS arm64 runner builds and inspects the bundle, runs the native tests,
  runs `Install.command` on a quarantined copy and loads the installed binary, and
  compares the CPU renders with the Windows ones (`ofx/tests/cpu_parity.cpp`,
  `ofx/tests/check_cpu_parity.py`; gate |mac - win| <= 1e-5 + 1e-5 * |win| per value).

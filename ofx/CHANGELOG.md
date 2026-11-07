# NegativeScan Studio OFX — Changelog

Current plugin: **0.90.1** · [Plugin release](https://github.com/leon117ge/NegativeScanStudio/releases/tag/ofx-v0.90.1) · [Scanning app changelog](../docs/CHANGELOG.md) · [Versioning policy](../docs/VERSIONING.md)

Plugin releases are independent from the Windows scanning app. / 插件与扫描软件独立发版。

## 0.90.1 — plugin-only prerelease

- Remove the mask/crosstalk group and stage. Keep camera calibration.
- Merge stock gamma, T/D, illuminant and film-base controls. Presets fill visible sliders, default base is .5/.5/.5, and metadata provides a baseline without repeated compensation.
- Keep Film white balance as a loader. 500T Base writes +1.57/0/+1.00 into visible RGB balance sliders. None resets them to zero. Remove hidden duplicate gains and the old white-code slope. Keep T5600 offsets unchanged.
- Add a host-native viewer gray/white point and sample radius. Sample Source through Density In. OpenCL downloads only the small patch for this optional feature.
- Harden TIFF/JSON loading with short status messages, Unicode/quoted/file URL paths, classic/BigTIFF in either byte order and 64-bit offsets. Read IFD/description without pixels. The old 0206 scan has no tag270.
- Add runtime OpenCL buffers, CPU fallback, per-frame constants and up to eight CPU threads. The matched full-frame Linux CPU benchmark measures 3779.94 to 331.55ms median, not Resolve FPS.
- Pass CPU/metadata/preset tests and actual PoCL CPU OpenCL execution. New 0.90.1 behavior remains unverified in real Windows Resolve or on a physical GPU. See [implementation and verification](../docs/OFX.md).

## 0.90 — initial CPU preview (historical)

- First Windows x64 combined CPU OFX: camera calibration → Density In → mask/crosstalk → Positive Out, with original DCTL defaults.
- T/D illuminant and Kodak stock-gamma presets; TIFF/JSON loading and optional Resolve source-path probing; host-free lifecycle and independent Go math checks.
- Initially shipped with the [app v0.90 release](https://github.com/leon117ge/NegativeScanStudio/releases/tag/v0.90), before independent plugin tags. Its historical ZIP remains there; the current plugin is the separate 0.90.1 release.
- The mask/crosstalk stage and hidden legacy-WB behavior described here were removed/replaced in 0.90.1. See [current usage](README.md), not this historical entry, for installation and parameters.

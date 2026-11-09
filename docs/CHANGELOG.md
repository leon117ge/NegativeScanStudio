# NegativeScan Studio app — Changelog / 扫描软件更新日志

Current local test app: **v0.94**, not published. Latest published app: **v0.91** · [App release](https://github.com/leon117ge/NegativeScanStudio/releases/tag/v0.91) · [OFX plugin changelog](../ofx/CHANGELOG.md) · [Versioning policy](VERSIONING.md)

This file tracks the scanning app, not plugin-only releases. / 本文件只记录扫描软件。

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

根据源码中的版本注释、备份文件与说明文件整理（v0.70 之前为 FilmBaseMeter v0.1–v0.67）。

- **v0.70** – Develop 模块：三个 DCTL 移植到 Go，Resolve 式节点链；Ektar 相关数据。
- **v0.71** – Develop 预览按帧旋转/翻转；默认输出为显示编码；嵌入 2383 (D65) LUT；卷打开与片基估计。
- **v0.72–v0.74** – 色彩/测试参数与命令行选项调整。
- **v0.75** – X-T3 实测预设与矩阵行；White LED / Narrowband 光源选择；QA 合成工具。
- **v0.76** – 矩阵行缩放保持中性等默认值调整。
- **v0.77** – 引导式去毛发/纤维。
- **v0.78** – Vision3 500T 相关数据与测试、默认值调整。
- **v0.79** – 纤维标记改进。
- **v0.80** – RGB Light Air 设置页；ISO + 固定快门；传感器马赛克（感光点平均）合成。
- **v0.81** – 三色扫描驱动灯；base shutter；串口原始日志；CFA 逐色缩放。
- **v0.82** – 调平结果与 Light 页联动；三色片基送 DCTL；三色+HDR 扫描。
- **v0.83** – 扫描配置预设（scanconfig）；HDR 子页重组；可停止的包围；RAW 清理；NegPy 风格折叠卡片。
- **v0.84** – 相机+灯输入色卡校准（3×3 保持中性，CIEDE2000）。
- **v0.85** – 调平逐通道目标（profile 矩阵前片基、TIFF 框修正）。
- **v0.86** – 调平/Light 页改进。
- **v0.87** – 三色自动调平支持手选片基矩形。
- **v0.88** – Light 页共享实时画面直接拖框；调平 R/G/B 读数显示、复制、CSV 日志；「写入 DCTL」写入 Density_In 片基与预设。详见 `NegativeScanStudio_v0.88_notes.txt`。

- **v0.89** – 优先实现 500T 3200K 平衡。基于 Kodak H-1-5219 rev.3-26 的光谱感度 10 nm 转录、Planck 光源积分和 ECN-2 gamma，生成有来源的近似密度偏移。支持 3200/4300/5500/6500K 和自定义 K，Light 页写入 DCTL、胶片库保存与加载、发送到 Develop。调平后不改灯、不改片基、不改相机 WB。Density_In 增加默认关闭的 `cast_on/cast_r/g/b`，Go Develop 同步公式。灰点实测与交叉偏色拟合后续实现。

- **v0.90** – HDR/三色扫描 TIFF 的 ImageDescription/tag270 写入 versioned `nss.scan/1` scan JSON（已知灯值、片基、相机矩阵/状态、灯型号、卷名、版本等）；hair/dust/crop保留。Go TIFF writer/headless往返测试通过，Windows exe交叉编译。首次同一release同时附带OFX v0.90原型，插件历史现独立记录于 [ofx/CHANGELOG.md](../ofx/CHANGELOG.md)。app仍为v0.90；OFX0.90.1不升级app。取消的400K WIP未应用。

## 仓库说明
仓库已通过 git 上传完整源码和构建所需的图标、资源与 LibRaw 静态库。Windows 可执行文件放在 GitHub Releases，不随源码重复提交。

The root `template.dctl` and `template_core.dctl` remain unchanged as inputs to `ofx/tools/generate.py`. They are not app assets or app export features. Removing them would break OFX regeneration, so app-only cleanup does not delete them.

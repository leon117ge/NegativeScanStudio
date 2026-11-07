# 设计文档 / Design

> Historical reference through v0.90. App v0.91 removes the DCTL page, all DCTL generation/export, and related write controls. The formulas below remain relevant to the built-in Develop preview, but the old page layout and export instructions are retired. See [current app workflow](APP_WORKFLOW.md), [v0.91 changelog](CHANGELOG.md#v091), and [OFX usage](../ofx/README.md).


## 总体架构
单一 Windows 可执行文件，GUI 使用 lxn/walk（本地 fork 在 `third_party/walk`）。业务逻辑尽量放在无 UI 依赖的包里（`hdrscan`, `lightlevel`, `colorsci`, `develop`, `cropper`, `rgblight`, `camera`, `basestat`），根目录只是页面与粘合层，因此算法可在 Linux 上测试（`go test ./...` 各包）。

## 页面
- **1. DCTL**：Film base 测量（打开 TIFF，选区域，`basestat` 统计）、片基预设表、导出三个 DCTL。
- **2. Camera & Light**：Camera（联机、实时预览、拖框）、Light（RGB Light Air 设置、自动片基调平，v0.88 起有共享实时画面）、White light、Trichrome。四页共用一个相机连接与同一个片基选框（`levelcam.go`）。
- **3. HDR Scan**：片基测光 → 包围曝光拍摄 → 合成 HDR TIFF；三色扫描。
- **4. Crop & Merge**：裁切、合并文件、去毛发（`hairpage.go`）、Develop 预览（`develop` 包）。

## 数据流
```
相机 X-T3 (PTP/WPD) ──RAW──▶ rawconv (LibRaw, 线性 16-bit TIFF)
       ▲                           │
RGB Light Air (串口)               ▼
  白光 / 单色 R,G,B ──▶ hdrscan: 片基测光 → 包围 (0/+2/+4 EV) → MergeN → *_HDR.tiff
                         trichrome: R/G/B 三帧各取对应通道 → RGB TIFF
                                   │
                                   ▼
          片基读数 (basestat / lightlevel 调平结果) → presets / filmdb / scanconfig
                                   │
                                   ▼
 DCTL: FilmScan_Density_In → FilmScan_MaskCrosstalk → FilmScan_Positive_Out (+ 2383 LUT)
       （develop 包用 float32 在 Go 中复现同一链，供预览）
```

## 关键设计约定
- 所有片基数值为 **线性 TIFF 值 / 65535**，各模块同一统计方法（`basestat`），保证 HDR 模块送来的片基与模块 1 测得的完全同尺度。
- HDR 合成以 frame 0 为尺度，片基附近像素严格保持 frame 0 值。
- 调平只改灯，不改相机曝光；相机用固定 ISO/快门。
- `rgblight` 只允许发送官方文档列出的命令，拒绝其它字节（不碰序列号写入/固件升级）。
- 设置与日志位于 `%APPDATA%\NegativeScanStudio\`（如 `leveling_log.csv`）。

## 500T 光源平衡（v0.89）

`colorsci/illuminant.go` 用 Kodak 感度和 Planck 光谱计算名义密度偏移。
`castpage.go` 负责 Light 页控件，`castconfig.go` 保存独立导出设置。
`filmdb.Entry.ColourCast` 保存胶片预设。`template.dctl` 与 `develop/eval.go`
使用同一密度加法。默认关闭，不影响现有扫描与导出行为。

灯光只负责片基调平，调平后不再改动。光源偏色和后续的交叉偏色校正均在 DCTL 中完成。
模型不把代表性胶片特性当作本卷实测。数据来源和适用边界见算法文档。

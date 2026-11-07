# 算法 / Algorithms

所有描述来自源码注释与实现。

## 1. 片基统计与检测（`basestat`, `hdrscan/basefind.go`）
- 值域：线性 16-bit / 65535。对干净片基区域按 R+G+B 剔除漏光（偏离干净片基的像素），然后逐通道取中位数。
- 自动模式：`hdrscan` 在画面中找最亮且均匀的片基区域。
- 手选（v0.87 起）：用户在实时画面上拖一个矩形（归一化坐标 `lightlevel.Rect`），设为 Rectangle 模式后只测该框；未画框时拒绝调平。Auto 模式记录的归一化框为全零。

## 2. 片基闭环调平（`lightlevel`）
- 相机曝光固定，只改灯的 R/G/B 亮度（1..100）。
- 每轮每个通道单独点亮该 LED 拍一张，在对应相机通道测片基中位数（同时记录 P99 与削顶比例）。
- 新亮度 = 旧亮度 × 目标 / 实测；重复直到全部通道在容差内。
- 默认参数：Target 0.50 FS、Tol ±2 %、MaxIter 4、Settle 300 ms、测量区默认中心 25–75 %。
- v0.85：`Targets[3]` 可逐通道覆盖目标（用于相机 profile 矩阵前片基与 TIFF 框修正）。
- 亮度到 1 仍超目标会报告 TooBright。
- v0.88：最终结果复核用实际 trichrome TIFF；结果写入 CSV 日志，可一键写入 `FilmScan_Density_In` 的 `base_r/g/b` 与 “Light leveling” 片基预设；复核失败、停止或不收敛不写入。

## 3. 相机色卡校准（`colorsci/camcal.go`, `chartcal.go`, v0.84）
- 24 块透射色卡（4×6，A 行为中性灰阶 A1–A6），参考数据为 IT8.7/1 CGATS（XYZ D50 或 Lab）。
- `DetectCamChart` 在线性 16-bit 图里找网格并定向（A 行亮度递增、通道比稳定 → `rampScore`）。
- 削顶检测：超过阈值的像素比例判定色块削顶。
- 拟合 **保持中性的 3×3 矩阵**（每行和约束使灰保持灰），最小化平均 **CIEDE2000**；色彩转换 XYZ D50 → Bradford → D65 线性 Rec709。

## 4. 密度空间 DCTL
- **FilmScan_Density_In**：线性扫描 / 片基 (`base_r/g/b`) → Cineon 风格密度码。参数：片基码 `dmin_code`=95、每码密度 0.002、满量程 1023、背光曝光偏移、黑位扣除等。
- **FilmScan_MaskCrosstalk**：密度空间橙色片基补偿（`bd_r/g/b`）与 3×3 染料串扰矩阵，默认无变化。
- **FilmScan_Positive_Out**：逐通道 gamma（默认 R 0.55 / G 0.635 / B 0.62）、曝光、平衡、灰点、输出编码（Rec709 / DaVinci Intermediate / Cineon / ACEScct / Linear），可接 Kodak 2383 打印 LUT。
- `develop` 包以 float32 1:1 复现上述链用于程序内预览。

## 5. HDR 合成（`hdrscan/merge*.go`）
- `MergeN`：frame 0 + 更亮的 +2/+3/+4 EV 帧，以 frame 0 尺度合成。
  1. 帧间比值在“亮帧未削顶、暗帧高于 1 % 噪声底”的像素上逐通道中位数测得并链式相乘；
  2. 失败则用曝光时间×ISO 的名义比值，并按 +2 帧实测/名义修正；
  3. 无曝光信息时 +2 帧假设为 4 倍。
- 从暗到亮混合，混合带由亮帧（膨胀后）削顶掩膜驱动；距片基 ~1 EV 内保持 frame 0 原值。
- Weighted / 传感器马赛克方法（v0.80）：片基保护带 ≥0.85·L 用 frame 0，<0.70·L 用合成结果；可按感光点平均。
- 支持对齐（中央绿色裁切 ±r px 位移估计）。

## 6. 去毛发 / 纤维（`hdrscan/hair.go`, v0.77）
用户框选区域内：L = log2(R+G+B+3) 半分辨率 → 24 方向二阶导核 → 鲁棒 Z 值（MAD）+ 非极大抑制 Z≥2 → 连通分组取最长纤维 → 全分辨率中心线细化、生成修补带 → 沿中心线 24 px 分块，从 24 个偏移候选块中按环域对数匹配选最佳，再用 Shepard 膜插值无缝融合。带外像素不变；不提供全幅自动检测（会误检真实线条）。

## 7. 裁切（`cropper`）
检测画面与片基/片夹的四条边界（`Side`/`Edge`），几何拟合后裁切，输出 TIFF（含 LZW）。

## 8. 三色扫描（`hdrscan/trichrome.go`）
R、G、B 灯分别点亮各拍一张（可包围），每帧只取对应相机通道，合成 RGB TIFF；片基同样用 `basestat` 统计，可直接送 DCTL。

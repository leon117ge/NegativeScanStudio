# 算法 / Algorithms

> Historical reference through v0.90. App v0.91 removes the DCTL page, all DCTL generation/export, and related write controls. The formulas below remain relevant to the built-in Develop preview, but the old page layout and export instructions are retired. See [current app workflow](APP_WORKFLOW.md), [v0.91 changelog](CHANGELOG.md#v091), and [OFX usage](../ofx/README.md).


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

## 9. 500T 3200K 光源平衡（v0.89）

500T 5219/7219 的标称平衡是 3200K 钨丝灯。Kodak H-1-5219 rev.3-26
给出光谱感度和 ECN-2 特性曲线。[转录记录](data/5219_digitization.md)
包含来源、图像坐标、10 nm 采样值的路径和误差说明。

### 计算

令 `L_c(λ)` 为 Kodak 图的 log sensitivity，`S_c(λ)=10^L_c(λ)`。
缺失曲线段的感度设为零，不把空白当作 logS=0。

```text
P(λ,T) ∝ λ^-5 / (exp(c2/(λT)) - 1), c2 = 1.438776877e7 nm K
E_c(T) = Σ S_c(λ) P(λ,T) Δλ, Δλ = 10 nm
q_c = log10(E_c(T)/E_c(3200))
ΔlogH_c = q_c - q_G
ΔD_c = γ_c ΔlogH_c
cast_c = -ΔD_c
```

`P` 在 550 nm 归一化。归一化常数在减去 `q_G` 后抵消。
感度单位是倒数能量曝光，不能再乘一次光子数量因子 `λ`。
积分只用可见曲线样本。省略 UV 和未画出的尾部是模型限制。
`γ_R/G/B = 0.4482/0.5266/0.5096` 来自 ECN-2 中间直线段，
不是 Positive_Out 的创作 gamma。绿色曝光保持不变，因此 `cast_g=0`。

相机扫描仍用原片基归一化：

```text
D_c = -log10((I_c - black_c) * 2^EV / base_c)
D'_c = D_c + cast_c                 (cast_on = 1)
code_c = (dmin_code + D'_c / dens_step) / full_code
```

默认 `cast_on=0`、偏移为零。不开校正时数学行为不变。
`out_trans=1` 是原始透射率诊断输出，不应用密度偏移。
`develop` 的 Density In 节点使用相同公式。

5500K Planck 模型的额外密度约为 `[-0.09256, 0, +0.18864] D`，
所以 DCTL 加 `[+0.09256, 0, -0.18864] D`。
[完整计算结果](data/5219_5500K_result.json) 可用下面的命令重算：

```sh
go run ./cmd/illuminantcalc -kelvin 5500
```

### 与 85 系列滤镜的量级比较

5500K 转到 3200K 是 `1e6/3200 - 1e6/5500 = +130.68 mired`。
这与通常标作 85B 的色温转换量相同。
85 的常见标称转换是 5500K 到 3400K，即 +112.30 mired。
Kodak 的 500T 数据表推荐日光用 85、EI 320，不能据此把 85 与 85B
视为同一透射谱。参考 [Kodak 85 系列透射曲线](https://www.kodak.com/content/products-brochures/Film/Color-Conversion-Filters-85-Series-Spectral-Transmittance.pdf)。
这只是色温量级核对，不是对实物 85 滤镜光谱积分的独立验证。

### 准确性边界

- 数值是有公开来源的模型推导，不是该卷底片的实测白平衡。
- Planck 5500K 和 CIE D55 光谱不同。LED、荧光灯和混光不能只由 K 数确定。
- 中性物体近似平坦反射。真实物体的反射谱可能产生不同偏移。
- Kodak 的代表性 ECN-2 曲线不等于每卷的冲洗结果，toe 和 shoulder 也不是常斜率。
- 感光层密度轴不等于任意相机与背光的 RGB 密度轴。此版本把两者近似对应，
  没有测扫描系统的密度串扰矩阵。不能把结果宣传成精确去偏色。
- 如果拍摄时用了 85 滤镜，不要再按未滤镜的 5500K 重复校正。
- 3200K 的此项偏移为零。这只表示无需光源转换，并不保证整个转正链自然中性。
  Positive_Out 的灰点、各通道 gamma 和已有自动白平衡仍需单独检查。
- 灰点实测和灰阶交叉偏色拟合留到后续版本。

### 状态与灯光

Light 页只计算、保存和发送 DCTL 参数，不调用串口、不拍摄、不改曝光。
`colour_cast.json` 保存当前导出设置。它独立于片基 preset，下次导出所有
Density_In 都使用这组设置，切换片基不会自动清除偏色校正。
`filmdb.Entry.ColourCast` 保存每种胶片的参数，加载后须显式写入才激活。
片基数据保持原值，不用假片基偷偷代替密度偏移。
日志使用原 `leveling_log.csv` 格式，mode 为 Colour cast，source 记密度偏移，
base 和 light 的零列表示本次没有测量这些值，不代表灯被设成零。

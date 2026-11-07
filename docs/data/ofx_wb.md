# OFX T/D 白平衡密度补偿（OFX 0.90.1）

使用 `ofx/tools/generate.py` 复现；10 nm 能量感度积分，Planck visible SPD：

`E_c(K) = Σ 10^logS_c(λ) B_λ(K) Δλ`

`ΔlogH_c = log10[E_c(K)/E_c(K0)] − log10[E_G(K)/E_G(K0)]`

`cast_c = −gamma_c ΔlogH_c`，直接 **加到** Density In 的 D，不是改斜率。等效 `base_effective = base × 10^cast`。Green anchor=0；没有给它额外的全局曝光补偿。

T：5219 感度 + 名义 Status M gamma 0.4482/0.5266/0.5096，K0=3200；沿用 v0.89 方法，不应用取消的400K设计。D：官方5207感度与中段gamma，K0=5600（Kodak标称5500，端点近似，见 [5207读取说明](5207_digitization.md)）。所有场景温度包括5600均为黑体近似，**不是 CIE daylight / 实测LED光谱**。

| Film | Scene K | cast R / D | cast G / D | cast B / D |
|---|---:|---:|---:|---:|
| T | 3200 | 0.000000 | 0.000000 | 0.000000 |
| T | 3800 | +0.035046 | 0.000000 | -0.069526 |
| T | 4400 | +0.060535 | 0.000000 | -0.121472 |
| T | 5000 | +0.079811 | 0.000000 | -0.161625 |
| T | 5600 | +0.094824 | 0.000000 | -0.193475 |
| D | 3200 | -0.109197 | 0.000000 | +0.199857 |
| D | 3800 | -0.069027 | 0.000000 | +0.127565 |
| D | 4400 | -0.039687 | 0.000000 | +0.073920 |
| D | 5000 | -0.017411 | 0.000000 | +0.032640 |
| D | 5600 | 0.000000 | 0.000000 | 0.000000 |

数据表保存完整精度于 [ofx_wb_presets.json](ofx_wb_presets.json)。模型 sensitivity 是拍摄感光层，cast 应用在扫描 RGB 轴是近似；未用完整独立层响应、实际scanner QE、实际光谱/ECN-2批次标定，也没用新LED gamma代替这里的感度→密度 slope。可用作起点、同卷灰卡验证；不是精确去色保证。Portra/Gold/UltraMax 默认 D，但**共用5207色温模型**，只 gamma 各有官方曲线读取。offset 不随手调 positive gamma 重算，两者独立。

## OFX 0.90.1：直接写 Film base

不再暴露 cast 字段/Apply 按钮。选择 T/D 或温度时立即写 Film base sliders：

`base_effective_c = baseline_c × 10^(+cast_c)`

**正号**来自 `D = −log10(pixel/base)`；`base × 10^cast` 正好使 D 增加 cast。用负号会把补偿反过来。10 个预设均有回归测试，对照旧 DCTL additive cast。该等价关系指密度分支，受同样的 epsilon/clamp 边界限制；transmittance 输出现在也跟随 base，不等同于旧 cast（旧 cast 在该分支不生效）。

新实例 baseline = 0.5/0.5/0.5；Load 成功后 baseline = 文件片基经 pending camera 矩阵处理后的值。隐藏持久参数保存 baseline；反复切换/Load 不在已经补偿过的值上累乘。手改 sliders 可精调；下一次选择预设将从 baseline 重置该微调。物理灯光、扫描 TIFF 像素和 app 片基均不改。

| Film | Scene K | Film base R / G / B (baseline 0.5) |
|---|---:|---|
| T | 3200 | 0.500000 / 0.500000 / 0.500000 |
| T | 3800 | 0.542021 / 0.500000 / 0.426034 |
| T | 4400 | 0.574784 / 0.500000 / 0.378005 |
| T | 5000 | 0.600871 / 0.500000 / 0.344623 |
| T | 5600 | 0.622006 / 0.500000 / 0.320254 |
| D | 3200 | 0.388842 / 0.500000 / 0.792186 |
| D | 3800 | 0.426523 / 0.500000 / 0.670710 |
| D | 4400 | 0.456334 / 0.500000 / 0.592775 |
| D | 5000 | 0.480351 / 0.500000 / 0.539026 |
| D | 5600 | 0.500000 / 0.500000 / 0.500000 |

完整数值：[ofx_0901_base_presets.json](ofx_0901_base_presets.json)。片种 gamma 仍为独立参数。500T 默认 T，其余 stock 默认 D，均可覆盖；场景温度不自动改。Positive Out Film white balance保留，原Legacy项更名 **500T Base**，以可见balance loader写+1.57/0/+1.00，None复位0；T/D和gamma不暗改balance。500T Base测于T5600+LED gamma，保持T5600表不变，两者可叠加。[Base记录](ofx_film_bases.md)。

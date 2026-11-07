# OFX T/D 白平衡密度补偿（v0.90）

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

应用预设写 cast 三通道/启用 cast、关闭 legacy Positive Film WB；manual fine tune 保留。保持物理灯光与实测片基不变，补偿只在插件内进行。

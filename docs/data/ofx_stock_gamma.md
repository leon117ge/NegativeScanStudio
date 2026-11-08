# OFX 片种 gamma 预设（v0.90）

这些值是项目从 Kodak 官方曲线读出的 **Status M 中段斜率估计**，不是 Kodak 发布的逐通道参数表，不是每卷/扫描灯标定。默认仍为旧 0.55/0.635/0.62，选择预设才填入 sliders，之后仍能手改。

| Preset | R | G | B | WB default |
|---|---:|---:|---:|---|
| Legacy/manual | 0.5500 | 0.6350 | 0.6200 | 保留 |
| Vision3 500T 5219 Status M | 0.4482 | 0.5266 | 0.5096 | T |
| 500T + RGB Light Air LED estimate | 0.4500 | 0.5400 | 0.5300 | T |
| Vision3 250D 5207 Status M | 0.4815 | 0.5602 | 0.5417 | D |
| UltraMax 400 Status M | 0.5186 | 0.5363 | 0.6048 | D |
| Portra 400 Status M | 0.5559 | 0.5496 | 0.6400 | D |
| Gold 200 Status M | 0.5507 | 0.5644 | 0.6104 | D |

5219 来源/读图方法/±0.03工程不确定性见 [5219_digitization.md](5219_digitization.md)。LED 0.45/0.54/0.53 是 [光谱模型](5219_led_gamma.md) 的舍入试验值，非Status M数据/非设备实测。5207 来源、March2026版与手读坐标见 [5207_digitization.md](5207_digitization.md)，约±0.03。

## 静态彩负：官方矢量曲线读取

以下均为 daylight exposure、C-41、Status M，读图验证标签 R/G/B 为下/中/上曲线。**Vector path索引仅对这次记录的 PDF edition/hash 有效**。

- **UltraMax 400** — February 2016 E-7023，PDF 第4页。https://kodakprofessional.com/sites/default/files/wysiwyg/pro/resources/E7023_max_400.pdf
  - SHA256 `598f44c39c2cd420554d9e7395ce7213e8bf9f1815fbf550dadaeb625826c77d`；RGB path IDs [35, 34, 33]。
- **Portra 400** — January 2025 E-4050, revised 1-25，PDF 第4页。https://www.kodakprofessional.com/sites/default/files/2025-07/e4050.pdf
  - SHA256 `70b15171673c5d01505f58ace448995ad72acbe5e401d1966563a61ac0712838`；RGB path IDs [36, 35, 34]。
- **Gold 200** — June 2023 E-7022, revised 06-23，PDF 第4页。https://www.kodakprofessional.com/sites/default/files/wysiwyg/pro/resources/E7022%20Gold%20tech%20sheet.pdf
  - SHA256 `8f20aa3ca762c898591f30479ead35587adf43eecc22a5a49acc48e54745f062`；RGB path IDs [38, 37, 36]。

复现：`python tools/digitize_still_gamma.py DIRECTORY`（需 pymupdf、numpy，目录含 ultramax.pdf / portra.pdf / gold.pdf）。不再分发 Kodak PDF/图像。提取原 polyline 顶点；Gold 的 cubic Bezier 每段101点展开；校准图框/轴刻度，转为 logH 与 density。原顶点及转换结果在 `colorsci/data/*_characteristic.csv`，具体轴坐标、SHA、拟合残差/替代窗口在 [ofx_stock_gamma_readings.json](ofx_stock_gamma_readings.json)。

主斜率窗口 logH=-1.5..0.0（lux seconds，61个等间隔点、曲线内线性插值、带截距OLS），不是 toe/shoulder 拟合。对不同窗口的敏感性：

| Stock/channel | 主窗口 gamma | -2..-0.5 | -1..0.5 |
|---|---:|---:|---:|
| UltraMax 400 R | 0.5186 | 0.5168 | 0.4946 |
| UltraMax 400 G | 0.5363 | 0.5377 | 0.4974 |
| UltraMax 400 B | 0.6048 | 0.6012 | 0.6052 |
| Portra 400 R | 0.5559 | 0.5410 | 0.5675 |
| Portra 400 G | 0.5496 | 0.5528 | 0.5458 |
| Portra 400 B | 0.6400 | 0.6360 | 0.6440 |
| Gold 200 R | 0.5507 | 0.5411 | 0.5047 |
| Gold 200 G | 0.5644 | 0.5685 | 0.5066 |
| Gold 200 B | 0.6104 | 0.6200 | 0.5663 |

向高光移动窗口，Gold R/G 和 UltraMax G 可变约0.04–0.06，因此不能只引用“±0.01数值精度”当实际不确定性。原图矢量坐标量化误差远小于形状/窗口/生产工艺系统误差；主窗口工程估计约±0.03，Gold/UltraMax 含肩部时可达±0.06或更大，**非置信区间/批次保证**。这些曲线不描述每次实际C-41/ECN-2工艺差异。原始表中很多小数仅为复现，不代表物理精度。

Film Stock 选项填 gamma 并默认500T=T、其它=D，用户可覆盖 T/D。选片不改变未验证的光谱串扰矩阵；不假设Status M slope经过任意矩阵后仍准确。新预设与旧legacy WB不叠加；灰点仍可独立使用。

## OFX0.90.2的可见scale

本页官方曲线读取值与预设数值均保持不变，没有统一乘1.25。Positive Out的Gamma scale位于Gamma B下，默认1.15，作为用户选择的扫描观感/对比因子。实际render gamma=可见R/G/B×scale。Exposure默认0.90。它们不是乳剂gamma重新测量，亦不更新T/D积分表。纯公式验证用scale1，或给参考同一effective gamma。旧500T Base测量对应scale1；0.90.2当前Base1.40/0/.80为新默认下用户眼调，未重新测量。

## OFX 0.90.6: Ektar 100

# NegativeScan Studio OFX 0.90.6

- Add Kodak Ektar 100 to Film stock gamma preset. Selecting it writes R **0.580572786**, G **0.579564388**, B **0.663740443** into the visible gamma sliders and defaults Film type to **D**; manual T/D override remains available.
- Gamma is a project estimate from Kodak E-4046 (January 2025, revised 1/25), page 4, daylight Status M characteristic vector curves; not Kodak-tabulated channel constants. Use the same 61-point, logH -1.5..0, interpolated OLS method as the other still stocks. See docs/data/ofx_stock_gamma_readings.json for full source, PDF SHA-256, axes, residuals and alternate fit windows.
- No Ektar Base preset is added: the existing table has only the calibrated/eye-tuned 500T Base, with no uncalibrated placeholders. Existing Base, gamma presets, Gamma scale **1.15**, Exposure **0.90**, and CPU/CUDA/OpenCL kernels are unchanged.
- Plugin-only release; scanning app stays **v0.91**.


PDF SHA-256: `38a41d47ad006cd563793033f83c8f9ef6cdd3c8300695c31facfc71d368f8db`. RGB paths: 33/32/31. Axis calibration and alternative windows are recorded in the JSON; raw vertices in `colorsci/data/ektar_characteristic.csv`. Estimated engineering uncertainty about +/-0.03, not a confidence interval or batch guarantee. D illumination still uses the existing 5207 approximation, not Ektar spectral calibration.

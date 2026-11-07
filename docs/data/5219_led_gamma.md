# 5219 在 RGB Light Air 下的 midscale gamma 估计

研究日期：2026-11-07，Asia/Shanghai（UTC+8）。**这是有条件的光谱模型，不是这卷胶片/相机的实测标定。**

## 结论

在下述假设下，中性曝光的有效密度斜率（D / log10 H）为：

| | R | G | B |
|---|---:|---:|---:|
| LED 预测 gamma | 0.4533 | 0.5355 | 0.5266 |
| 相对 G | 0.8464 | 1 | 0.9833 |
| Kodak Status M 输入斜率 | 0.4482 | 0.5266 | 0.5096 |

可以把 Positive Out 的 `gam_r/g/b = 0.45 / 0.54 / 0.53` 作为**待验证的 500T + 此灯光起点**，不能称为精确设备参数，也不应因此直接替换所有胶片的全局默认值。此次只记录研究，未改模板/UI 默认值，未发布程序。

特别重要：该灯与 Status M 的斜率预测接近，**不能用“650 nm 令红密度明显偏低”解释 0206 的 R/G≈0.71**。此前单凭夜景全图回归断言 Gamma R 是偏青根因、承诺知道峰值即可算出精确实际 gamma，都超出了证据。

## 来源及原始数据

1. Kodak H-1-5219，March 2026 / Revised 3-26，第 4 页 Spectral Dye Density Curves，第 3 页 sensitometry：
   https://www.kodak.com/content/pdfs/motion/KODAK-VISION3-500T-5219-7219-technical-information.pdf
   PDF SHA256：`6d037d8090373ee50d10bedfff3df14777a575dd107c50edcafc7ec5862d0972`。
   Kodak 只发表曲线，不直接列出本报告的 gamma 数字。Status M gamma 是项目从直线区读图得到的，详见 `5219_digitization.md`，原估计误差 ±0.03。
2. ANSI/ISO 5-3:1995 / ANSI/NAPM IT2.18-1996，Table 4（印刷页 8）：
   https://law.resource.org/pub/us/cfr/ibr/001/aimm.it2.18.1996.pdf
   SHA256：`6357992b5a498402a23e9ca60c65e02deb58848fb7bb84f24e5f5011972d2b39`。
   公开的 Status M **log spectral products**，不是单独 detector QE。表峰值为 5.000；本模型取 `10^(table-5)`，不能再乘一次 A 光源。表外按表列斜率延伸 log product。
   2009 版预览也检索到，但预览不含完整 Table 4，因此这里明确使用 1995/1996 公开表，**未声称核对了 2009 的完整数表**：
   https://cdn.standards.iteh.ai/samples/52915/fed16a10b2884f89840a0daa8c480143/ISO-5-3-2009.pdf
3. 用户提供的 CuteNew RGB Light Air 测量报告参数：2026-10-14；峰值 650.1/539.9/450.7 nm；FWHM 22.2/38.7/18.6 nm。本次执行器收到的是这些转录参数；原图片附件路径未能在其文件系统打开，**未独立复核报告图片**。假设高斯 SPD，不把 dominant wavelength 当 peak。

数值转录：`colorsci/data/5219_spectral_dye_density.csv` 与 `status_m_log_products.csv`。只存本项目的数字转录，不再分发 Kodak 图像。

## 曲线读取方法

`pdfimages -png` 提取第 4 页 dye plot：686×730 像素（本次 `img-004.png`；PDF 内 xref 编号不作为稳定标识）。PNG SHA256：`78ffcdbe3d5ccb0b47947f0de5ba85d511af3a256ecd5ba85fad0863f7758465`。

像素坐标从左上角计：横轴 x=96 为 400 nm，x=662 为 800 nm；纵轴 y=589 为 D=0，y=306 为 D=1。换算：

```
lambda_nm = 400 + (x - 96) * 400 / 566
D = (589 - y) / 283
```

每 10 nm 人工辨认 Cyan/Magenta/Yellow、Midscale Neutral、虚线 Minimum Density 五条曲线；用竖向墨迹检查和叠加图纠正归属，在手读位置 ±4 px 内选邻近黑像素。端点、虚线空隙、交叉与文字覆盖处按邻线趋势手读/插值，不能把这些点看作仪器数据。CSV 同时保存 x、y 和转换后的密度，约 0.01–0.02 D 的工程读图误差（遮挡处可能更大），非统计置信区间。

染料曲线 peak-normalized、图示 D-mins subtracted。蓝波段 magenta 有小幅负值；保留而非截成零：这是该图给出的净密度变化形状，不能把它解释成孤立染料的负物理吸收。Minimum Density 单独保留。中性曲线按图注解释为扣 D-min 后的数据，积分基线为 `midscale_neutral + minimum_density`；不加 D-min 的敏感性结果亦报告。

复现转录（需先取得原图）：

```
python3 tools/digitize_5219_dyes.py /path/to/original-dye-plot.png --overlay /tmp/5219-overlay.png
```

叠加图已逐波长检查，未把它提交到仓库。

## 透射率积分及标定

设 `d_k(lambda)` 是 C/M/Y 三条归一化净密度曲线，`a_k` 是其幅度坐标。光源：

```
S_c(lambda) = exp[-4 ln(2) ((lambda-peak_c)/FWHM_c)^2]
D_c(a) = -log10[ integral S_c 10^(-D0 - sum a_k d_k) dlambda / integral S_c dlambda ]
J_ck = dD_c/da_k = integral S_c 10^(-Dmid) d_k dlambda / integral S_c 10^(-Dmid) dlambda
```

计算使用 400–800 nm、1 nm 间隔的等步长求和；密度线性插值，Status M **log product** 线性插值。不是先平均光密度；实际在透射率空间加权。400–800 nm 外的 Status M 入射权重占比，按表尾斜率估计 R=4.58e-7、G=4.49e-13、B=1.32e-7；LED 高斯尾亦可忽略。曲线之外没有虚构染料光谱。

中性曲线对 C/M/Y 的 NNLS 幅度拟合得到 `a0 ≈ (0.9014, 1.2429, 1.7040)`；拟合 RMS=0.0674 D，最大残差=0.1872 D。**它们不精确相加**，提示净染料/掩膜/图形读数模型有限。主计算直接使用已发表的中性密度谱加 D-min 作积分基线，等效于把上述残差视作局部固定背景；不假装三条染料完全重建了胶片。

从局部 Status M Jacobian 解 `q = inverse(J_M) gamma_M`：

```
J_M (行 R/G/B，列 C/M/Y，每单位归一化染料幅度的 D):
[ 0.85637   0.07119   0.00067 ]
[ 0.12544   0.84649   0.11630 ]
[ 0.03295   0.03678   0.88493 ]

q = da/dlog10(H) = [0.48334, 0.47655, 0.53807]

J_LED (同一行列顺序):
[ 0.86895   0.06980   0.00009 ]
[ 0.11471   0.86825   0.12328 ]
[ 0.02721  -0.01367   0.96636 ]

gamma_LED = J_LED q = [0.45330, 0.53554, 0.52660]
```

这里的 q 是中性曝光方向的**幅度变化率**，不是三组独立层曝光的完整化学响应。把 J_LED 作为任意影像的 3×3 反串扰矩阵直接套进 DCTL 不成立；它是局部 forward 染料→测量矩阵，现有 MaskCrosstalk 处理后也不能不经转换照用本报告的 gamma。

红通道约 `0.86895×0.48334=0.4200` D/logH 来自 C，另约 `0.06980×0.47655=0.0333` 来自 M。即使窄带，也不能忽略染料串扰。蓝的微小负 M 项来自保留的净密度曲线，不代表传感器负响应。

## 检验及不确定性

复现（numpy/scipy；可选 tifffile）：

```
python3 tools/estimate_5219_led_gamma.py --scan /workspace/uploads/0206.tiff --output docs/data/5219_led_gamma_result.json
```

脚本以中央差分检验解析 Jacobian（绝对误差 <1e-8），并验证 `J_M q` 恢复输入 gamma（<1e-12）。JSON 保存所有未过度四舍五入的结果。

模型变体 gamma R/G/B：

- NNLS 重建中性谱作基线：0.4537 / 0.5380 / 0.5272。
- 不加 D-min：0.4537 / 0.5349 / 0.5249。
- 半幅中性密度：0.4530 / 0.5340 / 0.5251。
- 双幅中性密度：0.4539 / 0.5394 / 0.5293。

1000 个固定种子敏感性情景：gamma_M 各自 ±0.03；染料曲线在 40 nm 节点各 ±0.02 D 平滑扰动；LED peak 各 ±2 nm、FWHM 各 ±10%。各项范围是**人为压力测试假设，不是测量报告的公差**。得到 gamma 的情景总范围：R 0.419–0.488，G 0.494–0.578，B 0.487–0.566；R/G 0.739–0.969。可把名义结果周围约 ±0.04 看作粗略工程敏感度，**不是实际扫描器的误差保证或置信区间**。

未量化系统误差可能更大：LED 非高斯副峰、相机 QE/光学滤镜在各带内非平坦、杂散光/黑电平/非线性、TIFF 中的相机矩阵或其它密度空间变换、ECN-2 实际工艺/卷批、曲线代表性、曝光相关掩膜/染料形状变化。此处假定扫描通道权重就是 SPD，未取得完整 QE 时不能把结果说成“实际相机 gamma”。也未计算逐层独立曝光响应；Kodak 的单条中性特性曲线不足以约束这一完整模型。

## 与 0206 的比较

输入是本机 `0206.tiff`，4170×6246×3、uint16，按用户给定的线性编码使用。以前报告的全图斜率 R/G=0.709、B/G=0.986，没有伴随完整 ROI/滤除条件。本次每 8 像素取样，以三通道都在 (0.0005, 0.98) 内为条件，令 `D=-log10(pixel/0.5)`，带截距 OLS：

- 全图 n=407681：DR 对 DG=0.6900，DB 对 DG=0.9782。
- 去掉四周约 10% n=261250：0.7035 / 0.9586。

因此没有逐字复现旧的 0.709/0.986；取样和 ROI 会改变答案。片基固定常数只改带截距回归的截距，不改斜率。

光谱预测 R/G=0.846 明显高于这些夜景回归；B/G=0.983 与旧值/本次全图接近。**红方向不支持此前的解释；蓝方向相近也不构成标定验证**。夜景不同像素有不同色彩与光源，OLS 斜率不是中性灰阶曝光导数；不能据此反推每通道胶片 gamma，也不能据此断言片基/灯/矩阵哪一个一定正确或错误。

建议：保持实测片基独立处理，用 0.45/0.54/0.53 做有出处的试验起点；下一步须扫描同卷、同工艺、同设备的多级中性曝光，检查密度线性、toe/shoulder、串扰和偏置。即使 gamma 合理，灰点/场景照明的色平衡仍是另一项标定，改 gamma 不保证消除偏青。

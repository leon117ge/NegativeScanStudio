# NegativeScan Studio OFX v0.90 . Windows x64 CPU preview

## 安装 / Installation

1. 退出 DaVinci Resolve。解压 ZIP，把 **整个** `NegativeScanStudio.ofx.bundle` 文件夹复制到：
   `C:\Program Files\Common Files\OFX\Plugins\`（可能需要管理员权限）。
2. 重启 Resolve，在 OpenFX → NegativeScan Studio 中找 **NegativeScan Studio**。
3. 这是 CPU float RGBA 版本，无需额外 libstdc++/libgcc DLL。尚未在真实 Resolve 中验收；建议先在工程副本测试。

Quit Resolve, unzip, copy the entire bundle to `C:\Program Files\Common Files\OFX\Plugins\`, restart Resolve, then add **NegativeScan Studio** from OpenFX. Windows x64 only, CPU float RGBA; no extra C++ runtime DLLs. Real Resolve validation is still pending.

## 输入与节点 / Input and processing

一体链：**线性相机矩阵 → Density In → 密度域 Mask/Crosstalk → Positive Out**。可单独输出 Density/Crosstalk 检查；勾选 Output transmittance 时直接输出透射率，不把它误送给密度域后级。

- 输入应是 NSS 的未转正线性 TIFF。避免 Resolve 在插件前自动套入不匹配的输入色彩转换；DI timeline 输入必须明确勾选 Density In 的 Input is DaVinci Intermediate。此选项只解码 DI 传递函数，**不转换色域**。
- 保留原 DCTL 参数/默认值：片基 `.1933/.1883/.0593`、gamma `.55/.635/.62`、legacy Film WB=1、Cineon 输出、mask off、crosstalk strength=0。相机校准默认为开启的单位矩阵，与导出的 CameraCal DCTL 一致。
- Positive Out 的 Cineon/DI/ACEScct/Linear 是原模板的编码公式，不是完整的色域变换或打印模拟；Rec709 是原模板的传递函数。没有把 2383 LUT 合入本版本。
- Density/Crosstalk 各有自己的 code/step/full/clamp 参数，默认相容；若手动改了，必须保持两段一致。Positive Out 保留模板硬编码 95/0.002/1023；没有偷偷“修正”原公式。
- 用户保存的片基/灰点数据库未自动导入 OFX；可填入 base sliders、gray code sliders。六套固定 Crosstalk 矩阵与 DCTL 相同；光谱研究中的 forward Jacobian **不是**这里的反串扰预设。

Use unconverted linear negatives. Match Resolve input transforms to the plugin, and do not apply the old DCTL chain again. Select output encoding deliberately; these are the original transfer functions, not gamut conversions. All DCTL defaults remain unchanged. Saved per-user base/gray databases are entered manually, not embedded in the binary.

## 片种 gamma / Film stock gamma presets

`Film stock gamma preset`：Legacy/manual、500T Status M、500T LED estimate、250D、UltraMax 400、Portra 400、Gold 200。选择后填入 Gamma R/G/B，仍可手改。数据/方法/误差见 [`docs/data/ofx_stock_gamma.md`](../docs/data/ofx_stock_gamma.md)。

- 500T 自动选 T，其余片种自动选 D；Film balance 仍可覆盖。
- 场景温度：3200/3800/4400/5000/5600K。切换 T/D 或温度会填入 Density In 的 signed additive `cast_r/g/b` 并启用 cast；手动精调直接改这三个值，按钮可恢复预设。
- 选择新 WB/片种时关闭旧 Positive Out legacy Film WB，避免叠加两套色偏校正；灰点开关仍由用户控制。默认未选择新预设时，legacy 行为不变。
- T=5219/3200K，D=代表性 5207/5600K；Portra/Gold/UltraMax 的 D 色温偏移暂共用 5207 模型，不是假称各自实测。全部用 Planck 近似，非 CIE D 光源，非 LED 场景校准。灯光调平后不改灯。

Stock selection fills gamma and suggests T/D, which remains overridable. Temperature/type changes fill signed density offsets; manual tuning remains. Applying new WB disables the legacy tungsten Positive Out balance to avoid stacking. The D illuminant model uses representative 250D sensitivity for all daylight stocks. All are nominal models, not roll/scanner calibration.

## TIFF 元数据 / Metadata auto-read

NSS v0.90 的 HDR/三色扫描 TIFF 在标准 **ImageDescription/tag 270** 内写 `nss.scan/1` JSON：实际扫描灯 RGB drive（不是片基！）、成品线性 TIFF 片基、相机矩阵/强度/是否已烘焙、灯型号、卷名、版本、时间、处理状态。未知值不编造、不写假零。裁切工具原有 tag270 保留；去毛发/去灰尘写回也保留。

1. **Resolve 20 source auto-read** 默认启用：探测宿主扩展 `OfxImageEffectPropSrcFilePath`，取得路径后读取 TIFF，按文件路径与修改时间缓存。元数据实际作用在每次 render 的参数副本上，不在 render 中写宿主参数。**所以 UI sliders 不会随自动模式实时跳动。**
2. 标准 OFX 不保证给源路径；Resolve 20 扩展存在性的公开来源见 [ASWF #142](https://github.com/AcademySoftwareFoundation/openfx/issues/142)。不同版本、复合片段、代理、序列、Fusion/Color 节点位置可能不暴露正确路径，真实 Resolve 内仍待验收。本版不解析序列 `%04d` 路径，也不追踪其它中间节点的像素变换。仅在插件前没有改变扫描线性 RGB 的路径中使用自动片基。
3. 通用/可靠方法：选择 `Scan TIFF or sidecar JSON` → **Load from file**；会填入 base、pending camera matrix 和强度、显示 light drive RGB，并自动关闭 Auto，成为可手改的快照。没填路径时该按钮尝试宿主源路径。
4. 要手动覆盖，关闭 Auto 或先 Load；Auto 开启且元数据有效时，自动片基/相机矩阵优先。温度/cast/positive 参数不被 TIFF 覆盖。PWM 灯值只显示为 provenance，**绝不当成归一化片基值**。
5. 老 TIFF 没有元数据或宿主不暴露源路径时，自动模式保留手动值。显式选择的坏文件会报错而非悄悄套错参数；Load 失败不应当作加载成功。带 baked colour processing 的 TIFF 拒绝自动套用，以免重复转换。Classic TIFF LE/BE 支持；BigTIFF 请导出 JSON。
6. 导出 sidecar（有 Python 时，无第三方依赖）：`python export_metadata.py scan.tiff`，生成 `scan.tiff.nss.json`，不覆盖已存在文件。该文件可跨 Resolve 使用；也可用 ExifTool 读取 ImageDescription。

Resolve 20's optional source-path extension is probed, never assumed. When available, valid metadata overrides a render-local base/camera snapshot; UI values stay unchanged. For portable use, choose a TIFF/JSON and click **Load from file**: controls are populated and Auto is switched off so manual edits persist. Old scans retain manual values. Explicit invalid/processed metadata is rejected. Pending matrices also transform the imported base; already-baked matrices are not applied again. No firmware/light changes are made.

## Build / test

```
./ofx/build.sh /absolute/output/directory
CGO_LDFLAGS=... NATIVE_CXX=c++ ./ofx/test.sh /absolute/test/directory /path/to/0206.tiff
```

Requires MinGW-w64 g++, Python 3, native C++17 compiler, Go + NSS LibRaw development dependencies; parity check uses numpy/tifffile. Official OpenFX headers are vendored with upstream commit in `vendor/openfx/REVISION`; BSD license retained. JSON parser: nlohmann/json v3.12.0 (MIT). `tools/generate.py` mechanically ports committed DCTL templates, controls/defaults and regenerates WB tables. Gamma digitizer is `tools/digitize_still_gamma.py` (requires pymupdf/numpy and official PDFs, not redistributed).

See [`docs/OFX.md`](../docs/OFX.md) for schema, scientific limits and verification. No CUDA/OpenCL/Metal path in v0.90.

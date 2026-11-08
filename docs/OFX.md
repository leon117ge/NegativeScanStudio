# NegativeScan Studio OFX 0.90.6

This independently versioned plugin patch uses tag `ofx-v0.90.6`. App v0.91 has its own executable and tag. The metadata patch passes native CPU and mock-host tests. Real Windows Resolve and physical GPU acceptance remain outstanding. [Installation](../ofx/README.md).

## 0.90.6 Ektar 100

Ektar 100 appends a stock option without changing previous indices. It writes visible gamma R/G/B 0.580572786/0.579564388/0.663740443, defaults T/D to D, and keeps manual overrides. Values are interpolated OLS slopes from Kodak E-4046, January 2025, page 4 daylight Status M vector paths, using the existing logH -1.5..0 method. [Source, axes, PDF hash, residuals and alternate windows](data/ofx_stock_gamma_readings.json).

No uncalibrated Base placeholders exist, so no Ektar Base is invented. Existing Base, scale 1.15, Exposure 0.90 and all rendering kernels remain unchanged. Scanning app stays v0.91. Full CPU/mock-host suite and offline CUDA assembly checks are run; real Windows Resolve/GPU/overlay and actual Ektar scans need acceptance testing.

## 0.90.5 after-profile metadata

The app writes its last accepted Light-page leveling base, not a new image
measurement. The plugin recognizes the explicit coordinate marker and loads the
reference without applying the camera matrix twice. Older unmarked metadata is
unchanged. Rendering kernels and film presets are unchanged.

## 0.90.4：在画面直接点选灰白点

新增OFX OverlayInteractV1（OpenGL1.x系统动态加载），首个Viewer gray / white pick组请求默认展开。**先选NSS节点/效果 → viewer左下角选择Open FX Overlay / FX → 勾Pick gray on viewer → 点击/拖动中性灰白区域**。Shift + ~ (tilde)可切换overlay显隐；Window/Transform/Wipe模式不分发本插件点击。

十字保持屏幕像素大小，采样框按render scale/PAR换算。pen与pick_point均canonical XYAbsolute，不重复乘viewer zoom/render scale。首次点击启用Auto gray、可见Base/balances置None/0；Auto已启用时后续点击保留手动balance。选好可取消Pick gray on viewer，Auto仍在固定点逐帧工作；关闭Auto才回到手动gray codes。没有Bake/自动追踪。

宿主OpenGL上下文仅用于绘制UI，不读framebuffer、不改变CPU/CUDA/OpenCL图像算法；push/pop恢复GL状态，不修改宿主矩阵/投影，不swap buffers。SlaveToParam触发参数变更重绘，click/drag成一组undo，PenUp/LoseFocus/异常结束编辑。保留原生2D handle/手填canonical X/Y作为fallback。真实Resolve的绘制、鼠标事件分发、HiDPI/代理/非方像素仍待验收。所有原有数值默认和app EXE均未变。

参考：[OFX interact API](https://openfx.readthedocs.io/en/latest/Reference/api/file/ofxInteract_8h.html)、[Resolve onscreen controls手册章节（第三方镜像）](https://docs.imaster.org/davinci-resolve-21-manual/part-11-color-page-effects/ch.-151-using-open-fx-and-resolve-fx)。Resolve手册确认OpenFX onscreen controls/FX模式；这不代替本插件真机测试。

## 0.90.3：仅更新500T Base

用户眼调指定 **R +1.07 / G 0 / B +0.67 stops**（上版+1.40/0/+0.80），同时作为loader和新实例可见balance默认值。不是新测量。已有工程由宿主保存的slider值不自动覆写；重选500T Base可加载新值。Gamma scale1.15、Exposure0.90、stock gamma、T5600表、GPU/CPU算法和扫描app均未变。

## 0.90.2：默认观感与实际 GPU 路径

- 胶片 gamma **预设数值保持0.90.1原值**，没有统一×1.25。Positive Out 的 Gamma R/G/B 后紧接 **Gamma scale**，默认 **1.15**，范围.25–2。有效 gamma = 可见 R/G/B ×可见 scale，保持通道比例。Exposure 默认 **0.90 stops**（原1.00）。这两个新默认是用户选择的扫描观感/对比调整，不是 Kodak 实测 gamma 的更正。
- 因此0.90.2新实例默认输出有意不同于0.90.1。纯公式/DCTL对照需 scale=1、Exposure=1，或把对应有效 gamma/exposure显式送入参考。旧项目请备份；若宿主恢复旧Exposure1但新增scale取1.15，观感仍会变化。设scale1可回到旧gamma。
- 例如500T LED预设仍显示 .45/.54/.53，scale1.15时有效值 .5175/.621/.6095。旧500T Base实测于scale1。0.90.2当时Base按用户在新默认下眼调，从1.57/0/1.00改为1.40/0/0.80；已取消重测，不把眼调值视为通用stock校准。
- Windows NVIDIA/Resolve若采用CUDA，只有OpenCL的插件可能退回CPU，带来整帧GPU↔CPU搬运。这是本次排查的**可验证假设，不是已测出的用户机根因**。本版新增CUDA Driver API/PTX路径，让宿主可直接提供CUDA device pointers和stream。
- 播放/改变画面后点 **Show render engine**：应看到实际 `CUDA PTX (host stream)`、`OpenCL buffers (host queue)` 或 `CPU N threads`，并列出驱动/runtime可用性。仅“有CUDA驱动”不代表Resolve已协商CUDA；仍显示CPU时应先查宿主GPU模式、驱动、旧bundle/插件缓存，而不是据此承诺FPS。

## 处理链与交互

线性 camera calibration → Density In → Positive Out。按反馈移除整个 mask/crosstalk 参数组和计算阶段；camera calibration 保留。原独立 DCTL/Go Develop 不改。新 OFX Film base 默认 **0.5/0.5/0.5**；Positive gamma sliders 默认 .55/.635/.62，Gamma scale默认1.15，500T Base 默认加载到可见balance（1.07/0/0.67）、曝光 +0.90、Cineon 输出不变。旧项目请先备份：被移除的 stage 不再生效，不承诺与旧项目完全同观感。

Film base and balance 合并片种 gamma、T/D、3200/3800/4400/5000/5600K、片基sliders；Gamma R/G/B和紧随Gamma B的Gamma scale位于Positive Out。选择 stock 立即填 gamma 并建议 T/D；选择类型/色温立即填片基，无 Apply/cast 字段。手动微调保留。科学模型和完整片基表见 [WB](data/ofx_wb.md)，gamma 来源/不确定度见 [stock gamma](data/ofx_stock_gamma.md)。

`base = baseline × 10^(+offset)`：正号与 Density_In `−log10(pixel/base)` 一致。新实例 baseline .5；Load metadata 后用导入的 camera-corrected base 作 baseline。baseline 单独持久保存，切换预设或反复 Load 不累乘；手动 base 微调在下一次选预设时重置。自动 source metadata 模式在 render 副本套同一补偿，不写 UI。手改 base / 选预设 / Load 将退出 Auto，以允许手动覆盖。

Positive Out 保留 **Film white balance (None / 500T Base)**，是 loader，不是额外隐藏gain。500T Base 当前用户眼调R+1.07/G0/B+0.67 stops，立即写 Red/Green/Blue balance；None 将三条复位0。旧 white_code 0.0013 slope 已删除。T/D/stock选择只更新各自base/gamma，不清空手动balance，两者可显式叠加。每stock将来只有一个实测Base，其他片种未测不编数。[表、条件、测量记录](data/ofx_film_bases.md)。

## Viewer 灰/白点

1. 按上节选择Open FX Overlay，勾Pick gray on viewer，在画面点击/拖动实际中性灰白参考。也可手动开启Auto gray from point，再用原生2D handle或手填canonical X/Y。不能用任意有色物体。默认点仍由normalized .5/.5映射到画面中心。
2. `Sample radius (pixels)` 0–32，在 **当前 render 分辨率**上取方形 patch；图边裁切，跳过透明/非有限样本。请求 Source ROI 包含 patch，支持 nonzero bounds、render scale、PAR、premult。
3. 每帧从 SOURCE 读样本，解 DI、应用当前 camera 与 Density In，平均得到进入 Positive Out 的密度 code（×1023）。只在 render 副本启用 `gray_on`/`gray_r/g/b`，沿用现有灰点公式使 R/B 相对 G 中性；不改 Film base，也不在render写动画 key、不改变曝光目标；之后仍可手动微调balance。
4. CPU 直接读小 patch；OpenCL 用一次 `clEnqueueReadBufferRect` 阻塞下载小 patch，再用同样的密度算法。**仅开启 picker 时**产生这次小 readback/同步，默认关闭。不读整张 GPU 图，也不把 cl_mem 当 CPU pointer。
5. 固定画面坐标、逐帧采样，**不是跟踪器**；移动主体、饱和白、纯黑/透明区、上游节点改变像素都会影响结果。点不在输入 bounds 或无有效像素时保留手动灰点；Show render engine 显示 `pick unavailable`。没有bake按钮；已实现自绘GL overlay并保留原生point fallback，Resolve实际viewer呈现/输入仍待验收。

## 数据契约

TIFF IFD0，tag270（ImageDescription），ASCII、NUL 结尾 JSON；中文/非 BMP 字符使用 JSON Unicode escapes，不依赖私有 tag。保留 Make/Model/Software 等原 tag、LZW、原像素。

```json
{
  "schema": "nss.scan/1",
  "software": "NegativeScan Studio v0.90 trichrome ...",
  "created": "2026-11-07T14:00:00+08:00",
  "roll": "example",
  "mode": "trichrome",
  "light_model": "CuteNewDesign RGB Light Air",
  "light_rgb": [1200, 2300, 3400],
  "film_base": [0.49844, 0.49639, 0.50210],
  "film_base_space": "after_profile",
  "base_source": "light leveling (after profile)",
  "base_source": "measured RGB TIFF clear base",
  "encoding": "linear",
  "camera_matrix": [1,0,0,0,1,0,0,0,1],
  "camera_strength": 1,
  "camera_applied": false,
  "camera_profile": "example profile"
}
```

The example shows app v0.91 coordinates, not a universal film base. `light_rgb`
is lamp drive, never the reference. `film_base` is the accepted Light-page
leveling result after profile, unchanged by capture or merge. The app's default-on
capture option displays that result and timestamp. Off or no matching accepted
result omits the base instead of estimating it from image content. Light state
and scan presets persist the result.

OFX 0.90.5 recognizes `film_base_space: after_profile` and does not multiply
that base by the pending camera matrix again. TIFF pixels still receive their
pending matrix. Older unmarked files retain their original pre-profile behavior.
Visible base sliders equal the reference times `10^offset` for the current T/D
selection. T at 3200 K or D at 5600 K displays the exact loaded reference. Files with
`encoding=processed` or nonempty `processing` remain ineligible for automatic
linear conversion. Existing v0.90 TIFFs are not automatically repaired.

`created` 是写入时间（Asia/Shanghai UTC+8），不是胶片拍摄时间。当前无 EXIF 原始拍摄日期恢复，也没有宣称根据 Kelvin 可以推导设备 RGB PWM。扫描 TIFF 与照片场景色温是两个独立概念。

元数据装在原 HDR/三色写出路径中；裁切复制原 tag270；hair/dust 写回保留。离线 Merge 不复制当前灯状态到旧图；三色单通道中间文件标记 `encoding=trichrome-component`，不会当作成品RGB自动套用。单独 RAW Convert/开发预览导出没有扫描时的灯状态，**不冒充有卷扫描元数据**。老文件不被追溯修改。sidecar 按需用随 ZIP 脚本导出，不强制每次扫描多写一份文件。

## 文件加载与根因

实际检查用户的 `/workspace/uploads/0206.tiff`：4170×6246、约156MB、IFD 位于像素之后，**没有 tag270**（只有像素/Software 等 tag）。因此旧版的 “No NSS metadata…” 对这张图是正确的“旧扫描没有信息”，不是已经证实的 Windows 路径故障。不能从不存在的元数据恢复未知 RGB；旧文件需手填或提供含实测值的 sidecar。

本次加固：路径立即从 OFX string 复制；去外层引号/空白，支持 file:// percent UTF-8；Windows `_wfopen` + UTF-8→UTF-16，`_fseeki64`；classic TIFF / BigTIFF、LE/BE、64-bit offset。只 seek/read header、IFD 和 ≤1MiB description，**不读像素**。JSON BOM 亦支持；截断/数组/encoding/schema/matrix 校验；缺少信息负缓存，避免每帧重复读旧图。相同路径mtime最多每250ms检查一次，元数据只有路径/mtime变化才重读；显式Load强制立即重读。按钮捕获异常，不让异常跨 C ABI。窄 UI 短状态：`Loaded: 0.50/0.50/0.50` / `No NSS info (old scan)` / `File not found`。

Load 写入 base/camera、建立未作场景 WB 的 baseline，再套当前预设一次，关闭 Auto。失败不改变原来的 base。pending matrix 同时变换影像和导入片基；baked matrix 不重复应用。device `light_rgb` 只是 provenance/PWM，绝不当作归一化片基。

## Resolve 自动源路径边界

标准 OFX 不保证原始源路径。探测公开记录的 Resolve 20 `OfxImageEffectPropSrcFilePath`，依次检查 render args、输入 image、effect、source clip；缺失就用明确文件路径 + Load。公开依据：[ASWF #142](https://github.com/AcademySoftwareFoundation/openfx/issues/142)。未安装私有 Resolve SDK，不能保证各版本都暴露该属性。

Auto 有效时 metadata 只覆盖 render 副本，UI 不跳动；没有/损坏 metadata 回到手动，Load 可显示原因。代理、复合片段、序列 `%04d`、不同文件、上游非线性/色域变换可能使路径与像素不匹配。只有未被改写的同一线性 NSS 扫描可用，不是自动色彩管理。更改 camera/neutralise 后应重新确定片基。sidecar 导出脚本当前限 classic TIFF，插件自身支持 BigTIFF。

## CUDA / OpenCL / CPU

### CUDA：Windows NVIDIA实验路径

- 系统目录动态加载 **nvcuda.dll**，只用CUDA Driver API。无需在用户机安装nvcc/NVRTC/toolkit，不捆绑NVIDIA DLL或专有SDK headers。
- 声明OFX CudaRenderSupported/CudaStreamSupported，按CudaEnabled读取真实device pointers，按CudaStream使用宿主stream。用source allocation/stream恢复宿主context，作用域结束恢复线程原context；不创建自己的GPU context，不复制全帧到CPU。
- 每实例/host context只JIT一次PTX并缓存module/function。每launch把40个float常量作为160-byte参数块传入，不创建/上传device常量buffer，不共享可变kernel参数。
- 提供stream时只enqueue，不finish、不deviceSynchronize。旧宿主不提供stream属性时按OFX契约等待default stream；仅开启灰点时需小patch pinned-host download和该stream的等待。
- PTX由官方 **NVRTC12.0.140** 在Linux离线生成，目标compute_52/PTX8.0，精确除法、禁FMA/FTZ/fast-math。ptxas已对sm_52成功组装，16寄存器、无stack/spill。需CUDA12兼容的NVIDIA驱动与sm_52或更新设备；实际Windows Driver JIT仍待验收。
- 测试覆盖mock Driver ABI、opaque device addresses、context切换/恢复、host/default stream、module cache、native编译CUDA源数学和可选灰点。**这不是PTX GPU执行/真实CUDA math parity**。无物理NVIDIA GPU，不能声称已测实际CUDA性能。

### OpenCL与CPU

- 系统目录动态加载OpenCL.dll，采用宿主queue/cl_mem buffer，不支持OpenCL image。program和kernel按context/device缓存，设置args/enqueue以短mutex保护；切换两个真实PoCL context不会重复编译。不是每帧/每tile编译。
- CPU精确math和最多8线程保持；逐实例缓存完全相同Pipeline的Prepared常量，参数/metadata/灰点结果变化才重建，不按时间猜测动画值。render仍读取当前time的参数，避免陈旧动画缓存。
- 开启SupportsTiles/MultiResolution，关闭HostFrameThreading，避免宿主切片线程与内部CPU线程叠乘。clipGetImage明确请求render window对应canonical ROI；灰点关闭不扩展至全帧，开启才并入patch。宿主可返回更大的buffer，插件不假定紧密内存布局。
- GPU失败返回OFX GPU render failure (1002)，允许宿主重试CPU；不把CUDA/OpenCL pointer当CPU指针。可手动Render engine→CPU。自动回退取决于宿主实现。
- Metal与当前Windows包无关，没有Metal实现。CUDA/OpenCL都需要宿主真实协商，不能只看advertised flag判断是否在GPU。

## 验证与性能

- Go v0.90 writer fixture 真实写 TIFF，当前插件导入；classic/BigTIFF LE/BE、稀疏 >4GiB metadata offset、Unicode/quoted/file URL/BOM、实际旧156MB TIFF、坏 metadata 均测试。
- 最小 OFX suite host 调用真实 Load/Describe/Create/Render/Changed/ROI/Destroy：负 row stride、非零 origin、alpha/premult、自动 metadata、立即 stock/gamma/base、反复 Load/切换不累乘、WB 正号、picker 中性化及越界 fallback。原生/custom viewer overlay外观未测。
- Overlay host测试调用descriptor里的真实callback：Describe/Create/Draw/PenDown/Motion/Up/LoseFocus/Destroy，验证arm、canonical点位、首击Auto/可见balance复位、后续微调保留、RoD边界、GL命令几何/状态保护和render-scale/PAR。GL用记录mock，不是真实OpenGL/Resolve。
- CPU 对独立 Go DCTL port：100组合×360像素×RGB =108000；旧Go隐藏WB关闭，用相同可见manual gains等效比较。最大绝对差 **9.16e-5**（HDR），实际 scan patch 最大 **6.10e-5**，scaled max **1.50e-6**。通过 `1e-5*max(1,abs(reference))`，不声称 HDR 绝对差均≤1e-5。[结果](data/ofx_0903_parity_result.json)。
- 真实 OpenCL Render：164组合 +4个二维 picker/prepass 组合（0.90.2另验证双context program/kernel缓存），193536通道比较，最大绝对差 **2.10e-5**、scaled **9.93e-7**（相对 CPU）。[结果](data/ofx_0903_opencl_result.json)。不同 GPU 驱动仍需验收。
- Windows x64 MinGW 静态 C++/gcc DLL 导出2个 OFX 入口，仅系统 KERNEL32/msvcrt imports；OpenCL.dll 是运行时加载，不是硬依赖。

## 0.90.2实际Render CPU对照（不是CUDA/Resolve FPS）

结论：**没有额外CPU加速**。本轮新版大帧median慢约2.8%（五组范围重叠），16×16微tile单次慢约1.9%（约0.74µs），不将缓存改动包装成CPU吞吐提升。主要改进目标是让NVIDIA宿主可协商CUDA、避免CPU回退及不必要全图请求，真实收益必须在用户机确认。

同一minimal OFX host实际调用旧tag `ofx-v0.90.1` 与0.90.2 Render，不是只测一个数学函数。双方有效参数匹配0.90.2默认：raw gamma .55/.635/.62×1.15、Exposure.90、balance1.40/0/.80、相同pending camera/base metadata、Cineon、picker off。旧版用手调gamma/balance匹配，不把观感变化算作优化。

Linux8-vCPU Xeon/KVM，同-O3/no-math-errno/no-trapping-math。每process预热一次，五组old/new交替，实际0206全帧6246×4170，计时包含参数、metadata、image fetch、prep及内部线程启动；不含TIFF decode/process启动/真实宿主调度与GPU↔CPU搬运。

| CPU Render | 0.90.1 median / range ms | 0.90.2 median / range ms |
|---|---:|---:|
| 全帧 | 405.332535 / 402.708040–419.419989 | 416.594263 / 404.228400–470.040237 |
| 16×16 tile（2000次/组，单次） | 0.039079 / 0.038704–0.039299 | 0.039818 / 0.039739–0.040203 |

全输出checksum匹配、所有输出finite、固定grid参考误差0、失败0。小tile组中新版Prepared只建1次、metadata只解析1次、mtime probe1次；source全图请求从2000次变为0（改为明确ROI请求，真实宿主如何分配仍待测）。大帧CPU算术仍占主导：独立非对照profile八帧wall4.376s/user28.553s/system.584s；不能把250ms metadata节流或一次性JIT缓存宣称为成倍CPU像素计算加速。

复现：`ofx/tests/benchmark_host.sh`；[原始五组/校验/计数](data/ofx_0902_host_benchmark.json)。[CUDA离线构建/组装记录](data/ofx_0902_cuda_build.json)。无物理NVIDIA GPU，CUDA Driver JIT、PTX执行parity和GPU性能均未测。

### 历史0.90→0.90.1 CPU benchmark（不是0.90.2额外加速）

0206 全帧 6246×4170 =26,045,820 像素，内存 float RGBA。Linux box 8 vCPU，Intel Xeon/KVM；相同编译器与 `-O3 -fno-math-errno -fno-trapping-math`。对照提交 `fc8dd26` 单线程（旧版发货O2，但这里双方统一O3）；相同 .5 base/原gamma曝光Cineon、identity camera、旧cross关闭；双方使用相同实测Base（旧参考以None+manual R1.57/B1.00等效）。每路径预热1次，5组交替测，包含 preparation/线程启动，消费 checksum并逐值比较。

| path | median ms | min–max ms |
|---|---:|---:|
| 原 v0.90 单线程 | 3779.940 | 3757.330–3808.072 |
| 0.90.1 prepared 单线程 | 1796.088 | 1780.715–1822.019 |
| 0.90.1 prepared 8线程 | 331.550 | 283.902–336.799 |

该测试范围 **11.40×**，default全帧输出 bit-exact（max error0）；优化前后不是改低分辨率/缓存结果。单线程2.10×，再8线程5.42×，支持“重复逐像素计算/单核算术限制”的解释，不是 TIFF decode/I/O 加速。没有把 optional picker readback 纳入默认关闭基准。

范围只含 CPU 内存 render；不含 decode、metadata I/O、viewer/host调度、GPU传输/执行。**不等于 Windows/Resolve 端到端加速11.40×**；真实时间待用户验收。[原始5组、误差和checksum](data/ofx_0901_cpu_benchmark.json)，复现 `ofx/tests/benchmark.sh`。

科学限制：T=5219/3200K，D=5207/5600K（其他 daylight stocks 暂共用5207）；Planck 非实测 LED/CIE D。Status M 曲线 slope 与 scanner RGB gamma 不等价，见 [5219 LED](data/5219_led_gamma.md)。不改扫描灯/相机 WB/已取消400K stash。

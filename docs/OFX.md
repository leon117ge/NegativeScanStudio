# NegativeScan Studio OFX 0.90.1

这是 **插件独立版本**（tag `ofx-v0.90.1`）；NSS Go app 仍为 v0.90，exe 未重建/改动。v0.90 已由用户在 Windows Resolve 加载试用；**0.90.1 新 OpenCL 路径、viewer 控件尚未在真实 Resolve/物理 GPU 验收**。安装与双语说明：[ofx/README](../ofx/README.md)。

## 处理链与交互

线性 camera calibration → Density In → Positive Out。按反馈移除整个 mask/crosstalk 参数组和计算阶段；camera calibration 保留。原独立 DCTL/Go Develop 不改。新 OFX Film base 默认 **0.5/0.5/0.5**；Positive gamma 默认 .55/.635/.62、500T Base 默认加载到可见balance（1.57/0/1.00）、曝光 +1、Cineon 输出不变。旧项目请先备份：被移除的 stage 不再生效，不承诺与旧项目完全同观感。

Film base and balance 合并片种 gamma、T/D、3200/3800/4400/5000/5600K、片基与 gamma sliders。选择 stock 立即填 gamma 并建议 T/D；选择类型/色温立即填片基，无 Apply/cast 字段。手动微调保留。科学模型和完整片基表见 [WB](data/ofx_wb.md)，gamma 来源/不确定度见 [stock gamma](data/ofx_stock_gamma.md)。

`base = baseline × 10^(+offset)`：正号与 Density_In `−log10(pixel/base)` 一致。新实例 baseline .5；Load metadata 后用导入的 camera-corrected base 作 baseline。baseline 单独持久保存，切换预设或反复 Load 不累乘；手动 base 微调在下一次选预设时重置。自动 source metadata 模式在 render 副本套同一补偿，不写 UI。手改 base / 选预设 / Load 将退出 Auto，以允许手动覆盖。

Positive Out 保留 **Film white balance (None / 500T Base)**，是 loader，不是额外隐藏gain。500T Base 实测R+1.57/G0/B+1.00 stops，立即写 Red/Green/Blue balance；None 将三条复位0。旧 white_code 0.0013 slope 已删除。T/D/stock选择只更新各自base/gamma，不清空手动balance，两者可显式叠加。每stock将来只有一个实测Base，其他片种未测不编数。[表、条件、测量记录](data/ofx_film_bases.md)。

## Viewer 灰/白点

1. 打开 `Auto gray from point`（可见balance复位0、Base下拉置None，开始新的中性校正），在 viewer 的原生 2D point handle 拖到实际中性灰/白参考；不能用任意有色物体。若宿主不显示 handle，可手填 canonical X/Y。默认点以 normalized .5/.5 声明，由宿主映射到画面中心。
2. `Sample radius (pixels)` 0–32，在 **当前 render 分辨率**上取方形 patch；图边裁切，跳过透明/非有限样本。请求 Source ROI 包含 patch，支持 nonzero bounds、render scale、PAR、premult。
3. 每帧从 SOURCE 读样本，解 DI、应用当前 camera 与 Density In，平均得到进入 Positive Out 的密度 code（×1023）。只在 render 副本启用 `gray_on`/`gray_r/g/b`，沿用现有灰点公式使 R/B 相对 G 中性；不改 Film base，也不在render写动画 key、不改变曝光目标；之后仍可手动微调balance。
4. CPU 直接读小 patch；OpenCL 用一次 `clEnqueueReadBufferRect` 阻塞下载小 patch，再用同样的密度算法。**仅开启 picker 时**产生这次小 readback/同步，默认关闭。不读整张 GPU 图，也不把 cl_mem 当 CPU pointer。
5. 固定画面坐标、逐帧采样，**不是跟踪器**；移动主体、饱和白、纯黑/透明区、上游节点改变像素都会影响结果。点不在输入 bounds 或无有效像素时保留手动灰点；Show render engine 显示 `pick unavailable`。没有 bake 按钮/自绘 GL overlay，依赖宿主原生 point handle；Resolve 的实际 viewer 呈现仍待验收。

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
  "film_base": [0.48, 0.50, 0.46],
  "base_source": "measured RGB TIFF clear base",
  "encoding": "linear",
  "camera_matrix": [1,0,0,0,1,0,0,0,1],
  "camera_strength": 1,
  "camera_applied": false,
  "camera_profile": "example profile"
}
```

上面是格式示例，不是用户实际扫描值。`light_rgb` 是原设备 drive 数；`film_base` 才是线性 TIFF/65535。未知 light/base/matrix 省略；自动处理必须有有效 `film_base`。快照在扫描开始时从 GUI 取一次，三色用 ChannelSwitch 实际传入的 R/G/B；最终 TIFF 写入时用该图实际测到的片基覆盖。白光使用锁定时已知灯值，无设备/无锁定记录则不声称已知。三色 profile 为待应用相机矩阵，NSS 并未把它烘焙到 TIFF；片基仍是矩阵前，导入时对影像和片基应用同一矩阵。原 HDR colour processing 若已写入则记 `encoding=processed` 和 `processing` 描述，不让 OFX 再自动转换。

`created` 是写入时间（Asia/Shanghai UTC+8），不是胶片拍摄时间。当前无 EXIF 原始拍摄日期恢复，也没有宣称根据 Kelvin 可以推导设备 RGB PWM。扫描 TIFF 与照片场景色温是两个独立概念。

元数据装在原 HDR/三色写出路径中；裁切复制原 tag270；hair/dust 写回保留。离线 Merge 不复制当前灯状态到旧图；三色单通道中间文件标记 `encoding=trichrome-component`，不会当作成品RGB自动套用。单独 RAW Convert/开发预览导出没有扫描时的灯状态，**不冒充有卷扫描元数据**。老文件不被追溯修改。sidecar 按需用随 ZIP 脚本导出，不强制每次扫描多写一份文件。

## 文件加载与根因

实际检查用户的 `/workspace/uploads/0206.tiff`：4170×6246、约156MB、IFD 位于像素之后，**没有 tag270**（只有像素/Software 等 tag）。因此旧版的 “No NSS metadata…” 对这张图是正确的“旧扫描没有信息”，不是已经证实的 Windows 路径故障。不能从不存在的元数据恢复未知 RGB；旧文件需手填或提供含实测值的 sidecar。

本次加固：路径立即从 OFX string 复制；去外层引号/空白，支持 file:// percent UTF-8；Windows `_wfopen` + UTF-8→UTF-16，`_fseeki64`；classic TIFF / BigTIFF、LE/BE、64-bit offset。只 seek/read header、IFD 和 ≤1MiB description，**不读像素**。JSON BOM 亦支持；截断/数组/encoding/schema/matrix 校验；缺少信息负缓存，避免每帧重复读旧图。按钮捕获异常，不让异常跨 C ABI。窄 UI 短状态：`Loaded: 0.50/0.50/0.50` / `No NSS info (old scan)` / `File not found`。

Load 写入 base/camera、建立未作场景 WB 的 baseline，再套当前预设一次，关闭 Auto。失败不改变原来的 base。pending matrix 同时变换影像和导入片基；baked matrix 不重复应用。device `light_rgb` 只是 provenance/PWM，绝不当作归一化片基。

## Resolve 自动源路径边界

标准 OFX 不保证原始源路径。探测公开记录的 Resolve 20 `OfxImageEffectPropSrcFilePath`，依次检查 render args、输入 image、effect、source clip；缺失就用明确文件路径 + Load。公开依据：[ASWF #142](https://github.com/AcademySoftwareFoundation/openfx/issues/142)。未安装私有 Resolve SDK，不能保证各版本都暴露该属性。

Auto 有效时 metadata 只覆盖 render 副本，UI 不跳动；没有/损坏 metadata 回到手动，Load 可显示原因。代理、复合片段、序列 `%04d`、不同文件、上游非线性/色域变换可能使路径与像素不匹配。只有未被改写的同一线性 NSS 扫描可用，不是自动色彩管理。更改 camera/neutralise 后应重新确定片基。sidecar 导出脚本当前限 classic TIFF，插件自身支持 BigTIFF。

## OpenCL 与 CPU

- Windows 系统目录动态加载 `OpenCL.dll`；无额外 OpenCL/C++ runtime DLL。官方 OpenFX OpenCL **buffer** contract，宿主给 queue/context/device 与 cl_mem；不宣称支持 OpenCL image、CUDA/Metal。
- 每实例缓存编译 program，逐 render 独立 kernel/常量，线程安全；沿用宿主 queue，不读取/释放宿主 buffer。正常渲染异步 enqueue，不 finish/wait。Auto 未被宿主提供 OpenCL 时直接 CPU；若显示CPU，检查Resolve GPU processing mode及驱动OpenCL支持。CUDA模式是否给插件OpenCL queue仍待真机验证，不能强行把CUDA指针当OpenCL buffer。
- GPU 错误返回 OFX `kOfxStatGPURenderFailed` (1002)，按规范允许宿主 CPU retry，不能保证特定 Resolve 实现一定重试；必要时选择 Render engine → CPU。Show render engine 显示最近实际路径/编译错误，不以“支持标记”冒充已在 GPU 执行。
- CPU 优先宿主 multiThread suite，缺失则最多8个 std::thread，按行块并行。逐帧预计算 gain/fog/gray/contrast/matrix，跳过不需要的 Rec709 pow、identity camera。保留精确 exp/log，不使用近似 LUT/native OpenCL math 或不安全 `-ffast-math`；采用 `-O3 -fno-math-errno -fno-trapping-math`，保留 NaN/有限值校验。
- 原生 OpenCL 测试通过 **PoCL CPU** 真 queue/buffer，实际调用插件 Render entry + runtime CL loader + kernel；不是 fake OpenCL。没有物理 GPU/Windows Resolve 性能数据。首次 kernel 编译可能有一次延迟。

## 验证与性能

- Go v0.90 writer fixture 真实写 TIFF，当前插件导入；classic/BigTIFF LE/BE、稀疏 >4GiB metadata offset、Unicode/quoted/file URL/BOM、实际旧156MB TIFF、坏 metadata 均测试。
- 最小 OFX suite host 调用真实 Load/Describe/Create/Render/Changed/ROI/Destroy：负 row stride、非零 origin、alpha/premult、自动 metadata、立即 stock/gamma/base、反复 Load/切换不累乘、WB 正号、picker 中性化及越界 fallback。原生 viewer handle 外观未测。
- CPU 对独立 Go DCTL port：100组合×360像素×RGB =108000；旧Go隐藏WB关闭，用相同可见manual gains等效比较。最大绝对差 **9.16e-5**（HDR），实际 scan patch 最大 **6.10e-5**，scaled max **1.50e-6**。通过 `1e-5*max(1,abs(reference))`，不声称 HDR 绝对差均≤1e-5。[结果](data/ofx_0901_parity_result.json)。
- 真实 OpenCL Render：164组合 +4个二维 picker/prepass 组合，193536通道比较，最大绝对差 **2.29e-5**、scaled **9.93e-7**（相对 CPU）。[结果](data/ofx_0901_opencl_result.json)。不同 GPU 驱动仍需验收。
- Windows x64 MinGW 静态 C++/gcc DLL 导出2个 OFX 入口，仅系统 KERNEL32/msvcrt imports；OpenCL.dll 是运行时加载，不是硬依赖。

### CPU benchmark（不是 Resolve FPS）

0206 全帧 6246×4170 =26,045,820 像素，内存 float RGBA。Linux box 8 vCPU，Intel Xeon/KVM；相同编译器与 `-O3 -fno-math-errno -fno-trapping-math`。对照提交 `fc8dd26` 单线程（旧版发货O2，但这里双方统一O3）；相同 .5 base/原gamma曝光Cineon、identity camera、旧cross关闭；双方使用相同实测Base（旧参考以None+manual R1.57/B1.00等效）。每路径预热1次，5组交替测，包含 preparation/线程启动，消费 checksum并逐值比较。

| path | median ms | min–max ms |
|---|---:|---:|
| 原 v0.90 单线程 | 3779.940 | 3757.330–3808.072 |
| 0.90.1 prepared 单线程 | 1796.088 | 1780.715–1822.019 |
| 0.90.1 prepared 8线程 | 331.550 | 283.902–336.799 |

该测试范围 **11.40×**，default全帧输出 bit-exact（max error0）；优化前后不是改低分辨率/缓存结果。单线程2.10×，再8线程5.42×，支持“重复逐像素计算/单核算术限制”的解释，不是 TIFF decode/I/O 加速。没有把 optional picker readback 纳入默认关闭基准。

范围只含 CPU 内存 render；不含 decode、metadata I/O、viewer/host调度、GPU传输/执行。**不等于 Windows/Resolve 端到端加速11.40×**；真实时间待用户验收。[原始5组、误差和checksum](data/ofx_0901_cpu_benchmark.json)，复现 `ofx/tests/benchmark.sh`。

科学限制：T=5219/3200K，D=5207/5600K（其他 daylight stocks 暂共用5207）；Planck 非实测 LED/CIE D。Status M 曲线 slope 与 scanner RGB gamma 不等价，见 [5219 LED](data/5219_led_gamma.md)。不改扫描灯/相机 WB/已取消400K stash。

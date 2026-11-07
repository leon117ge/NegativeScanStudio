# NegativeScan Studio OFX 0.90.3 · Windows x64

插件独立更新；NSS app 仍为 **v0.90**，本 ZIP 不包含/不更新 exe。

## 安装 / Installation

1. **退出 Resolve**，备份工程，移除旧 bundle，避免同时装两个相同插件。
2. 解压 ZIP，把整个 `NegativeScanStudio.ofx.bundle` 复制到：
   `C:\Program Files\Common Files\OFX\Plugins\`（可能需要管理员权限）。
3. 重启 Resolve，在 OpenFX → NegativeScan Studio 找 **NegativeScan Studio OFX 0.90.3**。
4. Render engine 默认 Auto（按宿主提供的CUDA device pointer / OpenCL buffer使用）；不支持时 CPU（可检查Resolve GPU processing mode/驱动OpenCL支持；CUDA路径已实现但真机协商待测）。Show render engine 可查看最近实际路径；异常可切 CPU。

Quit Resolve, back up your project and remove the previous bundle. Unzip and copy the **entire** `NegativeScanStudio.ofx.bundle` to `C:\Program Files\Common Files\OFX\Plugins\` (administrator access may be needed). Restart Resolve and add **NegativeScan Studio OFX 0.90.3**. Do not install two copies. Auto uses CUDA or OpenCL buffers when offered by the host; CPU fallback is available. No extra C++/OpenCL DLLs are bundled or required for CPU use. This plugin update leaves the v0.90 app unchanged.

**0.90.3 未在真实 Windows Resolve/物理 GPU 验收**；已测native CPU、PoCL CPU OpenCL、CUDA PTX离线组装和Driver契约mock；没有真实CUDA执行。viewer 2D handle 的呈现亦待 Resolve 测试。

## 处理链 / Pipeline

**线性 camera calibration → Density In → Positive Out**。按反馈删除 mask/crosstalk 全组与阶段，保留 camera calibration。输入应为未转正的线性 NSS TIFF；不要重复套旧 DCTL 链。Input is DaVinci Intermediate 只解码 DI 传递函数，不做 gamut 转换。Cineon/DI/ACEScct/Linear/Rec709 是原 DCTL 编码公式，不是完整色彩管理/打印模拟。

新实例 Film base 改为 **0.5/0.5/0.5**；Positive gamma sliders默认 .55/.635/.62，Gamma scale默认1.15，500T Base默认加载可见balance1.07/0/0.67、曝光+0.90、Cineon 输出保留。旧项目中的 base 值可由宿主恢复，但被移除的 crosstalk 不再生效；请先在工程副本测试。

## 0.90.3 Base-only patch

500T Base now loads **+1.07 / 0 / +0.67 stops** (previously +1.40 / 0 / +0.80), as eye-tuned by the user. New-instance visible balance defaults match. Existing projects retain host-saved slider values; reselect 500T Base to load the new preset. App EXE, gamma/default exposure, GPU code and film-base tables are unchanged.

## 0.90.2 gamma与曝光（保持不变）

Stock gamma预设保持0.90.1原始值，**不做×1.25**。Positive Out中Gamma scale紧接Gamma B，默认 **1.15**，有效gamma=R/G/B slider×scale；Exposure默认 **0.90**。这些是用户选择的观感调整，不是改写官方曲线数据。新默认有意改变默认输出；要对照旧版/DCTL设scale1、Exposure1，或给参考相同有效参数。旧项目请备份。

500T LED显示值仍 .45/.54/.53，默认scale1.15使有效值 .5175/.621/.6095。旧Base测量条件为scale1；当前Base是新默认下用户眼调的1.07/0/0.67。Gamma sliders与scale在Positive Out，stock/T-D/scene/base在Film base and balance。

The stock preset numbers are unchanged. Gamma scale, directly below Gamma B in Positive Out, defaults to1.15; Exposure defaults to0.90. These intentional defaults change the render. Use scale1 and Exposure1 for an unchanged-DCTL comparison, or supply matched effective parameters.

## Film base and balance

- 同一组包含 stock gamma preset、T/D、3200/3800/4400/5000/5600K、Film base（Gamma R/G/B与scale在Positive Out）。
- 选择 stock **立即**填 gamma；500T→T，其他→D，可覆盖。保留 Legacy/manual、500T Status M、500T LED .45/.54/.53、250D、UltraMax400、Portra400、Gold200。
- 选择 T/D/色温 **立即**填 Film base，无 cast sliders / Apply。`base = baseline × 10^(+offset)`，与 Density_In 的 signed additive density offset 同号等价。
- baseline 新实例 .5/.5/.5；Load 后是文件片基（pending camera 处理后、场景 WB 前）。单独保存 baseline，切换不累乘；手动微调保留，下一次选预设从 baseline 重置微调。
- **保留** Positive Out 的 Film white balance，下拉为 **None / 500T Base**。500T Base **立即写可见** Red/Green/Blue balance = +1.07/0/+0.67 stops，可手改；None复位三条为0。render不再加额外隐藏ar/ab，没有双重应用/旧white-code slope。
- T/D、stock gamma、Base balance独立，切换前两者不清空balance，可显式叠加。500T Base当前为用户在T5600、Gamma scale1.15/Exposure0.90新默认下眼调的+1.07/0/+0.67，旧实测+1.57/0/+1.00保留作历史，不是通用Kodak常数。将来每stock仅一个实测Base，其他待测。
- T=5219/3200K、D=5207/5600K，其他 daylight stocks 共用5207模型；全为 Planck 近似，非实测 scanner/LED 校准。不改物理调平灯值。

Stock/type/temperature changes immediately populate controls. Manual fine tuning stays available. WB changes the effective film base, not lamp drive. Repeated selection uses the original loaded/default baseline, not the previously compensated result. The Positive WB dropdown is a loader: 500T Base writes +1.07/0/+0.67 into visible RGB balance sliders, with no hidden extra gain. None resets them to zero. Type/temperature and balance are independent and can explicitly stack.

数值、来源与误差：[片基表](https://github.com/leon117ge/NegativeScanStudio/blob/main/docs/data/ofx_wb.md)、[gamma](https://github.com/leon117ge/NegativeScanStudio/blob/main/docs/data/ofx_stock_gamma.md)。

Base校准：[测量条件与可见loader规则](https://github.com/leon117ge/NegativeScanStudio/blob/main/docs/data/ofx_film_bases.md)。

## Viewer gray / white point

开启 **Auto gray from point**（Base下拉置None、balance可见复位0，开始新中性校正），用 viewer 原生 2D point handle 拖到中性灰/白区域，或手填 canonical X/Y。Sample radius=0–32（当前 render 像素）。逐帧从 SOURCE patch 经 camera/Density In 后取平均 code，使用原 Positive gray-point 算法中性化 R/B 相对 G；不写 UI/key、不锁曝光。关闭后恢复手动 gray 参数。

GPU 开启 picker 时只下载小 patch（有一次同步），默认关闭不增加该开销。不是运动追踪；避免饱和白/纯黑/有色区域。点越界/透明时回退手动 gray，Show render engine 显示 pick unavailable。无 bake 按钮/自绘 overlay，原生 handle 需宿主支持；之后手动balance可在灰点上继续微调；关闭Auto不自动恢复Base。

Enable **Auto gray from point**, move the host's 2D on-screen handle onto a neutral patch, or enter canonical X/Y. Sampling happens on source pixels after camera/Density In, before Positive gain. It updates render-local gray codes each frame, not slider/keyframe values. This is a fixed point, not a tracker; GPU sampling downloads only the small patch. Native overlay availability depends on the host.

## TIFF / JSON 加载

NSS v0.90 HDR/三色扫描将 `nss.scan/1` JSON 写入 **ImageDescription/tag270**：扫描灯 RGB drive、线性片基、相机矩阵及是否已烘焙、灯型号、版本等。`light_rgb` 是设备 drive，**不是** `film_base`。旧文件不追溯补写未知值。

1. 选 Scan TIFF or sidecar JSON → **Load from file (or Resolve source)**。填 base/camera 并关闭 Auto，可以手动覆盖。所选温度补偿以文件片基为 baseline 套一次。
2. 状态短且明确：Loaded: … / No NSS info (old scan) / File not found。用户的旧0206.tiff实际没有 tag270，无法从无信息的旧图恢复 RGB。
3. 路径支持引号、UTF-8/Windows Unicode、file://；classic TIFF/BigTIFF LE/BE、64-bit offset。只读 header/IFD/≤1MiB metadata，不加载156MB像素。
4. Auto-read Resolve20 source metadata 是可选源路径扩展探测，不保证每个 Resolve 版本/代理/复合/序列可用。有效 metadata 只改 render 副本，UI 不随帧跳动。手改 base/选 WB/Load 会关闭 Auto。
5. 已处理/无效 metadata 拒绝加载，失败不改 base；Auto 缺信息保留 manual。源像素必须仍是同一扫描的线性 RGB，不追踪上游节点色彩变化。
6. `python export_metadata.py scan.tiff` 按需导出 `.tiff.nss.json`，不覆盖现有 sidecar；该辅助脚本目前限 classic TIFF，插件本体支持 BigTIFF。

Use a TIFF/JSON path and **Load** for portable operation. Resolve's optional source-filename extension is probed, not assumed. Old scans without metadata need manual values; no values are invented. Pending camera matrices transform both image and imported base; baked matrices are not reapplied.

## 性能与限制 / Performance

CUDA Driver路径动态使用系统 `nvcuda.dll`，PTX由驱动按host context JIT一次，使用宿主stream/device buffers，不需用户安装toolkit。PTX8.0/sm_52，需CUDA12兼容NVIDIA驱动；离线ptxas已验证，无真实GPU执行。OpenCL1.2同样动态加载系统OpenCL.dll，按context/device缓存program/kernel。当前Windows包无Metal。

播放/改参数后点Show render engine确认实际路径：CUDA PTX / OpenCL buffers / CPU N threads。驱动available不代表宿主已启用GPU。NVIDIA/Resolve的CUDA模式与旧OpenCL-only插件不匹配是可能的CPU回退原因，仍需用户机验证。失败返回 GPU render failure 允许 host retry，必要时手动 CPU。CPU 逐帧预计算、最多8线程；不用不安全 fast-math 或近似 LUT。

历史0.90→0.90.1基准在 Linux 8-vCPU box、6246×4170 内存 RGBA、同O3、预热后5组对照：旧单线程 median **3779.94ms** → 新单线程 **1796.09ms** → 新8线程 **331.55ms**（11.40×；该默认路径输出 bit-exact）。**不是 Resolve FPS/Windows 端到端速度保证**，不含 decode/宿主/UI，也不含默认关闭的 picker；实际 GPU性能待验收。实测PoCL OpenCL parity，不代表物理GPU/Resolve已测；不是0.90.2额外11.40×提升。0.90.2匹配实际CPU Render对照：405.33→416.59ms，5组范围重叠，**没有额外CPU加速结论**。CUDA/真实Resolve性能待测，见docs/OFX.md。

## Build / test

```sh
./ofx/build.sh /absolute/output/directory
NATIVE_CXX=c++ ./ofx/test.sh /absolute/test/directory /path/to/0206.tiff
NSS_TEST_OPENCL=1 NATIVE_CXX=c++ ./ofx/test.sh /absolute/test/directory /path/to/0206.tiff
NATIVE_CXX=c++ ./ofx/tests/benchmark_host.sh /absolute/test/directory /path/to/0206.tiff
```

MinGW-w64 g++、Python3、native C++17、Go/LibRaw 开发环境；parity用numpy/tifffile，可选OpenCL测试需pyopencl和ICD（例如PoCL）。官方 ASWF OpenFX headers revision见 vendor/openfx/REVISION；新增 ofxGPURender.h 同revision。Khronos OpenCL-Headers v2025.07.22（Apache2）、nlohmann/json3.12（MIT），许可证随ZIP提供。构建静态libstdc++/libgcc，DLL import仅系统KERNEL32/msvcrt。

CUDA PTX仅开发者改kernel时重建：安装固定NVRTC12.0.140后运行 `python3 ofx/tools/compile_cuda.py`；普通build校验已提交PTX的source/hash，不需要CUDA toolkit。NVIDIA工具/驱动各有自己的许可，不随ZIP分发。

完整 schema、科学近似和验证结果：[docs/OFX.md](https://github.com/leon117ge/NegativeScanStudio/blob/main/docs/OFX.md)。

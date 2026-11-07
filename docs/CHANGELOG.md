# 更新日志 / Changelog

根据源码中的版本注释、备份文件与说明文件整理（v0.70 之前为 FilmBaseMeter v0.1–v0.67）。

- **v0.70** – Develop 模块：三个 DCTL 移植到 Go，Resolve 式节点链；Ektar 相关数据。
- **v0.71** – Develop 预览按帧旋转/翻转；默认输出为显示编码；嵌入 2383 (D65) LUT；卷打开与片基估计。
- **v0.72–v0.74** – 色彩/测试参数与命令行选项调整。
- **v0.75** – X-T3 实测预设与矩阵行；White LED / Narrowband 光源选择；QA 合成工具。
- **v0.76** – 矩阵行缩放保持中性等默认值调整。
- **v0.77** – 引导式去毛发/纤维。
- **v0.78** – Vision3 500T 相关数据与测试、默认值调整。
- **v0.79** – 纤维标记改进。
- **v0.80** – RGB Light Air 设置页；ISO + 固定快门；传感器马赛克（感光点平均）合成。
- **v0.81** – 三色扫描驱动灯；base shutter；串口原始日志；CFA 逐色缩放。
- **v0.82** – 调平结果与 Light 页联动；三色片基送 DCTL；三色+HDR 扫描。
- **v0.83** – 扫描配置预设（scanconfig）；HDR 子页重组；可停止的包围；RAW 清理；NegPy 风格折叠卡片。
- **v0.84** – 相机+灯输入色卡校准（3×3 保持中性，CIEDE2000）。
- **v0.85** – 调平逐通道目标（profile 矩阵前片基、TIFF 框修正）。
- **v0.86** – 调平/Light 页改进。
- **v0.87** – 三色自动调平支持手选片基矩形。
- **v0.88** – Light 页共享实时画面直接拖框；调平 R/G/B 读数显示、复制、CSV 日志；「写入 DCTL」写入 Density_In 片基与预设。详见 `NegativeScanStudio_v0.88_notes.txt`。

- **v0.89** – 优先实现 500T 3200K 平衡。基于 Kodak H-1-5219 rev.3-26 的光谱感度 10 nm 转录、Planck 光源积分和 ECN-2 gamma，生成有来源的近似密度偏移。支持 3200/4300/5500/6500K 和自定义 K，Light 页写入 DCTL、胶片库保存与加载、发送到 Develop。调平后不改灯、不改片基、不改相机 WB。Density_In 增加默认关闭的 `cast_on/cast_r/g/b`，Go Develop 同步公式。灰点实测与交叉偏色拟合后续实现。

- **v0.90** – 首个 Windows x64 CPU OpenFX 一体插件：线性 CameraCal → Density In → Mask/Crosstalk → Positive Out，保留模板默认值；T/D 两套3200/3800/4400/5000/5600K signed density-offset presets、手动微调；500T/250D/UltraMax400/Portra400/Gold200官方曲线中段gamma预设与500T LED估计；片种默认T/D可覆盖。HDR/三色TIFF tag270写入versioned scan JSON，hair/dust/crop保留；OFX尝试Resolve20源路径扩展，另有TIFF/JSON选择+Load并保留manual override。最小OFX host与独立Go DCTL parity已测，静态链接DLL及NSS exe交叉编译；未在真实Resolve/Windows GUI/硬件验收。科学模型/完整精度/误差见 `docs/OFX.md` 和 `docs/data/ofx_*`。400K WIP stash未应用。

## OFX 插件独立版本

- **OFX 0.90.1**（`ofx-v0.90.1`；NSS app 保持 v0.90）– 移除整个 mask/crosstalk 组与阶段，保留 camera calibration；合并 stock/gamma/T-D/色温/Film base，base 默认0.5，预设立即按 `baseline × 10^(+offset)` 更新，无独立cast/Apply。metadata base 作未补偿baseline，防反复选择累乘；Positive WB保留并改None/500T Base loader：实测+1.57/0/+1.00立即写可见balance，无隐藏双倍gain/旧white-code slope；None复位0、可手改，各stock将来仅一个实测Base，T5600不改。文件加载短状态、引号/fileURL/Unicode、classic/BigTIFF LE/BE、64-bit seek、只读IFD/description；确认旧0206无tag270。新增viewer原生2D灰/白点+半径，SOURCE→Density采样逐帧gray correction，OpenCL仅下载小patch。Windows动态OpenCL buffer kernel/CPU fallback、逐帧预计算和最多8线程。全帧CPU benchmark 3779.94→331.55ms（Linux8vCPU、同O3、5组median、非ResolveFPS）；实际PoCL OpenCL/CPU/metadata/WB回归通过。新插件尚未在真实Resolve/物理GPU验收，见 `docs/OFX.md`。

## 仓库说明
仓库已通过 git 上传完整源码和构建所需的图标、资源与 LibRaw 静态库。Windows 可执行文件放在 GitHub Releases，不随源码重复提交。

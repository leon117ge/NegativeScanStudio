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

## 仓库说明
仓库已通过 git 上传完整源码和构建所需的图标、资源与 LibRaw 静态库。Windows 可执行文件放在 GitHub Releases，不随源码重复提交。

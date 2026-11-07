# v0.90 一体 OFX 与 TIFF provenance

状态：Windows x64 CPU 原型已交叉编译并通过 host-free 验证；**未在真实 DaVinci Resolve、实际 Windows GUI、真实扫描硬件中验收**。本版不改变旧 DCTL 默认值、物理调平灯光、旧 v0.89 光源模型 UI；已取消的 400K stash 保持未应用。

安装与参数操作：[OFX 中英说明](../ofx/README.md)。

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

## 自动读取的边界

标准 OFX 只提供像素/clip 时间，不保证原始文件路径。公开 ASWF issue #142 的 Resolve 20 开发者说明记录了 `OfxImageEffectPropSrcFilePath` / `OfxImageEffectPropSrcFrame`（https://github.com/AcademySoftwareFoundation/openfx/issues/142）。本版探测 render args、输入 image、effect instance、source clip 属性；缺失时走文件参数 + Load，而非承诺宿主一定支持。无需源 frame 时不使用 `SrcFrame`。没有 Resolve 安装，未独立打开其随附的私有 SDK，扩展真实可用性仍待用户测试。

每个实例只缓存一个路径/mtime 及已验证 metadata；render 内不改 UI、异常不跨 C ABI。显式文件损坏时报错；宿主自动路径没有可用 NSS metadata 时回到手动值。手动 Load 写参数并关闭 Auto，可撤销/继续手改。Auto 开启时 valid metadata 的 base/camera 覆盖 render 副本，不能同时假称 manual base 生效。路径 Unicode 用 UTF-8→Windows filesystem path，classic LE/BE TIFF 有长度/类型/数量检查，最多读 1MiB metadata，不解码全 TIFF 像素。

负片必须是未被上游节点改写的同一线性 RGB。代理/复合片段/静帧序列/DI 以外的 timeline transfer/gamut、显式不同文件、灰点和相机 profile 变更可能使 metadata 与实际像素不匹配；由使用者核对。更改 camera matrix/neutralise 后应重新确定片基。自动读取不是自动色彩管理，也不是对片基实测质量的担保。

## 色彩模型

- Density In / Crosstalk / Positive Out 按三个模板生成 C++，camera 按 `colorsci.CamDCTL` 的线性单位矩阵混合公式。固定矩阵、slider 默认值/范围、灰点、contrast、编码分支保持。用户动态保存的 base/gray 库可手填，未假造为内置预设。
- Positive Out 默认 gamma=0.55/0.635/0.62，默认 legacy WB=1，曝光=+1，输出=Cineon，不默换 LED gamma。
- [T/D 色温表](data/ofx_wb.md) 与 [片种 gamma](data/ofx_stock_gamma.md) 是两个独立模型。温度只改密度偏移，不改 gamma/扫描灯。
- LED forward Jacobian 与 Status M 测量轴不等价，详见 [5219 LED 研究](data/5219_led_gamma.md)。不能把任意 stock Status M gamma 宣称为经过 camera/crosstalk 后的精确 gamma。

## 验证

- `go test ./scanmeta ./rawconv ./hdrscan ./colorsci ./cropper ./develop` 全通过。
- TIFF writer 真正写出 24×24 uncompressed/LZW TIFF，重新解析 tag270；中文卷名往返、实际合成三色扫描与crop/hair rewrite保留、插件导入 pending camera/base 数值正确。辅助 tests 覆盖 BE TIFF、Unicode 路径、sidecar、错误 schema/encoding、非正片基、错误数组、坏 matrix/strength、截断 TIFF。
- 最小 OFX suite host 直接调用插件真实 exported entrypoints：Load/Describe/DescribeInContext/Create/Render/InstanceChanged/Destroy；nonzero origin、negative row stride、RGBA alpha/unpremultiply、Resolve-like 源路径属性、Load snapshot/manual override、片种 gamma/T-D override。不是只测一个 C++ 公式函数。
- 140 参数组合 × 360 像素（0206.tiff 16×16 实际采样 + 边界 + 固定种子随机值）×3 = **151200** 通道比较；独立 Go Develop DCTL port vs C++。全输出分支、contrast、gray point、DI、transmittance、signed cast、black subtract、custom matrix/mask/clamp。
- 最大绝对差 **2.67e-5**，出现于 HDR linear ≈18.29；实际图样本最大绝对差 2.29e-5；scaled max `abs(error)/max(1,abs(reference))` = **2.39e-6**，通过 `1e-5*max(1,abs(reference))` 的 float32 检查。**不是宣称所有 HDR 值都满足绝对1e-5**。单精度约1–数个 ULP，Go/C++ exp/log 实现/计算次序不同。未执行 Resolve 内 GPU DCTL，不以 host-free parity 冒充真实宿主结果。机器可读结果：[ofx_parity_result.json](data/ofx_parity_result.json)。
- Windows DLL 导出 OfxGetNumberOfPlugins/OfxGetPlugin，静态 C++/gcc 链接；import 表仅 KERNEL32.dll/msvcrt.dll。NSS Windows exe 交叉编译成功。

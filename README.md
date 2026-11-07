# NegativeScan Studio

胶片负片翻拍（相机扫描）工具，Windows 桌面程序，Go + [lxn/walk](https://github.com/lxn/walk) GUI。
当前版本 **v0.88**。作者：**Grok Bot**（AI 助手），为 leon117ge 编写与维护。

A Windows tool for scanning film negatives with a tethered camera (Fujifilm X-T3) and a
CuteNewDesign RGB Light Air backlight: film-base metering, light-based base leveling,
HDR / trichrome capture and merge, crop, hair removal, and DaVinci Resolve DCTL export.

## 功能模块（程序顶层标签）
1. **DCTL** – 片基测量、片基预设、DCTL 导出（FilmScan_Density_In → FilmScan_MaskCrosstalk → FilmScan_Positive_Out）。
2. **Camera & Light** – 相机联机（PTP/WPD）、RGB 灯控制、片基调平（Light / White light / Trichrome 页共享实时画面与手选框）。
3. **HDR Scan** – 片基测光、包围曝光拍摄与合成、三色（trichrome）扫描。
4. **Crop & Merge** – 自动裁切、合并文件、去毛发/纤维、Develop 预览。

## 代码结构
| 路径 | 内容 |
|---|---|
| `*.go`（根目录） | walk GUI 各页面与粘合逻辑（`main.go`, `hdrpage.go`, `lightpage.go`, `tripage.go`, `croppage.go`, `developpage.go`, `maskpage.go`, `presets.go`, `leveling*.go` …） |
| `basestat/` | 片基统计（16-bit 线性/65535、漏光剔除、逐通道中位数） |
| `camera/` | PTP over Windows Portable Devices，富士 X-T3 属性，模拟相机 |
| `rgblight/` | RGB Light Air 串口协议 RGBAIR API 1.3（只实现官方客户命令） |
| `lightlevel/` | 片基闭环调平（固定曝光，只调灯三通道） |
| `hdrscan/` | 片基检测、包围合成（Classic / Weighted / 传感器马赛克）、三色、去尘去毛发 |
| `colorsci/` | 色彩科学：Cineon 密度、橙色片基补偿、染料串扰、色卡 3×3 校准、胶片库 |
| `develop/` | 3 个 DCTL 的 Go 1:1 移植，Resolve 式节点链 + 2383 LUT |
| `cropper/` | 画幅边缘检测与裁切 |
| `rawconv/` | LibRaw RAW → 线性 16-bit TIFF |
| `filmdb/`, `scanconfig/` | 片基数据库、扫描配置预设 |
| `cmd/` | 开发/QA 命令行工具 |
| `third_party/` | walk（本地修改版，go.mod replace）、LibRaw 头文件、rawtoaces 数据 |
| `docs/` | 设计、算法、硬件、更新日志、路线图、RGBAIR API 文档 |

## 构建（Linux 交叉编译 Windows）
```sh
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ GOOS=windows GOARCH=amd64 \
  go build -trimpath -ldflags "-H windowsgui -s -w" -o NegativeScanStudio.exe .
```
需要 `rsrc_windows_amd64.syso`（图标+manifest）和 `third_party/libraw/lib/windows_amd64` 下的 LibRaw 静态库。
注意：二进制文件（.syso、.ico、.icc、LibRaw 静态库）若仓库中缺失，见 docs/CHANGELOG.md 的“仓库说明”。

## 文档
[设计](docs/DESIGN.md) · [算法](docs/ALGORITHMS.md) · [硬件](docs/HARDWARE.md) · [更新日志](docs/CHANGELOG.md) · [路线图](docs/ROADMAP.md)

## License
MIT © 2026 Grok Bot. Third-party code under `third_party/` keeps its own licenses (walk: BSD-3; LibRaw: LGPL/CDDL).

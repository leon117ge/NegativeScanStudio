# 硬件 / Hardware

## Fujifilm X-T3 联机（`camera/`）
- 通过 Windows Portable Devices 的 MTP/PTP 扩展命令（`wpd_windows.c/.go`），富士属性码表在 `fujinames.go`。
- 功能：实时预览、拍摄并取回 RAF、设置 ISO 与固定快门（如 1/60、1/120 s；v0.81 起有 base shutter）、包围曝光（`CaptureBracketEV`，可中止）。
- `camera/sim.go` 提供模拟相机用于测试。

## CuteNewDesign RGB Light Air（`rgblight/`）
- USB CDC 串口，协议 **RGBAIR API 1.3**（官方文档见 `RGBAIR_API_1.3_official.txt`）。
- 帧：`AA 55 | TYPE | SEQ | LEN(u16 LE) | PAYLOAD(CMD 开头) | CRC16 LE`，CRC-16/CCITT-FALSE（poly 0x1021，init 0xFFFF）覆盖 TYPE..PAYLOAD。
- TYPE：01 命令、81 响应、82 遥测、83 事件。
- 仅实现客户命令：01 亮度、02 开关、03 全部设置、10 模式、11 速度、12/13 时序、14/15 对焦光、20 状态、21、22。其它命令（序列号写入、固件升级/YMODEM）一律拒绝。
- v0.81 起记录原始串口日志；`sim.go` 为模拟灯。

### RGB 光谱记录（用户提供，报告日期 2026-10-14）

| 通道 | peak / nm | FWHM / nm | dominant / nm |
|---|---:|---:|---:|
| R | 650.1 | 22.2 | 633.7 |
| G | 539.9 | 38.7 | 547.6 |
| B | 450.7 | 18.6 | 456.7 |

- 参数来自用户转录/提供的测量报告；本次研究执行器未能打开原图片附件，未独立复核原图。未取得逐波长 SPD 或相机 QE。
- 计算暂按各通道高斯 SPD：`exp(-4 ln(2) ((λ-peak)/FWHM)^2)`。dominant wavelength 不是光谱峰值，不能代入 peak。
- 这些波长是**扫描灯光**，不是胶片拍摄场景的 3200K/5500K 光源；两种计算不能混用。三通道强度经片基调平只改变比例常数，不等于标定 gamma。
- 5219 + 此光谱的条件性 gamma 估计与串扰矩阵：[研究记录](data/5219_led_gamma.md)。名义 R/G/B=0.453/0.536/0.527，非设备实测；尚未修改全局默认值。


## 计划：Hikrobot 工业相机
`lightlevel.Camera` 是接口，注释写明以后可由单色工业相机（如 Hikrobot MVS 机身）实现。单色机配合三色灯可直接做 trichrome 扫描，无 Bayer 串扰。目前**尚未实现**。

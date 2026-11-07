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

## 计划：Hikrobot 工业相机
`lightlevel.Camera` 是接口，注释写明以后可由单色工业相机（如 Hikrobot MVS 机身）实现。单色机配合三色灯可直接做 trichrome 扫描，无 Bayer 串扰。目前**尚未实现**。

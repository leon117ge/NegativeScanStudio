# 路线图 / Roadmap

## v0.90 已实现（待真实宿主验收）

- Windows CPU OFX一体链、T/D 600K档位、Kodak官方曲线片种gamma预设。
- HDR/三色TIFF metadata、Resolve源路径探测、手动TIFF/sidecar Load。
- Go writer往返、最小OFX host、float32 math parity；见 [OFX验证](OFX.md)。

## 已完成 v0.89

- 500T 5219/7219 的 3200K 光源平衡模型。Kodak 感度积分与 ECN-2 中间 gamma 推导密度偏移。
- 3200K、4300K、5500K、6500K 和自定义 2500–10000K。所有场景光谱均用 Planck 近似。
- Light 页的 DCTL-only 控件、导出默认值、胶片库参数保存与加载、Develop 同公式预览。
- 片基调平后不改灯。模型近似值在界面和文档中明确标注。

## 下一步

1. 先在真实 Resolve 20 / Windows GUI / 相机+灯中验收 v0.90，确认OFX加载、参数UI、源路径扩展、metadata与手动覆盖；GPU路径和自动序列路径后续。
2. 灰点实测。为中性参考增加独立选框，用同一线性图上的片基和灰点求密度偏移。
   保持灯光、曝光和相机 WB 不变，把偏移写进 DCTL。
3. 灰阶拟合。用拍在该卷上的多个中性灰阶拟合每层斜率，修正交叉偏色。
   透射色卡本身不是经过该胶片曝光冲洗的灰阶，不能直接拿来测乳剂 gamma。
4. 测扫描系统的密度串扰，区分感光层轴与扫描 RGB 轴。加入实测光谱和 CIE D 系列模型。
5. 用同卷灰卡核对 500T 模型，测模型误差，再确定是否有足够依据默认启用。
6. Windows GUI、Resolve DCTL 和真实 X-T3 / RGB Light Air 验收。当前只有软件测试与交叉编译。

## 其它

- Hikrobot 单色工业相机支持。
- 灰点和灰阶模式完成前，不提供宣称精确的自动去偏色功能。

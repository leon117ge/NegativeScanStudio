# Historical 500T DCTL workflow through v0.90

这是 Kodak 数据表推导的近似模型，不是该卷的实测白平衡。
如果拍摄时用了 85 滤镜，或光源是混光、LED、荧光灯，不要直接套未滤镜日光预设。

1. 在 Camera & Light > Light 完成片基调平，按原按钮把片基 RGB 写入 DCTL。
2. 保持灯光不变。展开左侧 **Colour cast / 偏色校正**。
3. 选底片拍摄时的光源。未加滤镜的日光先选 **500T @ 5500K**。
   钨丝灯选 **3200K**，此项偏移为零。自定义范围是 2500–10000K。
4. 勾选 **Enable**，点 **Calculate**，查看有符号的密度偏移。
   5500K 约为 R +0.0926、G 0、B -0.1886 D。
5. 点偏色区域的 **写入 DCTL**。程序保存当前导出设置，不会发送任何灯光命令。
6. 到 DCTL 页导出 Density_In。如果打开 Auto-sync，现有导出路径会更新。
7. 在 Resolve 重新载入 DCTL，并重置节点或核对 `cast_on/cast_r/g/b`。
   已存在节点保留的 slider 值不会因文件更新自动变更。
8. 用 **Save stock preset** 保存为 `500T @ 5500K`。以后在偏色区域选已存 stock，
   点 **Load stock**，再点 **写入 DCTL** 激活。加载不会覆盖当前片基或改灯。

要在程序内看效果，点 **Send to Develop**。这个按钮只改当前 Develop 的 Density In
偏色参数，不重置已有的 Positive Out gamma、灰点或自动白平衡。
如果旧自动白平衡已去过同一偏色，先关掉它，避免重复补偿。

要停用，取消 **Enable** 再点 **写入 DCTL**。导出设置是独立的全局状态，
换成别种胶片或只切换片基 preset 不会自动关闭 500T 校正。

灰点实测与灰阶交叉偏色拟合不在 v0.89 中。软件测试和交叉编译已完成，
但没有在真实相机、灯和 Resolve 上验收。


This workflow is removed from app v0.91. See [current preview workflow](500T_USAGE.md).

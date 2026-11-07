# Film white balance：可见滑杆 Base loaders（OFX 0.90.3）

表源：[ofx_film_bases.json](ofx_film_bases.json)，生成 `ofx/src/film_bases.hpp`、下拉与默认值。每个 stock **最多且仅一个实测 Base 条目**；已支持片种各保留一个目标位置，不为未测的片种编数。目前只有 **500T Base**，其他 stock 待测后增加 `<Stock> Base`，不会按色温/gamma另造多个Base。`None` 是复位操作，不是片种。

| UI loader | Red balance / stops | Green | Blue |
|---|---:|---:|---:|
| None | 0 | 0 | 0 |
| 500T Base | +1.07 | 0 | +0.67 |

选择下拉，在 instanceChanged **立即写这三个可见 sliders**；可继续手改。默认下拉500T Base，默认sliders亦1.07/0/0.67。render不读取下拉计算额外gain：旧 `ar/ab` preset gain恒零（灰点另按原公式求gain），没有双重应用。选择 None 明确将三条balance复位0；可手动选择后再微调。下拉表示“最后加载的预设”，不是锁定sliders。

Film-base T/D、stock gamma、Base balance 是独立可见loader，切换T/D/stock **不再暗中清空或禁用**已经可见的balances；两者可以显式叠加。0.90.3当前Base由用户眼调指定；Gamma scale1.15、Exposure0.90默认保持不变，不是重新拟合的科学测量。T5600表未改。开启 Auto gray from point 是一次新中性校正：将下拉改None、可见balances清0，然后按source灰点逐帧求gray gain；之后仍可手动微调balance。关闭Auto gray不自动恢复旧Base，需要时重新加载。

## 0.90.3用户眼调决定

当前500T Base由用户指定 **+1.07/0/+0.67**（原+1.40/0/+0.80）。仅更新此loader与新实例可见balance默认值，没有执行新测量。已有工程保留宿主保存的slider值，重选500T Base可加载新值。

## 历史0.90.2用户眼调决定

当时500T Base从旧 **+1.57/0/+1.00** 改为 **+1.40/0/+0.80**。用户认为新默认下旧值偏红，直接指定此值。条件为Gamma scale1.15、Exposure0.90、T5600背景工作流；未额外断言该次眼调的raw gamma或white point。已取消重测，没有执行新的patch测量/拟合，也不把眼调数值标成Kodak官方值。

## 历史0.90.1测量条件与范围

用户提供 `/workspace/wb0206/residual_measurements.json` 的结果，原始数值存于 [ofx_500t_base_measurements.json](ofx_500t_base_measurements.json)：

- 0206.tiff 日光画面，baseline .5/.5/.5，**已启用T5600**，gamma .45/.54/.53、white code696。
- T5600 density offsets 保持 +.094824/0/−.193475；相应有效base .622006/.5/.320254，未再更改。
- 灰屋顶两区 R+1.57180/+1.56086，B+.94505/+.97872；白墙/白车约R+1.56696…+1.57164、B+.99378…+1.17346；云层R+1.67382、B+.87683。当时用户选取建议残差 +1.57/0/+1.00，现保留为历史记录，不是0.90.3当前preset。
- 这是该扫描系统/画面/流程的 **经验残差**，不是 Kodak 官方 stock 常数/独立色卡校准，不保证其他照明、scanner、曝光/冲洗/片种可直接使用。其他gamma/white point下可继续手调。
- R/B 为常量，不再随 white_code 乘旧0.0013斜率；旧条件是不同白光扫描，不能无依据迁移斜率。white_code仍按原Positive公式影响白点/反演，但不改变这两个loader数。

独立Go DCTL参考未修改：测试将其旧隐藏WB设None，并用等效manual balance进行比较。此请求有意改变了插件的WB数值，不宣称与原Legacy WB输出一致。v0.90旧工程请备份，必要时重新加载Base。

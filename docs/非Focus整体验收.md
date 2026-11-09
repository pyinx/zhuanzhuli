# 非 Focus 功能开发与整体验收

> 阶段更正（2026-10-23）：用户再次明确先完成开发、再进入正式验收。本文保留先前的分项诊断与回归记录；不表示已具备整体验收条件。通知面板关闭、外部同名模式恢复归属是开发缺口，自动返回超时仍需诊断，不应全归为验证缺证。当前状态见 [功能开发完成度](功能开发完成度.md)。

2026-10-23，macOS 15.6／arm64，Go 1.27.3、MyGo v0.6.2。按用户最新指令继续开发与自主验收。**保持系统“跨设备共享”原值，不切换系统 Focus，不用本轮结果替代通知效果验收。**产品设计、一键开始、暂停／退出、四主题和本地记录范围不变。

本轮发现的五类非 Focus 问题均已修复；主流程取得新的实机分项证据。完整产品仍未整体验收通过：系统通知能力按要求跳过，硬件、无障碍和部分系统兼容性仍缺证据。

## 修正

| 问题 | 修正与验证 |
|---|---|
| 待处理三选面板可能阻止退出，计时已停但进程留存 | 退出前清空待处理选择并关闭面板。真实故障及三个独立原生退出检查均先失败；修复后正常退出。真实 FixtureB 面板待处理时，再用 CUA Cmd+Q 核对进程退出、事件循环返回、pending=0 |
| 主动查看范围外选择仍不接收键盘，缺少应用菜单入口 | 托盘与原生“专注”菜单共用入口；主动查看时取得键盘、定位返回按钮，自动弹出仍不抢键盘。原生检查三次先失败再通过；三个按钮均经真实点击 |
| 删除／撤销写失败只在背后主窗报错 | 当前记录窗口就地说明失败，保留记录和撤销入口。删除失败三次复现；修复后删除、撤销各自经历写失败→修复目录→重试成功，顺序正确 |
| 长目标直接裁切，没有省略提示 | 会话与记录使用原生末行省略号、全文 tooltip／AX 值；计时和操作不移位。200 字完整保留，201 字明确拒绝；长列表截图已复核 |
| 偏好写失败后有效值和勾选状态仍改变 | 使用同一原子提交入口；失败不提交内存，勾选、音量、场景回到已保存值，设置页就地报错。应用名单也使用同一路径。三个失败回归先红后绿；真实设置故障、重试、添加／移除均有证据 |

只读验收状态和外观覆盖仅在 `--fixture-ui` 隔离运行生效，不是用户功能。深浅色覆盖仅作用于本应用实例，不写系统设置；普通启动拒绝该参数。没有增加按键注入、快捷指令、全局静音或隐私权限。

## 实际验收范围

| 范围 | 本轮实际结果 | 边界 |
|---|---|---|
| 目标与时长 | 空目标、201 字、小数分钟就地拒绝；200 字可开始；一分钟倒计时与超时继续 | Unicode 输入／粘贴不是 IME 候选验证 |
| 应用名单 | 系统选择器实际添加 FixtureA；移除写失败保留名单，恢复后重试成功 | 只操作自有夹具，不代替第三方办公应用兼容性 |
| 范围外三选 | Finder 实际打开 B；返回后 B.hidden=true、A.active=true、后台进度继续；加入、退出均实际点击 | 自动提示可见状态与主动查看后的操作分开取证；Dock／Cmd+Tab 未完整覆盖 |
| 临时加入 | 跨暂停恢复保留 B；默认名单只有 A；下次会话恢复为只有 A | 没有把临时加入写入偏好 |
| 暂停与超时 | 未确认恢复被拦截；确认后续接同一超时阶段；暂停 pending=0、背景／媒体停止 | 系统通知不在本轮效果范围内 |
| 四主题 | 森林、沙滩、草原、雪地均实际进入 AVPlayer 并保存截图；草原、雪地观察到 entry、elapsed=0、videoAllocated=true | 最终美术审美仍待评审 |
| 关闭窗口 | 主窗隐藏后相同会话继续计时，背景保留 | 关闭不等于退出 |
| 强退重开 | 对身份核验过的隔离进程 SIGKILL，再以同一路径、同一数据重开；显示暂停和未知间隔说明 | 不是系统断电或锁屏测试 |
| 强退恢复结果 | 恢复时长与检查点同为 38,000,567,041 ns；历史不增加，B 恢复可见，pending=0，背景与媒体停止 | 未证明通知恢复 |
| 开始前写失败 | 真实写入目标受阻时暂停，elapsed=0、pending=0、无背景；恢复文件后可退出 | 仅故障注入隔离目录，原 JSON 保留 |
| 记录 | 删除、撤销、失败恢复、8 条记录滚动、异常中断说明、全文提示 | 不是长期大规模数据性能结论 |
| 深浅色 | 原生 Aqua／DarkAqua 的主窗、设置、空／长列表及错误截图复核 | 未改全局外观；高对比度、降低透明度未实测 |

自动返回工作应用在部分 CUA 启动序列中出现过超时，界面显示明确回退提示；后续真实“返回当前工作”确认 A 成为前台。没有将其解释为所有启动／窗口切换入口均通过，也没有为强行抢焦点添加循环。

## 自动检查

基线记录 110（app:test）、111（app:build）通过；112 核查面板层级假设，实际层级正确，因此没有修改层级。最终命令与退出码登记在应用变更 records／evidence；按 AC 重复绑定的执行不累加为独立测试数量。

新增 Unicode 目标边界、Finish／Undo 写失败和偏好原子提交测试。`app:test` 覆盖根模块与共享日志契约；`tech:test` 覆盖探针及 Python 边界；`app:native` 增加主动查看接收键盘、待处理选择下正常退出回归；`app:effects` 保留十项真实自有效果；`app:media` 完整解码。`app:package` 增加实际应用入口的损坏数据、未来版本、无效检查点、第二实例锁和验收参数越界拒绝检查。结果以最终机器记录为准。

原生构造不是 VoiceOver／输入法通过，零音量引擎状态不是听觉通过。没有把通知检查改为成功；不执行 `hx done`。

隔离界面复现：

```sh
python3 scripts/acceptance.py start --appearance dark
python3 scripts/acceptance.py status
python3 scripts/acceptance.py stop
```

`light` 同样可用。启动器固定临时数据及夹具路径；`acceptance-state.json` 只记录该运行自己的状态，不提供驱动界面的远程控制接口。

## 证据索引

以下相对 `.validation/`，全部为合成目标和自有夹具。

- 修复前：`choice-keyboard-regression-red/`、`pending-choice-quit-regression-red/`、`quit-pending-choice-red.json`、`preferences-transaction-regression-red.log`，以及 history／settings-write-failure-before 截图。
- 真实三选：归档 `zhuanzhuli-acceptance-u9fkiem8`；`acceptance-return-choice/`、`join-paused-data.json`、`join-resumed-state.json`、`next-session-allowance.json`。
- 真实待处理退出：归档 `zhuanzhuli-acceptance-_istr4fz`；`live-pending-choice-before-quit.json`、`live-pending-choice-normal-exit.json`。
- 四主题、记录失败重试与异常恢复：归档 `zhuanzhuli-acceptance-8n6qynq7`；`entry-meadow-state.json`、`entry-snow-state.json`、`crash-recovery/result.json`、`start-write-failure-state.json`。
- 长内容最终截图：`acceptance-final-long-goal.png`、`acceptance-final-history-top.png`、`acceptance-final-history-bottom.png`；归档 `zhuanzhuli-acceptance-g0w1dkoz`。
- 最终设置：归档 `zhuanzhuli-acceptance-hyem60qg`；`settings-write-failure-after.png`、`preferences-retry-committed.json`、`acceptance-final-dark-main.png`、`acceptance-final-dark-settings.png`。

设置失败注入恢复原文件后再清理；异常／正常退出分开登记。真实用户默认 data.json 仍不存在，原始需求 SHA-256 未变。最终深色设置截图来自最终构建；较早的三选、记录、长目标截图对应的呈现代码在之后的偏好事务修正中未变。

## 剩余问题统一清单

1. **按用户决定跳过的 Focus 验证**：通知正向基线、开关对照、恢复及真实 Filter 回调；面板关闭兼容性、同名模式外部变化归属问题仍在。见 [系统 Focus 报告](系统Focus与过滤条件验证.md)。
2. **第三方与硬件组合**：Dock／Cmd+Tab、多屏／Spaces／全屏、插拔、真实办公应用及所有自动返回入口；自有夹具不能证明这些组合兼容。
3. **真实系统事件**：锁屏、合盖、睡眠和唤醒；没有为测试锁定用户电脑或要求解锁，强退恢复不能替代这些证据。
4. **无障碍与系统偏好**：真实 IME 候选、VoiceOver、完整键盘访问配置、高对比度、降低透明度、系统减少动态效果。AX 标签和外观截图只覆盖部分。
5. **声音与性能**：扬声器／耳机听觉、设备切换、循环接缝；长期和多屏指标。一次约 82 MB RSS、0.0% CPU 的进程采样只作样本，不作为阈值或续航结论。
6. **完整产品质量与交付**：最终美术评审、独立审查及应用阶段 Gate 元数据尚缺；签名公证、分发／App Store 和其他系统版本仍不在本轮发布范围。

这些是证据或能力缺口，不改为产品已完成，也不因此重做设计或扩大权限。

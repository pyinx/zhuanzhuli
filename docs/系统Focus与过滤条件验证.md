# 系统 Focus 与最小过滤条件样例：实机验证

日期：2026-10-23。环境：macOS 15.6、Apple Silicon、Xcode 26.2／Swift 6.2.3。延续用户授权：先验证原生通知，再做隔离 Focus Filter 样例；保持现有产品设计及一键开始流程。本轮未修改正式应用代码。

## 结果

| 项目 | 实际结果 | 判断边界 |
|---|---|---|
| 自有 FixtureA 通知授权与请求 | authorized，提醒／声音 enabled；通知请求提交成功 | 只证明授权及请求，不证明横幅或声音送达 |
| 原生通知正向基线 | 横幅样式、临时持久提醒样式均未观察到弹窗；声音未完成听觉验证 | 基线未建立，不能把“未出现”解释为 Focus 屏蔽 |
| 系统 Focus 开／关通知对照 | 未执行切换 | 用户明确选择保持原设置、先不切换系统 Focus；本轮不执行此项 |
| 最小 Swift 样例构建 | arm64 构建、严格本地签名核验、App Intents 元数据生成通过 | 本地 ad-hoc 签名，不是公证或分发验收 |
| 系统发现 | 系统设置→专注模式→勿扰模式→添加过滤条件，实际列出 FocusFilterProbe | 实机发现通过，无需先引入扩展目标或重写 Go 主界面 |
| 原生参数页 | 系统生成“启用测试标记”开关，默认关闭 | 配置页已观察，随后取消，未保存到已有模式 |
| `ProbeFocusFilter.current` | 实际读到 `active=false`，保存独立 readCurrent 事件 | 是本应用默认配置，不代表全局 Focus 关闭 |
| 系统 `perform()` 回调 | 0 条事件；未执行开／关触发 | 未验证，不能用构建、发现或 current 成功替代 |
| 正式应用通知适配复查 | AX 信任 true、模式列表可读、勿扰模式 false；面板关闭未确认，命令退出 3 | 仍是失败／受限结果；未切换 Focus，无待恢复变更 |

当前只证明了 **Swift 宿主内的最小 Focus Filter 可被这台 Mac 发现并读取默认配置**。它尚不能替代正式应用的一键通知联动，也没有据此完成 R06／AC5。

## 通知基线的环境限制

系统通知设置中，“当镜像或共享屏幕时允许通知”为关闭。此条件与当前自动化／捕获环境可能相关，但没有建立因果关系，也没有据此诊断系统缺陷。允许通知与请求提交成功仍不足以证明实际弹窗、声音。

未开启这项全局通知选项：它会扩大屏幕共享期间其他应用通知的可见范围，超出仅测试自有夹具的授权。没有读取其他应用通知内容、录制系统音频或申请麦克风权限。

系统 Focus 的“跨设备共享”开启。用户随后明确选择“保持原设置，先不切换系统 Focus”。据此保留原设置，本轮不执行系统模式切换、相关通知开／关对照或以切换触发的真实回调测试。这是已确定的本轮测试边界，不再列为等待授权，也不是通知能力已验收或永久取消该需求。

## 样例结构及后续接入判断

源码位于 `experiments/focus-filter/`，bundle ID 为 `local.focusprobe.filter`；不与正式应用共享数据，不自动创建或恢复会话。原生 SwiftUI 窗口提供只读查询和退出；`SetFocusFilterIntent` 只记录一个合成布尔参数。

系统回调 `perform`、只读查询 `readCurrent` 和窗口启动 `launch` 分别记录，源码没有直接调用 `perform()`。事件按 UUID 原子写入专属运行目录，记录本样例身份、时间和参数；不记录工作内容。当前运行只有 launch 与 readCurrent 两条事件。

这次可先保留宿主内 Intent 的小型实现；没有证据要求立即增加扩展、App Groups 或改变 Go 主应用架构。但仍须验证关闭样例后系统是否能执行回调、真实开启与关闭参数、重复／延迟回调及 Go 交接。发现成功不能推断后台执行已经可靠。

官方方向仍是系统 Focus → 应用配置。Apple 文档说明开启时调用过滤条件，关闭时回到默认参数；这不是创建／切换指定系统 Focus 的 API。[Focus Filters](https://developer.apple.com/documentation/appintents/focus/)、[定义过滤条件](https://developer.apple.com/documentation/appintents/defining-your-app-s-focus-filter)。既有产品一键开始、暂停／退出和用户决定是否继续的流程保持原样。

## 证据与清理

- 通知 run：`experiments/macos-capabilities/.runs/run-20261023T164522-5ba8a4ef/`，保留 `A-notification.json`、`A-notification-settings.json` 及 pre-focus 副本。
- 环境与恢复：`.validation/native-focus-environment.json`；FixtureA 从横幅临时改为提醒，随后已通过系统界面确认恢复横幅。声音开关未改。
- 样例 run：`.validation/focus-filter/20261023T165358-122002e2/`，含 build.log、两个原始事件、artifact-inspection.json、system-ui-observation.json。后者明确是实际 UI 的观察记录，非系统回调。
- 构建入口：`python3 experiments/focus-filter/build.py`。输出 `.build/focus-filter/products/Debug/FocusFilterProbe.app`；重建会新建证据目录，运行前应退出旧样例。
- 系统参数页截图已在本次 CUA 输出显示，未归档成本地图片，不伪造 screenshot attachment。
- 已在夹具 UI 请求清理本次通知；没有独立确认通知中心为空，因此只记录清理请求。夹具及样例均正常退出，`ps` 核对两个原 PID 均不存在。
- 添加过滤条件页面已取消，原勿扰模式仍显示“无专注模式过滤条件”。未修改定时、跨设备共享、全局音量、真实用户会话或正式应用偏好。

## 下一步验收条件

以下涉及切换的步骤仅保留为未来验收条件，本轮不执行。

1. 建立至少一次真实弹窗／声音的正向基线；若仍受环境抑制，先停止对照，保留未验证状态。
2. 使用可撤销的自有测试 Focus，比较关闭→开启→关闭的标准通知弹窗与声音，不把授权值当效果。
3. 在同一测试 Focus 添加 `active=true` 过滤条件，实际取得系统 `perform(true)` 与关闭后的 `perform(false)`；再验证样例未运行时的回调。
4. 回调可靠后才考虑最小 Go 状态交接；重复／延迟事件不能新建会话、覆盖手动暂停或误判任务完成。应用发起系统 Focus 开关的缺口单独保留。

以上未完成项没有转为范围豁免。完整应用仍处于 implementing，既有控制中心关闭兼容性、同名模式外部变化的归属判断及独立审查缺口保留。

本轮末次 `hx check 2026-10-23-focus-application` 退出 1：13 项阻断，包含旧应用记录与当前整树版本不匹配、通知检查退出 3、应用阶段 Gate 元数据及独立审查缺失。0/7 表示当前版本没有满足 harness 的完整证据绑定，不表示过去测试全部失败；此次独立样例的成功也不替代应用验收。未执行 `hx done`。

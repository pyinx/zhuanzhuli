# macOS 核心能力探针

本目录是独立技术验证工程，不是完整产品。执行边界来自已确认的 `.harness/changes/2026-10-22-focus-technical-validation/spec.md`；产品与 v0.2 设计保持不变。

固定 Go 1.27.3、MyGo v0.6.2、purego v0.11.1，目标 darwin/arm64、CGO_ENABLED=0。开发者需要 Go、Python 3 和系统工具。没有 Bun、前端工程、数据库、额外服务、全局工具链修改或最终用户安装要求；本探针构建不证明产品分发已通过。

## 命令

从项目根目录执行；harness 命令键见 `.harness/project.md`。

```sh
python3 experiments/macos-capabilities/tech.py preflight
python3 experiments/macos-capabilities/tech.py build
python3 experiments/macos-capabilities/tech.py test
python3 experiments/macos-capabilities/tech.py probe --suite approved --choice-diagnostic
python3 experiments/macos-capabilities/tech.py shot --own-windows --run latest
python3 experiments/macos-capabilities/tech.py check --run latest
python3 experiments/macos-capabilities/tech.py stop --run latest
```

build 下载固定 Go 模块到本目录 `.cache` 并构建三个本地 bundle 到 `.build/bundles`，不启动窗口，不访问证书、不执行额外 codesign、公证或安装器。不要在有测试进程运行时重新构建。`go.sum` 应保留，不跟随浮动依赖、不用本地参考目录 replace。

2026-11-06 目录瘦身再次移除了可重建的 `.build/`、`.cache/build/` 与 `.cache/mod/`；下次 build／test 会按固定 `go.mod`／`go.sum` 重新下载并生成，因此首次运行需要网络和更多时间。它们均被忽略，不属于项目资产。保留 `.runs/` 验证记录；实验源码仍用于现有系统回归，未按“过时实验”整目录删除。

probe 经 Launch Services 启动 FixtureA／FixtureB／Host，验证身份后只对 B 发一次隐藏请求，跨事件循环观察实际结果，等待两个夹具后台计数增长，恢复仍拥有的改动。随后建立新 WebView 对照页并返回 Native UI 主窗，供人工操作。自动采样不会点击通知按钮、选择授权、切换系统设置或触发锁屏／睡眠。

返回码：0 完整通过；1 故障；2 无效输入；3 有证据的受限结果；4 待观察／缺证。harness 仅将 0 视为通过；probe 返回 4 时窗口可能仍运行，表示人工项目尚未完成，不是测试程序已退出。

## 人工操作与停止

1. 主窗点击“开始测试”后，明确选择部分保护才能进入。该选择只用于测试，通知保护仍未验证。
2. “跳过验证入场”开始有效计时，暂停／继续／结束均共用一个控制器。首次激活范围外 B 时呈现一次独立原生选择面板，重复激活不重复弹出；关闭选择面板不自动选项，可从菜单栏“查看待处理选择”重开。切到 WebView 后可比较输入、键盘、同一会话和计时；中文字符串注入不是输入法候选验证。
3. 只有夹具按钮会发送合成通知。首次可能有系统授权，由用户选择；错误 `mygo: the user does not allow notifications` 是拒绝结果，不等于产品已屏蔽通知。
4. 锁屏、睡眠、系统外观和 VoiceOver 由用户在受控步骤中主动触发。不要通过脚本模拟成真实系统事件。声音只观察，不录音。
5. 结束时点击宿主“停止并清理测试程序”或 Cmd+Q。成功清理后，宿主写入带本次 token 的停止请求，两个夹具自行退出；普通关闭主窗保留进程，可从菜单栏或 Dock 重开。

若出现 `restore-failed`，保留 `.runs/<runID>/journal.json`，不要删除日志或强行开始下一轮。未知 pending 操作不能仅凭“现在可见”宣称已清理；实例改变不能自动恢复。临时测试夹具可由用户分别正常退出，再记录清理结果。程序不终止工作应用，不尝试隐藏其他应用，也不自动清理旧运行目录。

## 证据与边界

每轮 `.runs/<runID>` 保存 manifest、事件、journal、手动观察、六渠道矩阵、自动采样、报告、单窗口截图及进程日志。只写本地，不采集窗口标题、工作内容或剪贴板。日志中的会话目标被移除；请仅在窗口输入合成文字。

截图先核对既有捕获权限和窗口归属，只接受当前 MyGo 主窗口与必要选择窗口的明确句柄；无法采集时失败／保留未测，不申请权限、不回退全桌面。截图是独立探针窗口，不能替代旧 HTML 原型证据。

MyGo 已有窗口、菜单栏、Power、通知和 UI 测试能力；本目录只补产品缺失的身份边界、跨应用观察、归属日志、控制器和证据驱动。Go 状态／日志测试与真实 macOS 采样分开。没有测试能证明全面通知屏蔽、所有系统版本、第三方应用兼容或 App Store 审核通过。

源码结构：`internal/session` 为唯一会话归属，`internal/journal` 为改动归属与原子日志，`internal/macos` 通过公开系统框架和 purego 操作限定夹具，`internal/probe` 处理单次请求后的延迟观察，`internal/fixture` 校验运行与握手，`cmd/host` 为两种技术 UI 和执行宿主，`cmd/fixture` 为两个独立实例。


`--choice-diagnostic` 用脚本建立合成会话，三次将前台交给已验证的 Fixture B，测量真实 NSWorkspace 事件及自有面板的可见／当前 Space／遮挡状态；保留最后面板供代理自动操作。它不是人从 Dock 点击的证据。失败返回 1，三次可见也不把剩余通知等未知项判绿。计时刷新使用独立 Tick，不占用户命令去重额度。

`stop` 发送当前 token 的 host-stop.json 请求，由宿主清理后写夹具停止请求，等待全部进程退出；不直接强杀。token、符号链接、文件类型和大小有负向校验。技术窗口不需要用户逐项陪测；现阶段仍未证明的物理系统／权限项目直接记录为未测，不反复要求用户操作。
# 后续应用集成说明（2026-10-23）

完整应用开发已另行获用户授权，入口在根目录 cmd/focus。探针共享日志与异步观察已提升到根模块 pkg/journal、pkg/observe，通过本地模块引用复用；MyGo 仍固定 v0.6.2，禁止替换该框架。

只读通知审计命令为 python3 tech.py notifications：分别读取三个应用自己的设置，不申请权限、不发送通知、不切换 Focus；退出码 3 是受限，不是屏蔽通过。MyGo 错误不能证明用户点击拒绝，见根目录 docs/通知屏蔽可行性.md。

--restoration-diagnostic 覆盖原本隐藏、重复清理、模拟外部改变和本次加入后的暂停恢复。正式应用的系统效果由根目录 scripts/effects.py 针对两自有夹具验证；不能替代界面点击、第三方应用或全面通知验证。


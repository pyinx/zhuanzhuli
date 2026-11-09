# 归径

macOS 桌面专注应用，沿用已确认的 v0.2 产品设计。Go 1.27.3＋MyGo v0.6.2，原生 AppKit 控件、系统菜单与本地媒体。当前是开发版本，**尚未通过完整验收，通知联动仍为实验能力**。首版通知边界已获确认：系统 Focus 可控制的标准通知，明确关键警报、部分系统／来电及自绘提醒例外。

2026-10-24 最新开发进展：[原生界面优化与开发收口](docs/原生界面优化与开发收口.md)。原生时长分段、设置分组、自动焦点链与直接工作交接已接入；D13 通知收尾提供保留／明确关闭／稍后核对。桌面锁定及“不切换系统 Focus”的边界仍使最终实机检查缺证，不将开发包视为正式验收通过。

## 构建与运行

开发机执行：

需要固定的 Go 工具链、Python 3 和 Pillow；媒体校验另需 ffmpeg／ffprobe。本地合成工作音景使用 NumPy。

    python3 scripts/build.py
    open .build/归径.app

构建出的应用不依赖使用者安装 Go 或 Python。当前候选为 0.4.0／build 19，构建 macOS 15 及以上的通用应用（Apple 芯片 arm64、Intel x86_64），使用本地 ad-hoc 签名，尚未公证。依赖版本固定在 go.mod／go.sum。场景资源由 assets/release-media.json 唯一指定：六场景的入场短片、工作背景与音乐、四种自然录音和入场引导。构建缺资源直接失败；图标从 `assets/artwork/guijing-icon.svg` 生成透明圆角 ICNS。

首次在设置中添加允许应用，再填写目标、时长，明确选择部分保护后开始。其他普通应用会被收起并保留后台运行，Finder 和必要系统交互不拦截。菜单栏或 Dock 可以返回控制窗口，关闭窗口不结束会话。暂停或退出恢复本应用拥有的改动。设置中的恢复入口可以处理遗留状态；未知归属不会自动替用户决定。

数据默认位于 ~/Library/Application Support/Zhuanzhuli（以 Go 返回的 macOS 配置目录为准），使用本地 JSON，不读取工作内容。开发验证使用独立的 --data-dir，避免混入实际记录。

沉浸模式会在倒数前检查辅助功能权限。若系统里的「归径」已开启却仍被拒绝，在权限面板点「在 Finder 中显示」核对当前副本；在系统设置移除旧条目后，重新添加该副本并开启。回到权限面板会自动重查，计时不会自动开始或恢复。也可仅本次改用普通专注。

本版通过系统进程出生时间、PID、应用 ID 和路径共同核对实例，不再强求每款应用都有 AppKit launchDate。恢复日志第一次写入新身份前备份并升级为 v2；旧待恢复项必须先恢复或明确核对。旧应用会拒绝 v2。回退前先用新版结束专注并清理待处理项、退出应用，再显式执行 `.build/归径.app/Contents/MacOS/Focus --data-dir '<实际数据目录>' --downgrade-journal`。有运行中的实例或未解决改动时拒绝转换，保留原文件与备份。历史／偏好仍为 v1，旧记录来源未知时保留。

工作中只显示右上角透明、鼠标穿透的计时；控制通过菜单栏或 Dock 主动打开。暂停恢复先准备环境再计时，不重播入场。到点在计时处短暂提示一次。入场声音遵守环境声音与音量设置，人声独立开关；退出入场会停止其播放。diagnostics.jsonl 仅存阶段、错误代码、版本及待恢复计数，64 KiB 后轮换，最多保留当前与上一份，不记录目标、文档内容或自动上传。

通知设置可保存专用模式名称并开启实验性联动，默认关闭。需先在系统中建立同名 Focus、关闭放行配置并由用户授予本应用辅助功能权限；应用不会自动授予权限。开始／恢复接入模式切换和核验，暂停／退出尝试恢复自有改动；原有模式保留，重启后的不明改动需要显式核对。此前已确认原路径进程的辅助功能授权；重建后仍需以实际进程核对。控制中心适配及真实通知弹窗／声音未验收，不能据此承诺保护或可靠恢复。用户本轮要求保留跨设备共享原设置、不切换系统 Focus，相关效果验证跳过。

## 集中验证

    python3 scripts/verify.py tests
    python3 scripts/verify.py media
    python3 scripts/verify.py package
    python3 scripts/verify.py native
    python3 scripts/verify.py notifications

native 默认分别运行两个架构的应用自检和 AppKit 回归；在 Apple 芯片上验证 Intel 架构需要 Rosetta。只验证单个架构可加 `--arch arm64` 或 `--arch x86_64`。它验证真实 AppKit 控件和窗口能构造，不能替代点击、输入法、VoiceOver、深浅色或截图验收。notifications 在原路径应用、独立空数据中检查既有权限和生产控制中心适配，不切换系统模式或申请权限；能力未验证时退出 3。完整进展和剩余问题见 docs/应用开发与验证.md；通知依据见 docs/通知屏蔽可行性.md。

本地开发包默认使用绑定应用标识、Info.plist 和资源的 ad-hoc 签名，并通过 codesign 严格检查。它的签名身份随构建内容变化；macOS 可能需要移除旧的辅助功能条目，再添加当前版本。已安装有效 Developer ID Application 证书时，可用 `FOCUS_CODESIGN_IDENTITY='Developer ID Application: …' python3 scripts/build.py` 以同一证书签名，保持应用标识与签名身份稳定。按架构打包会保留原始签名；首次从 ad-hoc 切换仍需重新授权。这不代替公证。

整体功能验收见 docs/整体功能验收.md。开发者可用 python3 scripts/acceptance.py start 建立隔离原生交互场景，测试完成用 stop 清理。--fixture-ui 仅允许本轮三个具有独立路径／标识的自有夹具的应用效果，跳过系统 Focus；不能用它证明通知能力或第三方应用兼容性。测试包启动器固定临时数据路径，避免工具重新打开时落入真实数据目录。

正式交付只能运行 `python3 scripts/release.py`（或 hx run app:release）。它核对当前签名候选、源码指纹、全部 AC1–AC13、真实通知效果／完整时长／声音／辅助功能／显示器证据和两轴独立审查，缺失时返回 4，不创建 ZIP。验收清单位于 .validation/release-evidence.json，每项必须给出实际方法、通过状态和带 SHA-256 的证据文件；不能用开发测试代替实机效果。通过后 ZIP 名含版本和构建指纹，并解包逐文件比对。旧 .build/专注力-macOS-arm64.zip 属于历史产物，不能作为当前候选交付。

用户自用的安装包先执行 `python3 scripts/build.py`。运行 `python3 scripts/release.py --local-dmg --arch arm64 --arch x86_64` 可在 `.build` 生成两个按架构区分的 DMG；不带 `--arch` 仍可生成通用 DMG，`--local-package` 生成通用 ZIP。DMG 卷内含应用与「应用程序」快捷方式，并附 SHA-256 校验和与安装说明。打包会只读挂载映像，核对包内容、架构和签名，并运行 `hdiutil verify`。本地包与正式发布门禁独立。

本轮非 Focus 开发与自主验收见 [非 Focus 整体验收](docs/非Focus整体验收.md)：修正待处理面板阻止退出、主动查看键盘入口、记录与偏好写失败反馈和长目标省略。可用 `python3 scripts/acceptance.py start --appearance dark` 或 `light` 进行仅限隔离进程的原生外观测试；不更改系统设置，普通启动拒绝该参数。正常退出必须确认事件循环返回，构造检查包含待处理面板退出回归。

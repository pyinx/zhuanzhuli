# macOS 原生 UI／UX 复核

- 日期：2026-10-22
- 对象：已确认的产品需求、本轮设计规格和设计范围摘要
- 初次复核阶段：Gate 1 确认前；当时尚无具体界面稿或原型
- 当前进展：Gate 1 已确认；[候选界面与原型](产品交互设计.md) 已编写，Gate 2 已确认，验证缺口保留；没有已运行的应用，也没有浏览器截图或原生实机验收
- 结论性质：对设计要求的复核与补充，不是实机 UI 验收，也不是独立设计审查通过证明
- 产品基线：[产品需求文档](产品需求文档.md)
- 本轮权威规格：[spec](../.harness/changes/2026-10-22-focus-design/spec.md)

## 1. 结论

产品应优先成为符合 macOS 日常操作习惯的桌面应用。苹果风格要覆盖窗口、菜单、输入、焦点、材质、系统偏好和无障碍行为，不能仅用圆角、透明背景和大留白来定义。

以下缺口与建议保留初次复核时的背景。用户随后确认了更新后的范围与 9 项验收标准；具体方案的 NX01–NX12 映射见产品交互设计说明第 7 节。映射存在不等于原生行为通过，浏览器读取受安全策略限制，尚缺可视与交互证据。

目前已经确定产品流程与低干扰原则，但尚未写清上述原生行为。应在本轮设计验收中补入这些要求，再用具体方案与可评审原型呈现。当前没有可供逐像素审查的产品 UI；浏览器中打开的是 harness 的范围说明页，其排版不代表产品界面。

用户在本轮明确确认的是 macOS 桌面定位和苹果风格。下面的细化标准属于此次复核建议，纳入更新后的范围摘要供确认；具体布局、窗口形态和行为取舍仍需在 Gate 1 后设计，并在 Gate 2 确认，不将建议冒充用户已经选择的方案。

## 2. 技能适用方式

本轮采用 native-feel 技能的三个原则：

- **T3**：“Adopt the platform; don't compete with it.” 对应窗口、菜单、输入和系统材质的原生一致性。[技能：哲学 §3](/Users/zyb/.agents/skills/native-feel-cross-platform-desktop/references/01-philosophy.md#3-adopt-the-platform-dont-compete-with-it)
- **T4**：“Performance is a property of perception.” 对应输入不丢失、打开面板不闪烁、状态变化不抢焦点等可感知要求；当前不引入未经确认的毫秒、内存或 CPU 指标。[技能：哲学 §4](/Users/zyb/.agents/skills/native-feel-cross-platform-desktop/references/01-philosophy.md#4-performance-is-a-property-of-perception)
- **T7**：“Identity is muscle memory.” 用于保护用户已经熟悉的 Mac 快捷键和操作语义；本项目不是旧应用重写，不照搬 Raycast 的具体快捷键。[技能：哲学 §7](/Users/zyb/.agents/skills/native-feel-cross-platform-desktop/references/01-philosophy.md#7-identity-is-muscle-memory)

已阅读技能的决策树。当前只首先交付 macOS，且没有插件生态或复杂跨平台后端的已确认要求，因此没有依据照搬 Swift＋React＋Node＋Rust 四层架构。继续保留 Go＋MyGo 与必要 macOS 适配的技术基线，本轮不做架构选型。[技能：决策树，问题 1 与 3](/Users/zyb/.agents/skills/native-feel-cross-platform-desktop/checklists/decision-tree.md)

## 3. 需要补齐的设计要求

这些是规划中的缺口，不能表述为已经观察到的应用故障。

| 编号 | 优先级与当前缺口 | 复核建议 | 取舍与成本 |
| --- | --- | --- | --- |
| N01 | P1：现有规格只写苹果风格，没有可观察的原生标准 | 为窗口、输入、系统外观、辅助功能和反馈定义检查项，加入 AC9 | 放弃完全自由的自定义控件外观，增加平台适配与验证工作 |
| N02 | P1：关闭窗口、关闭面板、退出专注和退出应用尚未明确区分 | 设计中分开说明这些动作及会话去向；遵循系统窗口控制，不自绘最终窗口行为 | 需要验证后台会话与窗口可见性，不能用同一“关闭”处理所有场景 |
| N03 | P1：键盘、焦点与中文输入法尚无验收项 | 明确默认焦点、键盘导航、输入法候选确认、Esc、菜单命令和焦点返回 | 需设计并验证输入边界，不能只检查鼠标点击流程 |
| N04 | P1：技能建议使用系统通知，但产品同时要求全面屏蔽通知 | 到点反馈优先在本产品状态和轻量入口中表达，保留已确认的可选一次柔声；不得仅依赖系统通知 | 放弃容易实现的普通系统通知路径，需验证反馈在保护状态下仍可达 |
| N05 | P1：动效与半透明尚未结合系统辅助偏好 | 纳入减少动态效果、降低透明度、提高对比度、完整键盘访问和 VoiceOver 的设计状态 | 必须提供静态和不透明呈现，不能把“玻璃效果完整”作为唯一设计目标 |
| N06 | P2：系统字体、强调色、浅深色和材质层级尚未明确 | 控件遵循系统语义；自然场景负责氛围，内容区优先可读性；材质按用途克制使用 | 放弃所有界面固定品牌色及整页铺满玻璃；不同系统版本外观允许存在合理差异 |
| N07 | P1：轻量入口可能被误解为只能通过菜单栏控制 | 暂停、恢复和退出必须可达；菜单栏隐藏或空间不足时，也应在设计中有可访问路径 | 需要设计控制入口的回退，Dock 与菜单栏形态在方案比较时决定，不预先锁定 |
| N08 | P2：原型效果容易被当作真实系统能力 | 原型显著标明演示身份，分别标注浏览器可演示与必须实机验证的行为 | 浏览器原型不能独立获得“已经原生”的结论，需要后续 AppKit／MyGo 环境证据 |

依据：本轮 spec 的 Scope、AC2–AC8；[技能：原生约定的 Windowing、Input、Keyboard、Materials、Motion 与 Accessibility](/Users/zyb/.agents/skills/native-feel-cross-platform-desktop/references/06-native-conventions.md)；Apple 官方资料见第 7 节。

## 4. 拟纳入 AC9 的原生体验检查项

| 检查项 | 设计阶段要交代清楚 | 应用阶段另行验证 |
| --- | --- | --- |
| NX01 窗口语义 | 区分关闭面板、关闭窗口、退出专注和退出应用；不得无意丢失当前会话 | 系统标题栏、拖拽、缩放、全屏、最小化与多屏恢复 |
| NX02 菜单与入口 | 常用动作可发现；菜单栏受遮挡或隐藏时有可访问路径；设置入口符合 Mac 习惯 | 系统菜单、设置窗口、Dock／激活行为；不自动增加全局快捷键 |
| NX03 键盘导航 | 完整键盘访问下操作有逻辑顺序、可见焦点和可理解标签；不覆盖系统惯用组合键 | 系统键盘访问设置、Tab／方向键、菜单命令和焦点回到触发控件 |
| NX04 输入法 | 输入法确认候选文字不触发开始专注；输入内容正常编辑、复制、粘贴 | 拼音候选位置、组合输入、选区、光标与快捷键冲突 |
| NX05 Esc 与关闭 | Esc 关闭最近一层临时界面或取消当前动作；不直接结束整个专注 | 面板关闭后键盘焦点恢复；不无故抢回工作应用焦点 |
| NX06 外观 | 系统字体、语义色与控件层级；浅深色均可读，主题图片不承担唯一的状态提示 | 系统外观、强调色、滚动条、屏幕缩放及动态切换 |
| NX07 材质 | 材质用于合适的控制层；历史和编辑内容优先清晰；不要求所有表面透明 | 系统支持范围、原生材质和不透明回退，不用模糊值冒充系统实现 |
| NX08 动效 | 入场可跳过；减少动态效果时有静态或简化替代；工作阶段保持安静 | 系统偏好变化、动画开关、动态背景被遮挡后的资源使用 |
| NX09 对比度 | 降低透明度与提高对比度后层级仍清楚；状态同时有文字或图形依据 | 系统辅助偏好、文字放大、深浅主题及实际对比度 |
| NX10 VoiceOver | 目标、计时、保护状态与按钮有可理解的名称；不每秒播报计时而打断工作 | 辅助功能树、焦点读出、状态变化及所有关键操作 |
| NX11 反馈 | 到点只反馈一次，输入错误就地说明；保护状态可持续查看，不堆叠网页式浮动提示 | 保护已启用时仍能看到会话反馈，声音可关闭且不反复催促 |
| NX12 指针与内容 | 列表、控件、可编辑文字和可复制内容各用合适交互；不统一套用网页手形光标 | 指针、系统编辑菜单、输入焦点；不对整窗禁用文字选择或复制 |

本表规定的是需要设计和验证的行为，不指定 SwiftUI、AppKit 或 MyGo 的具体接口。浏览器原型可用明确标识的窗口示意解释视觉，不把画出的交通灯、阴影、圆角或半透明效果称为真实系统窗口。

## 5. 与技能通用建议的校正

### 5.1 绿色窗口按钮

技能的原生约定与检查表把默认绿色按钮描述为缩放、按 Option 才全屏。苹果当前官方说明：支持全屏的窗口点击绿色按钮可进入全屏；最大化、排列等另有系统选项。因此本项目保留系统行为，不根据技能文字反向改写窗口控制。[Apple：全屏](https://support.apple.com/en-gb/guide/mac-help/mchl9c21d2be/mac)、[Apple：移动与排列窗口](https://support.apple.com/en-gb/guide/mac-help/mchlp2469/mac)

### 5.2 通知与不打扰

技能关于避免网页浮动提示、采用系统通知的建议，应结合产品目标应用。此产品的到点反馈不能仅依赖可能被全面静音的系统通知，也不能改成抢焦点的网页弹窗。具体轻量反馈形态在设计阶段比较，继续保留用户已确认的一次提醒与可关闭提示音。

### 5.3 无障碍与检查表评分

不采用技能检查表中允许无障碍项目滞后而仍宣称就绪的宽松标准。关键操作不可访问、动效偏好不被尊重或文字不可读，都需要单独解决，不用总体通过率抵消。

同样，不直接采用该表中的启动、内存、CPU、全局热键、插件、自动更新或双平台同版功能要求。它们不是当前用户确认的全部需求，性能数值仍按待验证清单处理，不能因引用技能而扩大产品范围。

### 5.4 苹果风格与材质

Apple 官方建议克制使用 Liquid Glass，系统组件和辅助偏好共同决定呈现方式。本产品不把大面积半透明作为原生感的必要条件；降低透明度时不透明界面仍是正确设计。[Apple：Materials](https://developer.apple.com/design/human-interface-guidelines/materials)、[Apple：降低透明度](https://developer.apple.com/documentation/appkit/nsworkspace/accessibilitydisplayshouldreducetransparency)

## 6. 本轮变更与仍未完成的内容

- 将用户明确的 macOS 桌面定位与苹果风格加入 spec Decisions，并细化范围与验收项 AC9。
- 将平台与风格方向写入 .harness/design.md；候选标准与已确认方向分开，具体 token 和组件不伪造。
- 更新只读范围确认页，使其明确说明原生体验要求及浏览器原型的验证边界。
- 记录长期约束到记忆页，并通过 hx memory 重建索引。
- 尚未绘制具体界面、比较窗口布局方案、完成可评审原型或进行实机输入与窗口测试。没有声称完成 native-feel 就绪审计。
- 本轮属于用户对范围摘要的补充要求，不是 Gate 1 的明确确认；更新后仍需按 hx-align 确认完整摘要，再展开具体设计。

## 7. 来源与证据等级

| 来源 | 用途 | 证据边界 |
| --- | --- | --- |
| [Apple：Designing for macOS](https://developer.apple.com/design/human-interface-guidelines/designing-for-macos/) | 多应用、可调整窗口、菜单和键盘习惯 | 设计原则，不证明本项目已实现 |
| [Apple：Materials](https://developer.apple.com/design/human-interface-guidelines/materials) | 材质用途与克制使用 | 不锁定最低系统版本或具体接口 |
| [Apple：Testing system accessibility features](https://developer.apple.com/documentation/accessibility/testing-system-accessibility-features-in-your-app) | 降低透明度、减少动态效果、对比度和键盘访问 | 需实际设计与实机测试 |
| [Apple：Use apps in full screen](https://support.apple.com/en-gb/guide/mac-help/mchl9c21d2be/mac) | 纠正绿色按钮的反向描述 | 窗口须按系统支持能力处理 |
| [Apple：Move and arrange app windows](https://support.apple.com/en-gb/guide/mac-help/mchlp2469/mac) | 系统窗口排列、缩放、最小化 | 不是应用自行重排其他窗口的授权 |
| [Apple：MenuBarExtra](https://developer.apple.com/documentation/swiftui/menubarextra) | 理解菜单栏可提供后台常用动作 | 仅参考平台模式，不采用 SwiftUI 架构或替代 Go＋MyGo |
| [技能：原生约定](/Users/zyb/.agents/skills/native-feel-cross-platform-desktop/references/06-native-conventions.md) | 形成适用的交互检查项 | 属于建议，存在需校正内容，不作无条件权威 |
| [技能：决策树](/Users/zyb/.agents/skills/native-feel-cross-platform-desktop/checklists/decision-tree.md) | 排除不必要的架构照搬 | 没有重新开展技术架构设计 |
| [技能：就绪检查表](/Users/zyb/.agents/skills/native-feel-cross-platform-desktop/checklists/ship-readiness.md) | 抽取适用项目，并排除范围外事项 | 目前无应用可运行，所有实机项目尚未判为通过 |

官方资料核对日期为 2026-10-22。部分 HIG 页面通过搜索结果正文查阅；页面要求 JavaScript，提供的 Markdown 链接被工具以内容类型不支持拒绝读取。未将仅打开了占位页面的内容冒充全文实读，也未声称应用行为已由这些资料验证。

# 最小 macOS Focus Filter 验证

这是独立技术样例，不接入正式应用，不改变产品界面、一键开始或会话核心。系统 Focus 的开关和配置只通过原生界面测试；不引入快捷指令、私有 Focus API 或按键注入到正式应用。

目标依次为：原生标准通知基线及 Focus 对照；样例构建和系统发现；真实 Focus 开启／关闭的 `SetFocusFilterIntent.perform` 回调。构建成功、`current` 读取或配置界面出现，都不能单独算回调验证通过。

样例采用 macOS SwiftUI 宿主内的 App Intent，先验证最小形态。每个事件独立原子保存到构建时指定的隔离目录，只保存合成布尔标记、事件种类、时间和本样例身份。`readCurrent` 是只读诊断，与系统触发的 `perform` 分开，不直接调用 `perform()` 来冒充系统回调。不与正式应用交换数据、不自动开始／结束会话。

构建使用本机 Xcode，输出 `.build/focus-filter/`，运行记录位于 `.validation/focus-filter/`。本地 ad-hoc 签名不代表发布签名或系统发现已通过；不需要新增系统隐私权限。

```sh
python3 experiments/focus-filter/build.py
```

依据：[Apple Focus Filters](https://developer.apple.com/documentation/appintents/focus/)、[官方示例说明](https://developer.apple.com/documentation/appintents/defining-your-app-s-focus-filter)。该 Swift 宿主能否直接被本机系统识别、能否处理后台回调，均以实际结果为准。

2026-10-23 实测：macOS 15.6 已在系统设置中发现本样例并生成原生布尔参数页，`current` 读取默认 false 成功。真实开／关 `perform` 及后台回调仍未验证，配置已取消、样例已退出。详见 [实机报告](../../docs/系统Focus与过滤条件验证.md)。不要把这项发现验证作为正式应用通知联动通过。

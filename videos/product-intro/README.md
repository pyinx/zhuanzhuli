# 归径官网介绍视频

77 秒，1920×1080，30 fps，H.264／AAC，适合官网横版展示。以真实 macOS 应用录屏介绍目标、时长、应用选择、自然场景与声音、开始专注、暂停与继续、结束、模版和统计。内嵌中文字幕，静音观看也能理解，无配音。

官网使用以下文件：

- `guijing-intro.mp4`：主片，已设置 faststart。
- `poster.jpg`：1920×1080 封面。
- `guijing-intro.srt`：独立中文字幕；视频画面也有内嵌字幕。
- `CREDITS.md`：第三方音乐和影像署名，发布时保留或在视频附近提供链接。片尾也保留简短署名。
- `script.md`：镜头和字幕脚本。

网页可用原生 video 控件：

```html
<video controls playsinline preload="metadata" poster="poster.jpg">
  <source src="guijing-intro.mp4" type="video/mp4">
</video>
```

如作为静音循环展示，可以按官网设计添加 muted、autoplay、loop。当前仅生成本地文件，没有部署或上传。

素材与复核文件：`recordings/` 保存实际录屏，`edit/` 为剪辑清单和生成文件，`evidence/` 为来源、独立演示数据、关键帧及核验结果。官网发布无需上传这三个制作目录。

录制使用原有 0.4.0 (23) 应用的独立身份副本与独立数据目录，副本由原包派生并隔离重签，完整可执行文件哈希因签名而不同；两架构的程序节内容已独立核对一致。没有修改应用源代码、正式图标或分发资源，因此没有生成新版本或安装包。示例只选用文本编辑和专门创建的文稿，系统 Focus 联动关闭。

本片由多次真实演示录屏剪辑，不是一次无剪辑会话。准备、工作、暂停与继续取自第一轮演示；结尾的结束点击、统计和历史来自同一次补录，其实际投入为 2 分 08 秒。两轮使用相同的目标与 15 分钟配置，原始数据分别保留在 demo-data.json 与 retake-data.json。准备、入场及控制片段适度变速，剪掉等待，没有改写界面的计时数值。画面左下标明“实机演示 · 画面经剪辑”；工作画面只作整体亮度校正，没有重绘文字、控件或状态。其他窗口与系统权限提示不进入成片。详见 evidence/timeline.json 的 take、区间和速度。

演示结束后，专注会话为空，独立应用和示例编辑器均已退出。第一轮恢复账本为 38 条 released、2 条 abandoned：Pulse 的先前环境保持现状，SecureCRT 实例已结束或替换，未操作替代实例；补录会话的 20 条记录全部 released。本片不作为完整系统窗口恢复、全屏或通知联动的验收证据。

核验命令：在项目根目录运行 `hx run video:check --slug 2026-11-18-product-intro-video`，或运行 `python3 videos/product-intro/verify.py`。完整解码、编码参数、字幕时间、音量、原片哈希、真实历史与原有工作区文件哈希由命令检查；视觉与功能表述另经独立审查。核验发现根 README 在本轮期间被其他工作更新，已按前后哈希记入 `evidence/external-changes.json` 并保留；其他原有文件和应用二进制保持快照哈希。

重建需要本机 ffmpeg、Python Pillow、苹方字体，以及本项目已有音乐、图标与保留的录屏。运行 `python3 videos/product-intro/edit/render.py` 会重建当前剪辑；发布的六个文件请单独保存后再改剪辑。

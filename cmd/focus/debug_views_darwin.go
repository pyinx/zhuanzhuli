//go:build darwin

package main

import (
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"github.com/egoist/mygo"
	"time"
)

func (a *application) buildDebugSettings(root m.ID) {
	titleText(root, "调试日志", 15)
	r := row(root, 12)
	bodyText(r, "记录运行和沉浸模式的诊断信息", 13, false)
	m.Flex(r)
	var toggle m.ID
	status := m.ID(0)
	toggle = m.Switch(r, "记录调试日志", a.store.Data.Preferences.DebugLogging, func(on bool) {
		next := a.store.Data.Preferences
		next.DebugLogging = on
		if err := a.store.SetPreferences(next); err != nil {
			m.SetChecked(toggle, a.store.Data.Preferences.DebugLogging)
			showMessage(status, preferenceSaveError)
			return
		}
		if a.debug != nil {
			a.debug.setEnabled(on)
		}
		showMessage(status, "")
	})
	m.Arrange(r, toggle, false)
	bodyText(root, "日志仅保存在本机，可能包含所选应用的标识。关闭记录后，仍可查看和导出近期日志。", 12, true)
	status = message(root)
	action(root, "查看日志…", a.openDebugLog)
}

func (a *application) openDebugLog() {
	a.showConfigurationDetails("调试日志", func(root m.ID) {
		titleText(root, "调试日志", 20)
		bodyText(root, "导出当前显示的记录。日志可能包含所选应用的标识。", 12, true)
		tools := row(root, 8)
		levels := []debugLevel{"", debugInfo, debugWarn, debugError}
		var records []debugRecord
		var readFailed, writeFailed, exporting bool
		var level debugLevel
		var visible string
		var view, status, export m.ID
		show := func() {
			var count int
			visible, count = formatDebugRecords(records, level)
			m.Send(view, "setString:", m.NS(visible))
			m.Enabled(export, count > 0 && !exporting && !readFailed)
			switch {
			case readFailed:
				showMessage(status, "无法读取本地日志，请检查本地数据目录后重试。")
			case writeFailed:
				showMessage(status, "有日志未能写入，请检查磁盘和本地数据目录。")
			case len(records) == 0:
				showMessage(status, "暂无调试日志。开启记录并复现问题后，点「刷新」。")
			case count == 0:
				showMessage(status, "没有这个级别的日志。")
			default:
				showMessage(status, "")
			}
		}
		refresh := func() {
			if a.debug != nil {
				var err error
				records, writeFailed, err = a.debug.snapshot()
				readFailed = err != nil
			}
			show()
		}
		filter := m.Popup(tools, []string{"全部", "INFO", "WARN", "ERROR"}, 0, m.R(0, 0, 100, 28), func(i int) {
			if i >= 0 && i < len(levels) {
				level = levels[i]
				show()
			}
		})
		m.Send(filter, "setAccessibilityLabel:", m.NS("日志级别"))
		m.Arrange(tools, filter, false)
		action(tools, "刷新", refresh)
		m.Flex(tools)
		parent := a.detail
		export = action(tools, "导出…", func() {
			if exporting || visible == "" || readFailed {
				return
			}
			exporting = true
			m.Enabled(export, false)
			snapshot := visible
			go func() {
				path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
					Parent: parent, Title: "导出调试日志",
					DefaultPath: appDisplayName + "-调试日志-" + time.Now().Format("20060102-150405") + ".log",
					Filters:     []mygo.FileFilter{{Name: "日志", Extensions: []string{"log"}}},
				})
				if err == nil && path != "" {
					err = exportDebugLog(a.dir, path, snapshot)
				}
				mygo.RunOnMain(func() {
					if a.detail != parent {
						return
					}
					exporting = false
					m.Enabled(export, visible != "" && !readFailed)
					switch {
					case err != nil:
						showMessage(status, "无法导出日志。请选择应用数据目录以外的可写位置后重试。")
					case path != "":
						showMessage(status, "已导出当前筛选的日志。")
					}
				})
			}()
		})
		status = message(root)
		scroll := m.View("NSScrollView", m.R(0, 0, 495, 290))
		m.Send(scroll, "setHasVerticalScroller:", true)
		m.Send(scroll, "setAutohidesScrollers:", true)
		m.Send(scroll, "setBorderType:", int64(1))
		view = m.View("NSTextView", m.R(0, 0, 495, 290))
		m.Send(view, "setEditable:", false)
		m.Send(view, "setSelectable:", true)
		m.Send(view, "setRichText:", false)
		m.Send(view, "setAccessibilityLabel:", m.NS("调试日志内容"))
		m.Send(view, "setFont:", m.Send(m.Class("NSFont"), "monospacedSystemFontOfSize:weight:", float64(11), float64(0)))
		m.Send(view, "setVerticallyResizable:", true)
		m.Send(view, "setAutoresizingMask:", uint64(2)) // NSViewWidthSizable
		m.Send(m.Send(view, "textContainer"), "setWidthTracksTextView:", true)
		m.Send(scroll, "setDocumentView:", view)
		m.Release(view)
		m.Add(root, scroll)
		m.Release(scroll)
		m.Arrange(root, scroll, true)
		m.Fixed(scroll, "height", 290)
		refresh()
	})
}

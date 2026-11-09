//go:build darwin

package main

import (
	"fmt"
	"focus.local/zhuanzhuli/internal/media"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"github.com/egoist/mygo"
	"strings"
	"time"
)

func (a *application) openHistory() { a.showPage(pageHistory) }
func (a *application) dismissHistoryDates() {
	a.historyFromPicker.Dismiss()
	a.historyToPicker.Dismiss()
	a.report.fromPicker.Dismiss()
	a.report.toPicker.Dismiss()
}
func (a *application) renderHistory() {
	if a.historyRoot == 0 {
		root := a.pageRoot(pageHistory)
		panes, _ := a.buildSectionTabs(root, []string{"统计", "历史"}, a.historySection, func(i int) {
			a.dismissHistoryDates()
			a.historySection = i
			a.layoutFocusReport()
		})
		a.historyReportRoot, a.historyRoot = panes[0], panes[1]
		scroll, content := m.ScrollStack(a.historyReportRoot)
		m.Edges(scroll, a.historyReportRoot, 0)
		a.buildFocusReport(content)
	}
	if a.historyFromPicker != nil {
		a.historyFromPicker.Close()
	}
	if a.historyToPicker != nil {
		a.historyToPicker.Close()
	}
	position := m.Point{}
	if a.historyScroll != 0 {
		b := m.Bounds(m.Send(a.historyScroll, "contentView"))
		position = m.Point{X: b.X, Y: b.Y}
	}
	if a.historyList != nil {
		a.historyList.Close()
		a.historyList = nil
	}
	a.pageResponders[pageHistory] = 0
	m.ClearContent(a.historyRoot)
	header := m.Stack(a.historyRoot, true, 8)
	m.Equal(header, "leadingAnchor", a.historyRoot, "leadingAnchor", 24)
	m.Equal(header, "trailingAnchor", a.historyRoot, "trailingAnchor", -24)
	m.Equal(header, "topAnchor", a.historyRoot, "topAnchor", 20)
	titleText(header, "专注记录", 20)
	a.historySearch = field(header, a.historyQuery, "搜索专注目标名称", "搜索专注目标名称")
	dates := row(header, 8)
	a.historyFromPicker = m.NewDateFilter(dates, "开始日期", a.historyFrom, func(v string) { a.historyFrom = v; a.historyPage = 0; a.refreshHistoryResults() })
	a.historyToPicker = m.NewDateFilter(dates, "结束日期", a.historyTo, func(v string) { a.historyTo = v; a.historyPage = 0; a.refreshHistoryResults() })
	a.historyFromField = a.historyFromPicker.Button
	a.historyToField = a.historyToPicker.Button
	action(dates, "清空筛选", func() {
		a.historyQuery = ""
		a.historyFrom = ""
		a.historyTo = ""
		a.historyPage = 0
		a.renderHistory()
	})
	m.Flex(dates)
	footer := m.Stack(a.historyRoot, true, 8)
	m.Equal(footer, "leadingAnchor", a.historyRoot, "leadingAnchor", 24)
	m.Equal(footer, "trailingAnchor", a.historyRoot, "trailingAnchor", -24)
	m.Equal(footer, "bottomAnchor", a.historyRoot, "bottomAnchor", -20)
	a.historyNotice = message(footer)
	pager := row(footer, 8)
	a.historyPrev = action(pager, "上一页", func() { a.historyPage--; a.refreshHistoryResults() })
	a.historyPageCaption = bodyText(pager, "", 12, true)
	a.historyNext = action(pager, "下一页", func() { a.historyPage++; a.refreshHistoryResults() })
	m.Flex(pager)
	bottom := row(footer, 8)
	undo := action(bottom, "撤销上次删除", a.undoHistoryDeletion)
	a.historyUndo = undo
	m.Enabled(undo, len(a.undo) > 0)
	m.Hidden(bottom, len(a.undo) == 0)
	if len(a.undo) > 0 {
		bodyText(bottom, "⌘Z · 本次打开应用期间可撤销", 12, true)
	}
	m.Flex(bottom)
	a.historyList = m.NewRecordList(a.historyRoot, func() int { return len(a.historyFiltered) }, func(i int) m.RecordRow {
		v := a.historyFiltered[i]
		detail := v.Started.Local().Format("2006-01-02 15:04")
		if v.Interrupted || v.Phase == "interrupted" {
			detail += " · 曾异常中断"
		}
		return m.RecordRow{ID: v.ID, Title: v.Goal, Detail: detail, Duration: spent(v.Elapsed)}
	}, a.reuseHistoryRecord, a.deleteHistoryRecord)
	a.historyList.SetInspectHandler(a.openHistoryDetails)
	list := a.historyList.View
	a.historyScroll = list
	m.Equal(list, "leadingAnchor", a.historyRoot, "leadingAnchor", 16)
	m.Equal(list, "trailingAnchor", a.historyRoot, "trailingAnchor", -16)
	m.Equal(list, "topAnchor", header, "bottomAnchor", 16)
	m.Equal(list, "bottomAnchor", footer, "topAnchor", -12)
	a.historyEmpty = bodyText(header, "", 13, true)
	m.ObserveText(a.historySearch, func() {
		a.historyQuery = m.Value(a.historySearch)
		a.historyPage = 0
		a.refreshHistoryResults()
	})
	a.refreshHistoryResults()
	a.updateFocusReport()

	m.RestoreScroll(list, position)
	m.KeyLoop(a.main.NativeHandle())
}

func (a *application) reuseHistoryRecord(id string) {
	for _, v := range a.store.Data.History {
		if v.ID != id {
			continue
		}
		if session.Exists(a.controller.Snapshot().Phase) {
			a.openMain()
			return
		}
		a.draftGoal = v.Goal
		a.openMain()
		m.Text(a.goal, v.Goal)
		m.Send(m.ID(a.main.NativeHandle()), "makeFirstResponder:", a.goal)
		return
	}
}

func (a *application) deleteHistoryRecord(id string) {
	record, index, err := a.store.Delete(id)
	if err != nil {
		showMessage(a.historyNotice, "无法删除记录。检查磁盘和本地数据目录后重试。")
		return
	}
	a.undo = append(a.undo, deleted{record, index})
	a.refreshReadySummary()
	if a.historyRoot == 0 {
		return
	}
	a.renderHistory()
	if len(a.store.Data.History) > 0 {
		if index >= len(a.store.Data.History) {
			index = len(a.store.Data.History) - 1
		}
		a.focusHistoryRecord(a.store.Data.History[index].ID)
	} else if a.historyUndo != 0 {
		if a.mainPage == pageHistory {
			m.Send(m.ID(a.main.NativeHandle()), "makeFirstResponder:", a.historyUndo)
		}
	}
}
func (a *application) undoHistoryDeletion() {
	if len(a.undo) == 0 {
		return
	}
	d := a.undo[len(a.undo)-1]
	if err := a.store.Undo(d.record, d.index); err != nil {
		showMessage(a.historyNotice, "无法恢复记录。检查磁盘和本地数据目录后重试。")
		return
	}
	a.undo = a.undo[:len(a.undo)-1]
	a.refreshReadySummary()
	if a.historyRoot != 0 {
		a.renderHistory()
		a.focusHistoryRecord(d.record.ID)
	}
}
func (a *application) focusHistoryRecord(id string) {
	if a.historyList == nil || a.historyRoot == 0 {
		return
	}
	for i, v := range a.historyFiltered {
		if v.ID == id {
			a.historyList.Select(i)
			if a.mainPage == pageHistory {
				m.Send(m.ID(a.main.NativeHandle()), "makeFirstResponder:", a.historyList.Table)
			}
			return
		}
	}
}

func spent(d time.Duration) string {
	n := int(d / time.Second)
	if n < 0 {
		n = 0
	}
	if n >= 3600 {
		return fmt.Sprintf("%d 小时 %d 分", n/3600, n%3600/60)
	}
	return fmt.Sprintf("%d 分 %02d 秒", n/60, n%60)
}

func (a *application) refreshHistoryResults() {
	page, err := store.QueryHistory(a.store.Data.History, a.historyQuery, a.historyFrom, a.historyTo, a.historyPage, 20, time.Local)
	if err != nil {
		showMessage(a.historyNotice, err.Error())
		a.historyFiltered = nil
		a.historyList.Reload()
		m.Hidden(a.historyList.View, true)
		m.Text(a.historyEmpty, "筛选尚未生效，请调整日期范围或清空筛选。")
		m.Hidden(a.historyEmpty, false)
		m.Text(a.historyPageCaption, "筛选尚未生效")
		m.Enabled(a.historyPrev, false)
		m.Enabled(a.historyNext, false)
		return
	}
	showMessage(a.historyNotice, "")
	a.historyPage = page.Page
	a.historyFiltered = page.Records
	a.historyList.Reload()
	m.Enabled(a.historyPrev, page.Page > 0)
	m.Enabled(a.historyNext, page.Page+1 < page.Pages)
	text := "0 条记录"
	if page.Total > 0 {
		text = fmt.Sprintf("第 %d / %d 页 · %d 条记录", page.Page+1, page.Pages, page.Total)
	}
	m.Text(a.historyPageCaption, text)
	empty := "没有匹配的记录，试试其他关键词或清空筛选。"
	if len(a.store.Data.History) == 0 {
		empty = "这里还没有记录。结束一次专注后会自动保存。"
	}
	m.Text(a.historyEmpty, empty)
	m.Hidden(a.historyEmpty, page.Total > 0)
	m.Hidden(a.historyList.View, page.Total == 0)
}
func (a *application) openHistoryDetails(id string) {
	if a.mainModal() {
		return
	}
	var record *session.State
	for _, v := range a.store.Data.History {
		if v.ID == id {
			copy := v
			record = &copy
			break
		}
	}
	if record == nil {
		return
	}
	w := window("专注配置详情", 560, 540, false)
	a.detail = w
	root := m.FixedSurface(w.NativeHandle(), 560, 540, false)
	r, footer, _ := contentFooter(root)
	titleText(r, "专注详情", 20)
	goal := bodyText(r, record.Goal, 15, false)
	m.LimitGoalLabel(goal)
	m.Send(goal, "setSelectable:", true)
	bodyText(r, record.Started.Local().Format("2006-01-02 15:04")+" · 计划 "+fmt.Sprint(record.MinutesValue())+" 分钟 · 投入 "+spent(record.Elapsed), 12, true)
	name := record.TemplateName
	if name == "" {
		name = "自定义"
	}
	if record.Config == nil {
		name = "未记录"
	}
	bodyText(r, "当次模版："+name, 13, false)
	if record.LegacyAppScope {
		bodyText(r, "旧会话应用来源未记录；列出的是原有范围。", 12, true)
	}
	names := []string{}
	if record.Config != nil {
		for _, app := range record.Config.Apps {
			names = append(names, app.Name+"（"+app.ID+"）")
		}
	} else {
		names = record.AllowedIDs()
	}
	bodyText(r, "专注应用："+strings.Join(names, "、"), 13, false)
	bodyText(r, "场景："+media.SceneName(record.SceneValue())+"（"+record.SceneValue()+"）", 13, false)
	if record.Config == nil {
		bodyText(r, "背景、声音、音量、入场、人声、到点提示：未记录", 13, true)
	} else {
		c := record.Config
		on := func(v bool) string {
			if v {
				return "开启"
			}
			return "关闭"
		}
		bodyText(r, fmt.Sprintf("背景 %s · 音乐 %s · 音量 %d%%", on(c.Background), on(c.Sound), int(c.Volume*100+.5)), 13, false)
		if c.Ambience == nil {
			bodyText(r, "白噪音：旧版混合，未独立记录", 12, true)
		} else {
			name := media.AmbienceName(c.Ambience.Mode)
			if c.Ambience.Mode == "off" {
				name = "关闭"
			}
			if c.Ambience.Mode == "auto" {
				id, _ := media.AutomaticAmbience(c.Scene)
				name = "自动 · " + media.AmbienceName(id)
			}
			bodyText(r, fmt.Sprintf("白噪音 %s · 音量 %d%%", name, int(c.Ambience.Volume*100+.5)), 13, false)
		}
		bodyText(r, fmt.Sprintf("入场 %s · 人声 %s · 到点提示 %s", on(c.Entry), on(c.Voice), on(c.ReminderSound)), 13, false)
	}
	if record.RecoveredConfig {
		bodyText(r, "这是旧会话恢复时补齐的配置，开始时完整配置未记录。", 12, true)
	}
	if len(record.TemporaryAllowed) > 0 {
		bodyText(r, "本次临时加入："+strings.Join(record.TemporaryAllowed, "、"), 13, false)
	}
	bodyText(r, "保护："+func() string {
		if record.Partial {
			return "部分保护；不代表系统通知效果已验证"
		}
		return "完整保护状态未记录"
	}(), 12, true)
	close := action(footer, "关闭", w.Close)
	m.Send(close, "setKeyEquivalent:", m.NS("\x1b"))
	handle := w.NativeHandle()
	w.OnClose(func(*mygo.CloseEvent) { m.EndSheet(handle) })
	w.OnClosed(func() {
		m.EndSheet(handle)
		m.UnbindTree(root)
		if a.detail == w {
			a.detail = nil
		}
		a.refreshNavigation()
	})
	if !m.BeginSheet(a.main.NativeHandle(), handle) {
		w.Close()
		return
	}
	a.refreshNavigation()
	m.KeyLoop(handle)
}

//go:build darwin

package main

import (
	"fmt"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type noticeKind uint8

const (
	noticeInfo noticeKind = iota
	noticeRecovery
	noticeStorage
	noticeHandoff
	noticeHandoffProgress
	noticeProtection
	noticeMedia
)

// Notice is one presentation value; its category never depends on translated text.
type Notice struct {
	Kind noticeKind
	Text string
}

type notificationDraft struct {
	profile string
	linked  bool
}
type uiState struct {
	permissionRefresh                                                                        func()
	readyProtectionCaption                                                                   m.ID
	unknownSheet                                                                             *m.AlertSheet
	appPicker                                                                                *mygo.Window
	pickerList                                                                               *m.AppList
	control, entry                                                                           *mygo.Window
	controlDirty                                                                             bool
	entryRoot, settingsRoot, historyRoot                                                     m.ID
	entryStatus                                                                              m.ID
	goalError, minuteError, appsError, appSummary, sceneSummary, sceneDescription            m.ID
	addAppButton, removeAppButton                                                            m.ID
	appList                                                                                  *m.AppList
	notificationDraft                                                                        *notificationDraft
	profileField, linkedField, draftNotice                                                   m.ID
	historyScroll                                                                            m.ID
	appChange, emptyApps, repairAppButton                                                    m.ID
	environmentPreview, environmentPreviewNotice, volumeCaption, auditionNotice, voiceDetail m.ID
	trayTemplate                                                                             bool
	audition                                                                                 m.Audio
	auditionNature                                                                           m.Audio
	auditionGeneration                                                                       uint64
	auditionTimer                                                                            *time.Timer
	historyNotice, historyUndo                                                               m.ID
	historyList                                                                              *m.RecordList
	reuseGoal                                                                                m.ID
	auditionError                                                                            string
	pauseMenu, resumeMenu, endMenu                                                           *mygo.MenuItem
	mainPage, lastSettingsPage                                                               mainPage
	shell, content                                                                           m.ID
	pages                                                                                    [pageCount]m.ID
	pageResponders                                                                           [pageCount]m.ID
	navigation                                                                               *m.NavigationList
	settingsScroll                                                                           [4]m.ID
	settingsNotices                                                                          [4]m.ID
	settingsErrors                                                                           [4]string
	pageMenus                                                                                []*mygo.MenuItem
	mainSession, popoverSession                                                              sessionControls
}

func window(title string, w, h int, panel bool) *mygo.Window {
	win := mygo.NewWindow(mygo.WindowOptions{Title: title, Width: w, Height: h, UseContentSize: true, DisableResize: true, DisableFullScreen: true, Panel: panel, Hidden: true, Content: ui.View(func(c *ui.Context) { ui.Column(c).Fill().Children(func() {}) })})
	m.KeyLoop(win.NativeHandle())
	m.ContentMinimum(win.NativeHandle(), float64(w), float64(h))
	return win
}
func resizable(w *mygo.Window, minW, minH float64) {
	n := m.ID(w.NativeHandle())
	mask := objc.Send[uint64](n, m.S("styleMask"))
	m.Send(n, "setStyleMask:", mask|8)
	// NSWindow's contentMinSize is a CGSize, passed with the existing rect ABI
	// through setContentSize: only for the default. A size binding is explicit.
	m.ContentMinimum(w.NativeHandle(), minW, minH)
}
func replaceFixedSurface(w *mygo.Window, old m.ID, width, height float64, transparent bool) m.ID {
	if old != 0 {
		m.UnbindTree(old)
		m.Send(old, "removeFromSuperview")
	}
	return m.FixedSurface(w.NativeHandle(), width, height, transparent)
}

func bodyText(p m.ID, text string, size float64, secondary bool) m.ID {
	l := m.Label(p, text, m.R(0, 0, 100, 20), size)
	m.Arrange(p, l, objc.Send[int64](p, m.S("orientation")) != 0)
	if secondary {
		m.Secondary(l)
	}
	return l
}
func titleText(p m.ID, text string, size float64) m.ID {
	l := m.Heading(p, text, m.R(0, 0, 100, 24), size)
	m.Arrange(p, l, objc.Send[int64](p, m.S("orientation")) != 0)
	return l
}
func row(p m.ID, spacing float64) m.ID {
	r := m.Stack(p, false, spacing)
	m.Arrange(p, r, true)
	return r
}
func rowAction(p m.ID, title string, fn func()) m.ID {
	b := m.Button(p, title, m.R(0, 0, 100, 28), fn)
	m.Arrange(p, b, false)
	m.Minimum(b, "height", 28)
	return b
}
func action(p m.ID, title string, fn func()) m.ID {
	b := rowAction(p, title, fn)
	m.FitButtonWidth(b)
	return b
}

func field(p m.ID, value, placeholder, ax string) m.ID {
	f := m.Field(p, value, placeholder, m.R(0, 0, 100, 30))
	m.Arrange(p, f, true)
	m.Fixed(f, "height", 30)
	m.Send(f, "setAccessibilityLabel:", m.NS(ax))
	return f
}
func message(p m.ID) m.ID { l := bodyText(p, "", 12, true); m.Hidden(l, true); return l }
func showMessage(id m.ID, text string) {
	if id == 0 {
		return
	}
	m.Text(id, text)
	m.Hidden(id, text == "")
}
func contentFooter(root m.ID) (m.ID, m.ID, m.ID) {
	footer := m.Stack(root, true, 8)
	m.Equal(footer, "leadingAnchor", root, "leadingAnchor", 24)
	m.Equal(footer, "trailingAnchor", root, "trailingAnchor", -24)
	m.Equal(footer, "bottomAnchor", root, "bottomAnchor", -20)
	scroll, content := m.ScrollStack(root)
	m.Equal(scroll, "leadingAnchor", root, "leadingAnchor", 0)
	m.Equal(scroll, "trailingAnchor", root, "trailingAnchor", 0)
	m.Equal(scroll, "topAnchor", root, "topAnchor", 0)
	m.Equal(scroll, "bottomAnchor", footer, "topAnchor", -12)
	return content, footer, scroll
}
func (a *application) openMain() {
	if a.quitReady {
		return
	}
	if a.controller.Snapshot().Phase == "entry" {
		a.openEntry()
		return
	}
	a.showPage(pageFocus)
}
func (a *application) renderMain() {
	s := a.controller.Snapshot()
	if s.Phase == "entry" {
		a.openEntry()
		return
	}
	if a.entry != nil && !session.Working(s.Phase) {
		a.video.Stop()
		a.entry.Hide()
	}
	if !a.quitReady && a.nativeReady {
		a.ensureMain()
		if a.root == 0 {
			a.renderReady()
		}
		a.refreshFocusContent()
	}
	if a.control != nil {
		if !session.Exists(s.Phase) {
			a.control.Close()
		}
	}
	a.refreshReadySummary()
	a.refresh()
}
func (a *application) renderReady() {
	a.ensureFocusDraft()
	a.pageResponders[pageFocus] = 0
	if a.root != 0 {
		a.draftGoal = m.Value(a.goal)
		a.focusForm.close()
		m.UnbindTree(a.root)
		m.Send(a.root, "removeFromSuperview")
	}
	a.root = m.Auto(m.View("NSView", m.R(0, 0, mainWidth-176, mainHeight)))
	parent := a.pageRoot(pageFocus)
	m.Add(parent, a.root)
	m.Edges(a.root, parent, 0)
	m.Release(a.root)
	r, footer, _ := contentFooter(a.root)
	titleText(r, "开始专注", 22)
	if v, ok := a.savedTemplate(a.store.Data.Preferences.QuickTemplate); ok {
		quick := row(r, 8)
		b := action(quick, fmt.Sprintf("开始固定模版 · %d 分", v.Config.Minutes), func() { a.startSavedTemplate(v.ID) })
		m.Send(b, "setToolTip:", m.NS(v.Name))
		m.Flex(quick)
	}
	titleText(r, "这次想推进什么？", 13)
	a.goal = field(r, a.draftGoal, "例如，写完方案第一版", "本次专注目标，最多 200 字")
	m.ObserveText(a.goal, func() { a.draftGoal = m.Value(a.goal); showMessage(a.goalError, "") })
	a.reuseGoal = action(r, "复用上次目标", func() {
		if len(a.store.Data.History) > 0 {
			a.reuseHistoryRecord(a.store.Data.History[0].ID)
		}
	})
	a.goalError = message(r)
	titleText(r, "使用模版", 13)
	names := []string{"自定义"}
	for _, v := range a.store.Data.Templates {
		names = append(names, v.Name)
	}
	a.templatePopup = m.Popup(r, names, a.focusTemplateIndex(), m.R(0, 0, 300, 28), a.selectFocusTemplate)
	m.Arrange(r, a.templatePopup, true)
	a.focusDirty = bodyText(r, "", 12, true)
	saves := row(r, 8)
	if a.focusDraft.sourceID != "" {
		action(saves, "开始已保存模版", func() { a.startSavedTemplate(a.focusDraft.sourceID) })
		action(saves, "更新此模版", a.saveFocusTemplate)
	}
	action(saves, "另存为模版…", a.saveFocusAsTemplate)
	m.Flex(saves)
	a.focusForm = a.buildConfigForm(r, func() *session.Config { return &a.focusDraft.config }, &a.focusDraft.duration, func() { a.quitDiscarded = false; a.refreshConfigurationDirty() }, true)
	a.appsError = a.focusForm.notice
	a.status = message(footer)
	startRow := row(footer, 12)
	m.Flex(startRow)
	a.readyProtectionCaption = bodyText(startRow, "", 12, true)
	a.primary = action(startRow, "开始", a.start)
	m.Primary(a.primary)
	a.refreshConfigurationDirty()
	a.refreshReadySummary()
	m.KeyLoop(a.main.NativeHandle())
}
func (a *application) refreshReadySummary() {
	if a.readyProtectionCaption != 0 {
		caption := "部分保护：系统通知和声音仍可能出现。"
		if a.store.Data.Preferences.Immersion {
			caption = "沉浸模式已开启 · 系统通知仍可能出现。"
		}
		m.Text(a.readyProtectionCaption, caption)
	}
	if a.reuseGoal != 0 {
		m.Hidden(a.reuseGoal, len(a.store.Data.History) == 0)
		m.Enabled(a.reuseGoal, len(a.store.Data.History) > 0)
	}
	a.refreshConfigurationDirty()
}

func (a *application) validateStart(goal string) bool {
	minutes, e := strconv.Atoi(strings.TrimSpace(m.Value(a.minutes)))
	valid := true
	focus := m.ID(0)
	if goal == "" || utf8.RuneCountInString(goal) > 200 || strings.ContainsRune(goal, 0) {
		showMessage(a.goalError, "请输入 1–200 字的目标。")
		focus = a.goal
		valid = false
	} else {
		showMessage(a.goalError, "")
	}
	if e != nil || minutes < 1 || minutes > 180 {
		showMessage(a.minuteError, "请输入 1–180 的整数分钟。")
		if focus == 0 {
			focus = a.minutes
		}
		valid = false
	} else {
		showMessage(a.minuteError, "")
	}
	if focus != 0 {
		m.Send(m.ID(a.main.NativeHandle()), "makeFirstResponder:", focus)
	}
	return valid
}
func phaseName(s string) string {
	switch s {
	case "entry":
		return "正在进入"
	case "active":
		return "专注中"
	case "overtime":
		return "继续专注"
	case "paused":
		return "已暂停"
	}
	return "准备开始"
}
func duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	n := int(d / time.Second)
	return fmt.Sprintf("%02d:%02d", n/60, n%60)
}
func (a *application) escape(handle uintptr) bool {
	if a.previewWindow != nil && a.previewWindow.NativeHandle() == handle {
		a.stopAudition()
		return true
	}
	if a.appPicker != nil && (a.appPicker.NativeHandle() == handle || a.main != nil && a.main.NativeHandle() == handle) {
		a.appPicker.Close()
		return true
	}
	if a.unknownSheet != nil && a.main != nil && (uintptr(m.Send(m.ID(a.main.NativeHandle()), "attachedSheet")) == handle || a.main.NativeHandle() == handle) {
		a.unknownSheet.Cancel()
		return true
	}
	if a.control != nil && a.control.NativeHandle() == handle {
		a.control.Hide()
		return true
	}
	if a.entry != nil && a.entry.NativeHandle() == handle {
		a.enter()
		return true
	}
	if a.main != nil && a.main.NativeHandle() == handle {
		if !a.countdownAt.IsZero() {
			a.cancelEntry()
			return true
		}
		if a.controller.Snapshot().Phase == "entry" && a.entryGuideOnly() {
			a.enter()
			return true
		}
		a.hideMain()
		return true
	}
	for _, w := range []*mygo.Window{a.choice} {
		if w != nil && w.NativeHandle() == handle {
			w.Hide()
			return true
		}
	}
	return false
}
func (a *application) refresh() {
	if a.mainPage == pageHistory && a.report.plot != 0 {
		a.refreshHistoryStats(time.Now())
	}
	s := a.controller.Snapshot()
	defer a.refreshCounter(s)
	if a.main != nil {
		a.refreshFocusContent()
	}

	a.refreshNavigation()
	a.refreshAudition()
	for _, f := range []*configForm{a.focusForm, a.templateForm} {
		if f != nil && f.mediaGroup != 0 {
			caption := a.formMediaFailureCaption(f)
			m.Text(f.mediaCaption, caption)
			m.Hidden(f.mediaGroup, caption == "")
		}
	}
	msg := a.notice.Text
	if mediaNotice := a.mediaFailureCaption(); mediaNotice != "" {
		if msg != "" {
			msg += " · "
		}
		msg += mediaNotice + "；查看详情可重读资源或结束后调整对应效果。"
	}
	if a.busy && a.notice.Kind != noticeHandoffProgress {
		msg = "正在整理工作环境…"
	}
	showMessage(a.status, msg)
	if a.primary != 0 {
		m.Enabled(a.primary, !a.busy && !session.Exists(s.Phase))
	}
	for _, fields := range []sessionControls{a.mainSession, a.popoverSession} {
		if fields.root == 0 {
			continue
		}
		caption := msg
		if caption == "" {
			if s.Immersion {
				caption = "沉浸模式 · 系统通知按原设置显示。"
			} else {
				caption = a.notificationCaption()
			}
		}
		if s.Phase == "paused" {
			caption = "部分保护：系统通知和声音仍可能出现。\n" + msg
			if s.Immersion {
				caption = "沉浸模式已暂停。\n" + msg
			}
			if a.appProtectionError() != "" {
				caption = "应用限制未生效；通知和声音仍可能出现。\n" + msg
			}
		}
		showMessage(fields.status, caption)
		m.Send(fields.status, "setToolTip:", m.NS(caption))
		m.Send(fields.primary, "setTitle:", m.NS(sessionPrimaryTitle(s, a.protectionFailure != nil)))
		m.Enabled(fields.primary, session.Exists(s.Phase) && (!a.busy || session.Working(s.Phase)))
		if m.Value(fields.goal) != s.Goal {
			m.Text(fields.goal, s.Goal)
			m.Send(fields.goal, "setToolTip:", m.NS(s.Goal))
		}
		label := "查看详情…"
		if a.pending != nil {
			label = "处理范围外应用…"
		}
		m.Send(fields.detail, "setTitle:", m.NS(label))
		m.FitButtonWidth(fields.detail)
		ordinary := a.notice.Text == "" || a.notice.Kind == noticeInfo || a.notice.Kind == noticeHandoffProgress
		m.Hidden(fields.detail, (s.Phase == "paused" && a.protectionFailure != nil) || (a.pending == nil && ordinary && a.protectionFailure == nil && a.mediaFailureCaption() == ""))
		m.Text(fields.timer, counter(s))
		m.Text(fields.phase, phaseName(s.Phase))
	}
	if a.entryStatus != 0 {
		caption := msg
		if a.entryError != "" {
			caption = "入场短片未播放，可跳过或重试。"
		}
		showMessage(a.entryStatus, caption)
	}
	for _, b := range a.choiceButtons {
		m.Enabled(b, !a.busy)
	}
	if a.choiceStatus != 0 {
		caption := a.outsideResult
		if caption == "" {
			caption = "加入本次只用于当前专注，不改变模版。"
		}
		if a.busy {
			caption = "正在整理环境… 仍可结束专注。"
		}
		m.Text(a.choiceStatus, caption)
	}
	if a.pauseMenu != nil {
		a.pauseMenu.SetEnabled(session.Working(s.Phase) || s.Phase == "entry")
		a.resumeMenu.SetEnabled(s.Phase == "paused" && !a.busy)
		a.endMenu.SetEnabled(session.Exists(s.Phase))
	}
	if a.tray != nil {
		a.refreshTrayTitle(trayTitle(s))
	}
}

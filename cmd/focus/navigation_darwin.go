//go:build darwin

package main

import (
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

type mainPage uint8

const (
	pageFocus mainPage = iota
	pageTemplates
	pageHistory
	pageSettings
	pageCount
)
const mainWidth, mainHeight = 900, 660
const minimumWidth, minimumHeight = 780, 580

var pageRows = [pageCount]int{0, 1, 2, 3}

func (a *application) settingsPane() int {
	if a.mainPage == pageSettings {
		return 2
	}
	return -1
}
func (a *application) ensureMain() {
	if a.main != nil || a.quitReady {
		return
	}
	a.main = window(appDisplayName, mainWidth, mainHeight, false)
	a.windowSizing = map[string]any{"created": m.WindowMetrics(a.main.NativeHandle())}
	initial, minimum := mainWindowSizes(m.VisibleContentSize(a.main.NativeHandle()))
	a.windowSizing["requested"] = map[string]any{"initial": initial, "minimum": minimum}
	resizable(a.main, minimum.X, minimum.Y)
	a.windowSizing["minimum-set"] = m.WindowMetrics(a.main.NativeHandle())
	m.ContentSize(a.main.NativeHandle(), initial.X, initial.Y)
	a.windowSizing["content-set"] = m.WindowMetrics(a.main.NativeHandle())
	m.FitWindowOnScreen(a.main.NativeHandle())
	a.windowSizing["after-fit"] = m.WindowMetrics(a.main.NativeHandle())
	w := a.main
	a.lastSettingsPage = pageSettings
	a.shell = m.FillSurface(w.NativeHandle())
	sidebar := m.Auto(m.View("NSBox", m.R(0, 0, 176, mainHeight)))
	m.Send(sidebar, "setBoxType:", int64(4))
	m.Send(sidebar, "setTitlePosition:", int64(0))
	m.Send(sidebar, "setBorderWidth:", float64(0))
	m.Send(sidebar, "setCornerRadius:", float64(0))
	m.Send(sidebar, "setFillColor:", m.Send(m.Class("NSColor"), "windowBackgroundColor"))
	m.Add(a.shell, sidebar)
	m.Release(sidebar)
	m.Equal(sidebar, "leadingAnchor", a.shell, "leadingAnchor", 0)
	m.Equal(sidebar, "topAnchor", a.shell, "topAnchor", 0)
	m.Equal(sidebar, "bottomAnchor", a.shell, "bottomAnchor", 0)
	m.Fixed(sidebar, "width", 176)
	a.content = m.Auto(m.View("NSView", m.R(176, 0, mainWidth-176, mainHeight)))
	m.Add(a.shell, a.content)
	m.Release(a.content)
	m.Equal(a.content, "leadingAnchor", sidebar, "trailingAnchor", 0)
	m.Equal(a.content, "trailingAnchor", a.shell, "trailingAnchor", 0)
	m.Equal(a.content, "topAnchor", a.shell, "topAnchor", 0)
	m.Equal(a.content, "bottomAnchor", a.shell, "bottomAnchor", 0)
	items := []m.NavigationItem{
		{Title: "专注", Symbol: "scope", Selectable: true},
		{Title: "模版", Symbol: "square.stack", Selectable: true},
		{Title: "统计", Symbol: "chart.bar", Selectable: true},
		{Title: "设置", Symbol: "gearshape", Selectable: true},
	}

	a.navigation = m.NewNavigationList(sidebar, items, func(row int) {
		for page, index := range pageRows {
			if row == index {
				keepNavigationFocus := a.navigation != nil && m.Send(m.ID(w.NativeHandle()), "firstResponder") == a.navigation.Table
				a.showPage(mainPage(page))
				if keepNavigationFocus && !a.mainModal() {
					m.Send(m.ID(w.NativeHandle()), "makeFirstResponder:", a.navigation.Table)
				}
				return
			}
		}
	})
	m.Equal(a.navigation.View, "leadingAnchor", sidebar, "leadingAnchor", 8)
	m.Equal(a.navigation.View, "trailingAnchor", sidebar, "trailingAnchor", -8)
	m.Equal(a.navigation.View, "topAnchor", sidebar, "topAnchor", 16)
	m.Equal(a.navigation.View, "bottomAnchor", sidebar, "bottomAnchor", -16)
	a.windowSizing["shell-built"] = m.WindowMetrics(a.main.NativeHandle())
	w.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitReady {
			e.PreventDefault()
			a.hideMain()
		}
	})
	w.OnFocus(func() {
		if a.main == w && !a.quitReady {
			a.refresh()
		}
	})
	w.OnResize(func() { a.layoutFocusReport() })
	w.OnClosed(func() {
		if a.main != w {
			return
		}
		a.stopAudition()
		if a.historyFromPicker != nil {
			a.historyFromPicker.Close()
		}
		if a.historyToPicker != nil {
			a.historyToPicker.Close()
		}
		a.focusForm.close()
		a.templateForm.close()
		if a.appList != nil {
			a.appList.Close()
			a.appList = nil
		}
		if a.historyList != nil {
			a.historyList.Close()
			a.historyList = nil
		}
		a.navigation.Close()
		m.UnbindTree(a.shell)
		a.main = nil
		a.shell = 0
		a.content = 0
		a.root = 0
		a.pages = [pageCount]m.ID{}
		a.pageResponders = [pageCount]m.ID{}
		a.mainSession = sessionControls{}
		a.settingsRoot = 0
		a.historyRoot = 0
		a.historyReportRoot = 0
		a.report.fromPicker.Close()
		a.report.toPicker.Close()
		a.report.plot = 0
		a.settingsSections = [2]m.ID{}
		a.settingsScroll = [4]m.ID{}
		a.focusEffectsForm = nil
		a.profileField = 0
		a.linkedField = 0
	})
}
func (a *application) pageRoot(page mainPage) m.ID {
	if a.pages[page] == 0 {
		root := m.Auto(m.View("NSView", m.R(0, 0, mainWidth-176, mainHeight)))
		m.Add(a.content, root)
		m.Edges(root, a.content, 0)
		m.Release(root)
		a.pages[page] = root
		m.Hidden(root, page != a.mainPage)
	}
	return a.pages[page]
}
func (a *application) mainModal() bool {
	return a.main != nil && (m.Send(m.ID(a.main.NativeHandle()), "attachedSheet") != 0 || m.Send(m.Send(m.Class("NSApplication"), "sharedApplication"), "modalWindow") != 0 || a.appPicker != nil || a.unknownSheet != nil)
}
func (a *application) rememberPageResponder() {
	if a.main == nil {
		return
	}
	responder := m.Send(m.ID(a.main.NativeHandle()), "firstResponder")
	if objc.Send[bool](responder, m.S("isKindOfClass:"), m.Class("NSTextView")) && objc.Send[bool](responder, m.S("isFieldEditor")) {
		responder = m.Send(responder, "delegate")
	}
	root := a.pages[a.mainPage]
	if root != 0 && objc.Send[bool](responder, m.S("isKindOfClass:"), m.Class("NSView")) && objc.Send[bool](responder, m.S("isDescendantOf:"), root) {
		a.pageResponders[a.mainPage] = responder
	}
}
func (a *application) showPage(page mainPage) {
	if a.quitReady || page >= pageCount {
		return
	}
	a.ensureMain()
	if a.mainModal() {
		if a.navigation != nil {
			a.navigation.Select(pageRows[a.mainPage])
		}
		a.refreshNavigation()
		return
	}
	if page != a.mainPage {
		a.dismissHistoryDates()
		a.rememberPageResponder()
		a.rememberNotificationDraft()
		a.stopAudition()
		m.Send(m.ID(a.main.NativeHandle()), "makeFirstResponder:", m.ID(0))
	}
	a.mainPage = page
	if page == pageSettings {
		a.lastSettingsPage = page
	}
	a.settingsRoot = 0
	a.settingsNotice = 0
	switch page {
	case pageFocus:
		a.pageRoot(pageFocus)
		if a.root == 0 {
			a.renderReady()
		}
		a.presentFocusContent()
	case pageHistory:
		if a.historyRoot == 0 {
			a.renderHistory()
		}
	case pageTemplates:
		if a.templateRoot == 0 {
			a.renderTemplates()
		}
	case pageSettings:
		a.settingsRoot = a.pageRoot(pageSettings)
		if a.settingsScroll[2] == 0 || a.settingsErrors[2] != a.settingsErrorKey() {
			a.renderSettings()
		}
		a.settingsNotice = a.settingsNotices[2]

	}
	for p, root := range a.pages {
		if root != 0 {
			m.Hidden(root, mainPage(p) != page)
		}
	}
	a.navigation.Select(pageRows[page])
	a.refreshNavigation()
	a.refreshReadySummary()
	a.refresh()
	a.main.Show()
	a.main.Focus()
	a.layoutFocusReport()
	m.KeyLoop(a.main.NativeHandle())
	responder := a.pageResponders[page]
	if responder == 0 && page == pageFocus {
		if session.Exists(a.controller.Snapshot().Phase) {
			responder = a.mainSession.primary
		} else {
			responder = a.goal
		}
	}
	if responder != 0 && objc.Send[bool](responder, m.S("isDescendantOf:"), a.pages[page]) && !objc.Send[bool](responder, m.S("isHiddenOrHasHiddenAncestor")) {
		m.Send(m.ID(a.main.NativeHandle()), "makeFirstResponder:", responder)
	}
}

func (a *application) buildSectionTabs(root m.ID, labels []string, selected int, change func(int)) ([2]m.ID, m.ID) {
	var panes [2]m.ID
	tabs := m.Auto(m.Segmented(root, labels, selected, m.R(0, 0, 250, 28), func(index int) {
		if index < 0 || index >= len(panes) {
			return
		}
		for i, pane := range panes {
			m.Hidden(pane, i != index)
		}
		change(index)
		m.KeyLoop(a.main.NativeHandle())
	}))
	m.Send(tabs, "setSegmentStyle:", int64(5)) // NSSegmentStyleCapsule
	for i := range labels {
		m.Send(tabs, "setWidth:forSegment:", float64(102), int64(i))
	}
	m.Equal(tabs, "centerXAnchor", root, "centerXAnchor", 0)
	m.Equal(tabs, "topAnchor", root, "topAnchor", 20)
	m.Fixed(tabs, "height", 32)
	for i := range panes {
		p := m.Auto(m.View("NSView", m.R(0, 0, 1, 1)))
		m.Add(root, p)
		m.Release(p)
		m.Equal(p, "topAnchor", tabs, "bottomAnchor", 8)
		m.Equal(p, "bottomAnchor", root, "bottomAnchor", 0)
		m.Equal(p, "leadingAnchor", root, "leadingAnchor", 0)
		m.Equal(p, "trailingAnchor", root, "trailingAnchor", 0)
		m.Hidden(p, i != selected)
		panes[i] = p
	}
	return panes, tabs
}
func (a *application) reopenMain() {
	if a.quitReady || a.mainModal() {
		return
	}
	if a.controller.Snapshot().Phase == "entry" {
		a.openEntry()
		return
	}
	if a.main == nil {
		a.openMain()
		return
	}
	if a.main.IsMinimized() {
		a.main.Restore()
	}
	if !a.main.IsVisible() {
		a.showPage(a.mainPage)
	} else {
		a.main.Show()
		a.main.Focus()
	}
}
func (a *application) hideMain() {
	a.dismissHistoryDates()
	resumeEntry := a.quitPrompt && a.controller.Snapshot().Phase == "entry"
	a.quitPrompt = false
	a.rememberPageResponder()
	a.rememberNotificationDraft()
	a.stopAudition()
	if a.detail != nil {
		a.detail.Close()
	}
	if a.appPicker != nil {
		a.appPicker.Close()
	}
	if a.unknownSheet != nil {
		a.unknownSheet.Close()
		a.unknownSheet = nil
	}
	if a.main != nil {
		a.main.Hide()
	}
	a.refreshNavigation()
	if resumeEntry {
		a.resumeEntryPresentation()
	}
}
func (a *application) refreshNavigation() {
	if a.navigation == nil {
		return
	}
	enabled := !a.mainModal()
	m.Enabled(a.navigation.Table, enabled)
	for _, item := range a.pageMenus {
		item.SetEnabled(enabled)
	}
}
func (a *application) refreshFocusContent() {
	// AppKit may transfer focus when the current editor's view is hidden.
	// Only restructure the key focus page; background handoff updates labels.
	if a.main != nil && a.mainPage == pageFocus && a.main.IsFocused() {
		a.presentFocusContent()
	}
}
func (a *application) presentFocusContent() {
	if a.pages[pageFocus] == 0 || !a.countdownAt.IsZero() {
		return
	}
	s := a.controller.Snapshot()
	working := session.Exists(s.Phase)
	if working {
		if a.mainSession.root == 0 {
			root := m.Auto(m.View("NSView", m.R(0, 0, mainWidth-176, mainHeight)))
			m.Add(a.pages[pageFocus], root)
			m.Edges(root, a.pages[pageFocus], 0)
			m.Release(root)
			a.mainSession.root = root
		}
		if a.mainSession.timer == 0 {
			a.pageResponders[pageFocus] = 0
			a.mainSession = a.buildSessionControls(a.mainSession.root, false)
		}
		m.Hidden(a.mainSession.root, false)
	} else if a.mainSession.root != 0 {
		m.Hidden(a.mainSession.root, true)
	}
	if a.root != 0 {
		m.Hidden(a.root, working)
	}
}

func mainWindowSizes(available m.Point) (initial, minimum m.Point) {
	initial = m.Point{X: mainWidth, Y: mainHeight}
	minimum = m.Point{X: minimumWidth, Y: minimumHeight}
	if available.X > 0 && available.Y > 0 {
		initial.X = min(initial.X, available.X)
		initial.Y = min(initial.Y, available.Y)
		minimum.X = min(minimum.X, available.X)
		minimum.Y = min(minimum.Y, available.Y)
	}
	return
}

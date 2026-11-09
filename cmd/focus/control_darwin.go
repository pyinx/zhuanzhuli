//go:build darwin

package main

import (
	"fmt"

	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type sessionControls struct {
	root, phase, goal, timer, status, primary, detail m.ID
}

func sessionPrimaryTitle(s session.State, issue bool) string {
	if s.Phase == "paused" {
		if issue {
			return "处理准备问题…"
		}
		if s.Immersion {
			return "恢复沉浸专注"
		}
		return "以部分保护恢复"
	}
	return "暂停"
}
func (a *application) sessionPrimaryAction() {
	s := a.controller.Snapshot()
	if s.Phase == "paused" {
		if a.protectionFailure != nil {
			a.openControlDetails()
			return
		}
		a.acceptLimitedApps = false
		a.resumeWithConsent()
	} else if s.Phase == "entry" || session.Working(s.Phase) {
		a.pauseByUser("已暂停，准备好后继续。")
	}
}
func (a *application) openControl(popover bool) {
	s := a.controller.Snapshot()
	if !session.Exists(s.Phase) {
		a.openMain()
		return
	}
	if s.Phase == "entry" {
		a.openEntry()
		return
	}
	if !popover || a.tray == nil {
		a.openMain()
		return
	}
	if a.mainModal() {
		return
	}
	if a.control == nil {
		a.control = mygo.NewFlyout(mygo.FlyoutOptions{Tray: a.tray, Width: 336, Height: 280, Placement: mygo.PlacementBottom, Popover: true, Focusable: true, Hidden: true, Content: ui.View(func(c *ui.Context) { ui.Column(c).Fill().Children(func() {}) })})
		w := a.control
		w.Show()
		if a.startDisplay == 0 && w.NativeHandle() != 0 {
			a.startDisplay = m.CaptureDisplay(w.NativeHandle())
		}
		if w.NativeHandle() == 0 {
			w.Close()
			a.control = nil
			a.openMain()
			return
		}
		w.OnClosed(func() {
			if a.control == w {
				m.UnbindTree(a.popoverSession.root)
				a.control = nil
				a.popoverSession = sessionControls{}
			}
		})
		a.renderControl()
	}
	a.control.Show()
	if a.controlDirty {
		a.renderControl()
	}
	a.refresh()
	a.control.Focus()
}
func (a *application) renderControl() {
	if a.control == nil {
		return
	}
	if a.control.NativeHandle() == 0 {
		a.controlDirty = true
		return
	}
	a.controlDirty = false
	root := replaceFixedSurface(a.control, a.popoverSession.root, 336, 280, true)
	a.popoverSession = a.buildSessionControls(root, true)
}
func (a *application) buildSessionControls(root m.ID, compact bool) sessionControls {
	fields := sessionControls{root: root}
	var r, actions m.ID
	if compact {
		r = m.Stack(root, true, 8)
		m.Edges(r, root, 20)
		actions = r
	} else {
		r, actions, _ = contentFooter(root)
		titleText(r, "本次专注", 22)
		m.Gap(r, 8)
	}
	s := a.controller.Snapshot()
	fields.phase = bodyText(r, phaseName(s.Phase), 12, true)
	size := float64(15)
	if !compact {
		size = 20
	}
	fields.goal = titleText(r, s.Goal, size)
	m.LimitGoalLabel(fields.goal)
	m.Send(fields.goal, "setSelectable:", true)
	clockSize := float64(44)
	if !compact {
		clockSize = 52
		m.Gap(r, 12)
	}
	fields.timer = bodyText(r, counter(s), clockSize, false)
	m.Send(fields.timer, "setFont:", m.Send(m.Class("NSFont"), "monospacedDigitSystemFontOfSize:weight:", clockSize, float64(-.2)))
	fields.status = bodyText(r, "", 12, true)
	if compact {
		m.Send(fields.status, "setMaximumNumberOfLines:", int64(2))
	}
	buttons := row(actions, 8)
	m.Send(buttons, "setDistribution:", int64(1))
	fields.primary = rowAction(buttons, sessionPrimaryTitle(s, a.protectionFailure != nil), a.sessionPrimaryAction)
	m.Primary(fields.primary)
	rowAction(buttons, "结束专注", a.end)
	bottom := row(actions, 8)
	fields.detail = action(bottom, "查看详情…", a.openControlDetails)
	m.Flex(bottom)
	hide := a.hideMain
	if compact {
		hide = a.control.Hide
	}
	action(bottom, "收起", hide)
	m.KeyLoop(a.main.NativeHandle())
	if compact {
		m.KeyLoop(a.control.NativeHandle())
	}
	return fields
}

func (a *application) openControlDetails() {
	if a.protectionFailure != nil || a.mediaFailureCaption() != "" {
		a.openFailureDetails()
		return
	}
	if a.pending != nil {
		a.showChoice()
		return
	}
	pane := 0
	if a.mediaFailureCaption() != "" && a.protectionFailure == nil {
		pane = 1
	}
	for _, leaf := range protectionLeaves(a.protectionFailure) {
		if issue, ok := leaf.(*protectionError); ok && issue.kind != "应用限制" {
			pane = 3
		}
	}
	if a.protectionFailure == nil && a.notice.Kind != noticeHandoff && a.mediaFailureCaption() == "" {
		pane = 2
	}
	a.openSettingsPane(pane)
	if a.settingsNotice != 0 && a.notice.Text != "" && a.protectionFailure == nil {
		showMessage(a.settingsNotice, a.notice.Text)
	}
}
func (a *application) openEntry() {
	if a.quitPrompt || !a.countdownAt.IsZero() {
		return
	}
	showScene := a.showsEntryScene()
	if !showScene || a.entryAt.IsZero() || a.busy {
		if showScene && !a.entryAt.IsZero() && a.entry != nil {
			a.entry.Show()
			a.entry.Focus()
		} else if a.main != nil {
			a.main.Show()
			a.main.Focus()
		}
		return
	}
	if a.entry == nil {
		a.entry = window("正在进入专注", 480, 334, false)
		w := a.entry
		w.OnClose(func(e *mygo.CloseEvent) {
			if !a.quitReady {
				e.PreventDefault()
				if a.controller.Snapshot().Phase == "entry" {
					a.cancelEntry()
					a.openMain()
				} else {
					w.Hide()
				}
			}
		})
		w.OnClosed(func() {
			if a.entry == w {
				m.UnbindTree(a.entryRoot)
				a.entry = nil
				a.entryRoot = 0
				a.entryStatus = 0
			}
		})
	}
	frame := m.DisplayFrame(a.startDisplay, false)
	if !a.entryPresentation {
		a.previousPresentation = m.PresentEntry(a.entry.NativeHandle(), a.startDisplay)
		a.entryPresentation = true
	}
	if a.entryRoot == 0 || a.video.Player == 0 || a.entryError != "" {
		a.video.Stop()
		a.entryRoot = replaceFixedSurface(a.entry, a.entryRoot, frame.W, frame.H, false)
		if !m.Backdrop(a.entryRoot, a.media(".png"), m.R(0, 0, frame.W, frame.H)) {
			a.recordMediaFailure(a.effectiveConfig().Scene, ".png", fmt.Errorf("背景图片无法解码"))
		}
		// Fit the authored composition, leaving the clean forest behind its edges.
		width, height := frame.W, frame.W*1248/1920
		if height > frame.H {
			height = frame.H
			width = height * 1920 / 1248
		}
		host := m.View("NSView", m.R((frame.W-width)/2, (frame.H-height)/2, width, height))
		m.Add(a.entryRoot, host)
		if a.entryError == "" {
			if err := a.video.Play(host, a.media(".mp4"), m.R(0, 0, width, height), 0); err != nil {
				a.entryError = err.Error()
			}
		}
		m.Release(host)
		footer := m.View("NSVisualEffectView", m.R((frame.W-560)/2, 24, 560, 94))
		m.Send(footer, "setMaterial:", int64(21))
		m.Add(a.entryRoot, footer)
		m.Label(footer, "慢慢呼吸，准备好后回到工作。", m.R(20, 61, 520, 20), 13)
		a.entryStatus = m.Label(footer, "", m.R(20, 39, 520, 18), 12)
		if a.entryError != "" {
			m.Button(footer, "重试短片", m.R(192, 8, 110, 28), func() { a.startEntryVideo(false) })
		}
		m.Button(footer, "返回准备", m.R(16, 8, 110, 28), func() { a.cancelEntry(); a.openMain() })
		b := m.Button(footer, "跳过，开始专注", m.R(376, 8, 168, 28), a.enter)
		m.Primary(b)
		m.Release(footer)
	}
	a.refresh()
	a.entry.Show()
	a.entry.Focus()
}
func (a *application) closeEntryPresentation() {
	a.restoreEntryPresentation()
	if a.entry != nil {
		a.entry.Hide()
	}
}
func (a *application) restoreEntryPresentation() {
	if a.entryPresentation {
		m.RestorePresentation(a.previousPresentation)
		a.entryPresentation = false
	}
}

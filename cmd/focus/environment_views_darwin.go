//go:build darwin

package main

import (
	"errors"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"time"
)

func (a *application) openChoice() {
	if a.choice != nil || a.pending == nil {
		return
	}
	ref := *a.pending
	w := window("范围外应用", 428, 224, true)
	a.choice = w
	root := m.FixedSurface(w.NativeHandle(), 428, 224, false)
	r := m.Stack(root, true, 12)
	m.Edges(r, root, 20)
	m.LimitGoalLabel(titleText(r, ref.Label+" 不在本次专注应用中", 17))
	goal := bodyText(r, "当前目标："+a.controller.Snapshot().Goal, 13, true)
	m.LimitGoalLabel(goal)
	a.choiceStatus = bodyText(r, a.outsideResult, 12, true)
	buttons := row(r, 8)
	m.Send(buttons, "setDistribution:", int64(1))
	current := func() bool {
		if a.busy || a.choice != w || a.pending == nil || !session.Working(a.controller.Snapshot().Phase) {
			return false
		}
		if _, err := m.Instance(ref); err != nil {
			a.pending = nil
			a.closeChoice()
			a.fail(noticeInfo, "此前范围外应用已退出或更换，没有对新进程放行。", nil)
			a.openMain()
			return false
		}
		return true
	}
	back := rowAction(buttons, "返回工作", func() {
		if !current() {
			return
		}
		a.effect(func() error { return a.hide(ref) }, func(e error) {
			if e != nil {
				a.fail(noticeHandoff, "暂未返回工作", e)
				return
			}
			a.pending = nil
			a.closeChoice()
			a.activateWork()
		})
	})
	join := rowAction(buttons, "加入本次", func() {
		if !current() {
			return
		}
		a.controller.AllowApp(session.App{ID: ref.BundleID, Name: ref.Label, Path: ref.Path})
		a.lastWork = &ref
		a.pending = nil
		a.closeChoice()
		if !a.persist() {
			return
		}
		a.effect(func() error {
			if err := a.waitHidden(ref, false); err != nil {
				return err
			}
			return a.journal.Abandon(ref, "explicitly allowed for this session")
		}, func(err error) {
			if err != nil {
				a.fail(noticeHandoff, "已加入本次，但未能确认恢复窗口；可手动打开。", err)
			}
		})
	})
	a.choiceButtons = []m.ID{back, join}
	if a.controller.Snapshot().Immersion {
		m.Hidden(join, true)
		a.choiceButtons = []m.ID{back}
	}
	rowAction(buttons, "结束专注", a.end)
	m.Primary(back)
	bodyText(r, "关闭此窗可稍后决定，专注仍在继续。", 12, true)
	w.OnClose(func(e *mygo.CloseEvent) {
		if a.choice == w && a.pending != nil {
			e.PreventDefault()
			w.Hide()
		}
	})
	w.OnClosed(func() {
		m.UnbindTree(r)
		if a.choice == w {
			a.choice = nil
			a.choiceButtons = nil
			a.choiceStatus = 0
		}
	})
	a.refresh()
	w.ShowInactive()
}
func (a *application) closeChoice() {
	if a.choice != nil {
		w := a.choice
		a.choice = nil
		a.choiceButtons = nil
		a.choiceStatus = 0
		w.Close()
	}
}
func (a *application) showChoice() {
	if a.pending != nil && !a.busy {
		if _, err := m.Instance(*a.pending); err != nil || a.controller.Allowed(a.pending.BundleID) {
			a.pending = nil
			a.closeChoice()
			a.fail(noticeInfo, "此前范围外应用已退出或已加入本次，没有待处理对象。", nil)
			return
		}
	}
	if a.choice == nil {
		a.openChoice()
	}
	if a.choice != nil {
		// Only this explicit user action takes the keyboard. Automatic
		// outside-app prompts continue to use ShowInactive in openChoice.
		// MyGo intentionally does not activate the app for Panel.Show.
		m.Send(m.Send(m.Class("NSApplication"), "sharedApplication"), "activate")
		a.choice.Show()
		if len(a.choiceButtons) > 0 {
			m.Send(m.ID(a.choice.NativeHandle()), "makeFirstResponder:", a.choiceButtons[0])
		}
	}
}
func (a *application) backgroundsOn() {
	for _, w := range a.backgrounds {
		w.Close()
	}
	a.backgrounds = nil
	if !a.effectiveConfig().Background {
		return
	}
	path := a.media(".png")
	if path == "" {
		a.diagnostic("media", "background-unavailable")
		return
	}
	for _, d := range mygo.Screen.Displays() {
		b := d.Bounds
		w := mygo.NewWindow(mygo.WindowOptions{Title: "专注环境", Width: b.Width, Height: b.Height, X: b.X, Y: b.Y, Hidden: true, Frameless: true, DisableResize: true, DisableMove: true, SkipTaskbar: true, Content: ui.View(func(c *ui.Context) { ui.Column(c).Fill().Children(func() {}) })})
		n := m.ID(w.NativeHandle())
		// MyGo's Frameless hides a titled window's chrome. A desktop backdrop
		// needs a borderless window so AppKit does not shift it below the menu bar.
		m.Send(n, "setStyleMask:", uint64(0))
		w.SetBounds(b)
		m.Send(n, "setLevel:", int64(-2147483622))
		m.Send(n, "setIgnoresMouseEvents:", true)
		m.Send(n, "setCollectionBehavior:", uint64(1|16|256))
		r := m.Surface(w.NativeHandle())
		if !m.Backdrop(r, path, m.R(0, 0, float64(b.Width), float64(b.Height))) {
			a.recordMediaFailure(a.effectiveConfig().Scene, ".png", errors.New("背景图片无法解码"))
			w.Close()
			return
		}
		a.backgrounds = append(a.backgrounds, w)
		w.ShowInactive()
	}
}

var playDueReminder = playReminderSound

func (a *application) gentleReminder() {
	a.reminderUntil = time.Now().Add(12 * time.Second)
	a.reminderPlaybackFailed = false
	if a.effectiveConfig().ReminderSound { // Only an explicitly enabled, app-owned local sound.
		if err := playDueReminder(); err != nil {
			a.reminderPlaybackFailed = true
			a.notice = Notice{noticeMedia, "时间到了，但提示音未播放。检查系统声音输出后，可在入场与提醒中重试提示音。"}
			a.diagnostic("reminder", "play-failed")
		}
	}
}

func playReminderSound() error {
	s := m.Send(m.Class("NSSound"), "soundNamed:", m.NS("Glass"))
	if s == 0 {
		return errors.New("系统提示音不可用")
	}
	m.Send(s, "stop")
	m.Send(s, "setVolume:", float32(.7))
	if !objc.Send[bool](s, m.S("play")) {
		return errors.New("系统提示音播放失败")
	}
	return nil
}

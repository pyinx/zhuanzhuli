package main

import (
	"focus.local/macos-capabilities/internal/session"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func needsChoice(before, after session.View) bool {
	return !before.PendingChoice && after.PendingChoice
}
func choiceView(c *ui.Context, busy bool, choose func(string)) {
	ui.Column(c).Fill().Padding(20).Gap(14).Children(func() {
		ui.Text(c, "Fixture B 不在本次允许范围内").FontSize(17).Bold()
		ui.Text(c, "请选择如何继续。不会自动关闭或终止应用。").FontSize(12)
		ui.Row(c).Gap(8).Children(func() {
			for _, b := range []struct{ label, kind string }{{"返回", "return"}, {"加入本次", "allow"}, {"退出专注", "end"}} {
				b := b
				if ui.Button(c, b.label).Disabled(busy).Clicked() {
					choose(b.kind)
				}
			}
		})
	})
}
func (h *host) presentChoice() {
	if h.choice != nil || !h.controller.Snapshot().PendingChoice {
		return
	}
	win := mygo.NewWindow(mygo.WindowOptions{Title: "范围外应用 · 请选择", Panel: true, AlwaysOnTop: true, Width: 440, Height: 180, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true, Content: ui.View(func(c *ui.Context) { choiceView(c, h.busy, func(kind string) { h.action(kind) }) })})
	h.choice = win
	win.OnClosed(func() {
		if h.choice == win {
			h.choice = nil
		}
	})
	h.event("choice-presented", map[string]string{"kind": "native-panel", "fixture": "B"})
}

//go:build darwin

package main

import (
	"focus.local/zhuanzhuli/internal/session"
	"time"
)

// All presentations derive from the same snapshot; none owns another clock.
func counter(s session.State) string {
	limit := time.Duration(s.MinutesValue()) * time.Minute
	if s.Elapsed >= limit {
		return "+" + duration(s.Elapsed-limit)
	}
	return duration((limit - s.Elapsed + time.Second - 1) / time.Second * time.Second)
}
func trayTitle(s session.State) string {
	if !session.Exists(s.Phase) {
		return "◉"
	}
	if s.Phase == "entry" {
		return "◉ 正在进入"
	}
	if s.Phase == "paused" {
		return "Ⅱ " + counter(s)
	}
	return "◉ " + counter(s)
}

func (a *application) refreshCounter(s session.State) {
	if !a.nativeReady {
		return
	}
	if !session.Working(s.Phase) && !(s.Phase == "paused" && s.WorkStarted != nil && *s.WorkStarted) {
		a.counterOverlay.Close()
		return
	}
	if a.counterOverlay.Window == 0 {
		a.counterOverlay.Open(a.startDisplay)
	}
	text := counter(s)
	if s.Phase == "paused" {
		text = "已暂停  " + text
	}
	due := session.Working(s.Phase) && time.Now().Before(a.reminderUntil)
	if due {
		if a.reminderPlaybackFailed {
			text = "到点 · 声音失败"
		} else {
			text = "时间到了  " + text
		}
	}
	a.counterOverlay.Update(text, due)
}

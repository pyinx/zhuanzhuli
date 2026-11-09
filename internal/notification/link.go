// Package notification owns a narrowly scoped system Focus lease. A successful
// lease is evidence of a selected mode, never evidence that notifications cannot escape.
package notification

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"focus.local/zhuanzhuli/pkg/journal"
)

var ErrUncertain = errors.New("无法确认系统专注模式仍由本次专注控制")

// Panel must return a complete, unambiguous list, including explicit off values.
// Implementations must re-read the state before pressing, and observe the result.
type Panel interface {
	Modes() (map[string]bool, error)
	Set(name string, on bool) error
}
type Driver interface{ WithPanel(func(Panel) error) error }
type lease struct {
	Version int    `json:"version"`
	Profile string `json:"profile"`
	Status  string `json:"status"`
}

// Link is used only by the application's serial environment worker.
type Link struct {
	path  string
	state lease
	live  bool
}

func Open(path string) (*Link, error) {
	l := &Link{path: path, state: lease{Version: 1, Status: "released"}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &l.state); err != nil {
		return nil, err
	}
	if l.state.Version != 1 || (l.state.Status != "pending" && l.state.Status != "applied" && l.state.Status != "released") || (l.state.Status != "released" && strings.TrimSpace(l.state.Profile) == "") {
		return nil, errors.New("通知恢复记录无效；保留原文件")
	}
	// A restarted process cannot prove continuous ownership. Never turn Focus off
	// merely because the same profile is selected after a crash.
	return l, nil
}
func (l *Link) Pending() bool { return l.state.Status != "released" }
func (l *Link) PendingProfile() string {
	if l.Pending() {
		return l.state.Profile
	}
	return ""
}
func (l *Link) write(s lease) error {
	if err := journal.WriteJSON(l.path, s); err != nil {
		return err
	}
	l.state = s
	return nil
}

// KeepCurrent ends tracking without changing system Focus.
func (l *Link) KeepCurrent() error {
	if err := l.write(lease{Version: 1, Status: "released"}); err != nil {
		return err
	}
	l.live = false
	return nil
}

// CloseConfirmed is an explicit user command for this pending profile, never an
// ownership inference. The caller must capture the profile shown in the review.
// All accesses, including this call, remain on the serial environment worker.
func (l *Link) CloseConfirmed(d Driver, profile string) error {
	if !l.Pending() {
		return nil
	}
	if profile == "" || profile != l.state.Profile {
		return errors.New("通知记录已改变，请重新核对；未切换系统模式")
	}
	// A known panel cleanup failure must not strand a new system operation.
	if err := d.WithPanel(func(p Panel) error { _, err := p.Modes(); return err }); err != nil {
		return err
	}
	err := d.WithPanel(func(p Panel) error {
		modes, err := p.Modes()
		if err != nil {
			return err
		}
		if !only(modes, profile, true) && !only(modes, profile, false) {
			return errors.New("系统专注模式已改变或不可识别；保留当前状态，请重新核对")
		}
		if modes[profile] {
			if err = p.Set(profile, false); err != nil {
				return err
			}
			modes, err = p.Modes()
			if err != nil {
				return err
			}
		}
		if !only(modes, profile, false) {
			return errors.New("未确认指定专注模式已关闭；记录保留待核对")
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Commit after both the observed system result and panel cleanup. A failed
	// save retains the lease; retry observes off without toggling it back on.
	return l.KeepCurrent()
}
func only(modes map[string]bool, name string, on bool) bool {
	v, ok := modes[name]
	if !ok || v != on {
		return false
	}
	for n, active := range modes {
		if active && (n != name || !on) {
			return false
		}
	}
	return true
}

// Acquire preserves any existing different Focus, and borrows a pre-existing
// matching Focus without taking ownership. continuing is checked just before mutation.
func (l *Link) Acquire(d Driver, profile string, continuing func() bool) error {
	if l.Pending() {
		return ErrUncertain
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return errors.New("请先选择已在系统中配置的专用专注模式")
	}
	// A driver may fail after its callback, while dismissing the system panel.
	// Prove a read-only round trip first so a known cleanup failure cannot
	// enable Focus and strand an applied change behind an unusable panel.
	if err := d.WithPanel(func(p Panel) error {
		_, err := p.Modes()
		return err
	}); err != nil {
		return err
	}
	return d.WithPanel(func(p Panel) error {
		modes, err := p.Modes()
		if err != nil {
			return err
		}
		if _, ok := modes[profile]; !ok {
			return errors.New("没有找到指定的专注模式；未修改系统")
		}
		if only(modes, profile, true) {
			return nil
		} // borrowed, never undo
		if !only(modes, profile, false) {
			return errors.New("系统已有其他专注模式，已保留；通知保护受限")
		}
		if !continuing() {
			return errors.New("会话已暂停或结束，未开启专注模式")
		}
		if err = l.write(lease{Version: 1, Profile: profile, Status: "pending"}); err != nil {
			return err
		}
		if !continuing() {
			return l.KeepCurrent()
		}
		if err = p.Set(profile, true); err != nil {
			return err
		}
		modes, err = p.Modes()
		if err != nil {
			return err
		}
		if !only(modes, profile, true) {
			return errors.New("无法确认专注模式已开启；改动保留待核对")
		}
		if err = l.write(lease{Version: 1, Profile: profile, Status: "applied"}); err != nil {
			return err
		}
		l.live = true
		return nil
	})
}
func (l *Link) Restore(d Driver) error {
	if !l.Pending() {
		return nil
	}
	if !l.live || l.state.Status != "applied" {
		return ErrUncertain
	}
	return d.WithPanel(func(p Panel) error {
		modes, err := p.Modes()
		if err != nil {
			return err
		}
		if _, ok := modes[l.state.Profile]; !ok {
			return errors.New("原专注模式不再可识别；未修改系统")
		}
		if !only(modes, l.state.Profile, true) {
			return l.KeepCurrent()
		} // preserve external changes
		// A live process and the same selected mode do not prove ownership:
		// external off→on produces exactly this observation. The panel API has
		// no change generation or conditional write. Preserve the active mode
		// and the durable pending record until explicit review, never guess.
		return ErrUncertain
	})
}

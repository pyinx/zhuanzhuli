//go:build darwin

package macos

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var escapeOnce sync.Once
var escapeErr error
var axSystem func() uintptr
var axPID func(uintptr, *int32) int32
var cgKey func(uintptr, uint16, bool) uintptr
var cgFlags func(uintptr, uint64)
var cgPostPID func(int32, uintptr)

func loadPanelEscape() error {
	if err := loadAX(); err != nil {
		return err
	}
	escapeOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			escapeErr = err
			return
		}
		purego.RegisterLibFunc(&axSystem, lib, "AXUIElementCreateSystemWide")
		purego.RegisterLibFunc(&axPID, lib, "AXUIElementGetPid")
		purego.RegisterLibFunc(&cgKey, lib, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgFlags, lib, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgPostPID, lib, "CGEventPostToPid")
	})
	return escapeErr
}

// PanelCloser is single-use for one WithPanel operation. OnMain must synchronously
// dispatch AppKit identity checks to the main thread. No global events are posted.
type PanelCloser struct {
	application ID // retained NSRunningApplication identity, never a PID-only lease
	pid         int32
	path        string
	onMain      func(func())
	SentPairs   int
	FocusedPID  int32
	used        bool
}

// Control Center may have no LaunchServices launchDate. Its SDK explicitly
// requires NSRunningApplication.isEqual: for process comparison. Retain that
// instance only for this short operation; durable app journals keep Describe.
func NewPanelCloser(pid int32, onMain func(func())) (*PanelCloser, error) {
	if pid < 1 || onMain == nil {
		return nil, errors.New("控制中心身份检查不可用")
	}
	c := &PanelCloser{pid: pid, onMain: onMain}
	var err error
	onMain(func() {
		a := Send(Class("NSRunningApplication"), "runningApplicationWithProcessIdentifier:", pid)
		if a == 0 || ControlCenterPID() != pid || objc.Send[bool](a, S("isTerminated")) || Str(Send(a, "bundleIdentifier")) != "com.apple.controlcenter" {
			err = errors.New("控制中心实例不可识别")
			return
		}
		c.path, err = bundlePath(Str(Send(Send(a, "bundleURL"), "path")))
		if err == nil {
			c.application = Send(a, "retain")
		}
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *PanelCloser) Release() {
	if c.application != 0 {
		c.onMain(func() { Release(c.application); c.application = 0 })
	}
}

type panelEscapeState struct {
	trusted, instanceSame, windowSame, focusedWindowSame bool
	pid, ownedPID, focusedPID                            int32
	windows                                              int64 // -1 means unreadable
}

func (s panelEscapeState) validate() error {
	if !s.trusted {
		return errors.New("辅助功能权限不可用，未发送 Esc")
	}
	if s.pid < 1 || s.ownedPID != s.pid || !s.instanceSame {
		return errors.New("控制中心进程归属已改变，未发送 Esc")
	}
	if s.windows != 1 || !s.windowSame {
		return errors.New("控制中心窗口归属未知或已改变，未发送 Esc")
	}
	if s.focusedPID != s.pid || !s.focusedWindowSame {
		return errors.New("控制中心不是已确认的键盘前台窗口，未发送 Esc")
	}
	return nil
}

func (c *PanelCloser) sendOnce(s panelEscapeState, post func() error) error {
	if c.used {
		return errors.New("本次关闭已尝试，不重复发送 Esc")
	}
	c.used = true
	if err := s.validate(); err != nil {
		return err
	}
	if err := post(); err != nil {
		return err
	}
	c.SentPairs++
	return nil
}

func (c *PanelCloser) Close(app, window uintptr) error {
	if err := loadPanelEscape(); err != nil {
		return err
	}
	s := panelEscapeState{pid: c.pid, trusted: axTrusted(), windows: -1}
	if c.onMain != nil && c.application != 0 {
		c.onMain(func() {
			a := Send(Class("NSRunningApplication"), "runningApplicationWithProcessIdentifier:", c.pid)
			path, e := bundlePath(Str(Send(Send(a, "bundleURL"), "path")))
			s.instanceSame = e == nil && path == c.path && a != 0 && ControlCenterPID() == c.pid &&
				!objc.Send[bool](c.application, S("isTerminated")) && !objc.Send[bool](a, S("isTerminated")) &&
				objc.Send[bool](c.application, S("isEqual:"), a) && objc.Send[int32](a, S("processIdentifier")) == c.pid
		})
	}
	if app != 0 {
		axPID(app, &s.ownedPID)
	}
	w, code := axGet(app, "AXWindows")
	if w != 0 {
		defer cfDrop(w)
		if code == 0 && cfType(w) == cfArrayType() {
			s.windows = cfCount(w)
			if s.windows == 1 && window != 0 {
				s.windowSame = cfEqual(cfAt(w, 0), window)
			}
		}
	}
	system := axSystem()
	if system != 0 {
		defer cfDrop(system)
		if axTimeout(system, 0.4) != 0 {
			return errors.New("无法限制键盘前台检查时限，未发送 Esc")
		}
		focused, fc := axGet(system, "AXFocusedApplication")
		if focused != 0 {
			defer cfDrop(focused)
			if fc == 0 && axPID(focused, &s.focusedPID) == 0 && s.focusedPID == s.pid {
				if axTimeout(focused, 0.4) != 0 {
					return errors.New("无法限制前台窗口检查时限，未发送 Esc")
				}
				fw, wc := axGet(focused, "AXFocusedWindow")
				if fw != 0 {
					defer cfDrop(fw)
					s.focusedWindowSame = wc == 0 && window != 0 && cfEqual(fw, window)
				}
			}
		}
	}
	c.FocusedPID = s.focusedPID
	return c.sendOnce(s, func() error {
		// kVK_Escape = 0x35 in the SDK. Allocate both before posting either.
		down, up := cgKey(0, 0x35, true), cgKey(0, 0x35, false)
		if down != 0 {
			defer cfDrop(down)
		}
		if up != 0 {
			defer cfDrop(up)
		}
		if down == 0 || up == 0 {
			return errors.New("无法创建完整 Esc 事件，未发送")
		}
		cgFlags(down, 0)
		cgFlags(up, 0)
		cgPostPID(s.pid, down)
		cgPostPID(s.pid, up)
		return nil // WithPanel separately observes actual window disappearance.
	})
}

//go:build darwin

package macos

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

// Read and write an explicit state, never press the window's toggle button.
// Some AppKit windows publish this AX attribute. Treat it as a per-window
// capability: unsupported or non-Boolean values never authorize a mutation.
const fullscreenAttribute = "AXFullScreen"

var ErrWorkWindowUnavailable = errors.New("没有可识别的工作窗口，请先打开应用窗口")
var ErrWorkWindowGone = errors.New("工作窗口已关闭")

type FullscreenWindow struct {
	ref, observer uintptr
	pid           int32
	revision      atomic.Uint64
	ownedRevision uint64
}

func WorkFullscreenWindow(pid int32) (*FullscreenWindow, error) {
	return workFullscreenWindow(pid, false)
}

func FocusedFullscreenWindow(pid int32) (*FullscreenWindow, error) {
	return workFullscreenWindow(pid, true)
}

func workFullscreenWindow(pid int32, focused bool) (*FullscreenWindow, error) {
	if err := loadAX(); err != nil {
		return nil, err
	}
	app := axApp(pid)
	if app == 0 {
		return nil, ErrWorkWindowUnavailable
	}
	defer cfDrop(app)
	if code := axTimeout(app, .5); code != 0 {
		return nil, fmt.Errorf("无法限制窗口查询等待时间（AX %d）", code)
	}
	attrs := []string{"AXMainWindow", "AXFocusedWindow"}
	if focused {
		attrs = []string{"AXFocusedWindow"}
	}
	var preferred *FullscreenWindow
	for _, attr := range attrs {
		ref, code := axGet(app, attr)
		if ref == 0 {
			continue
		}
		if code == 0 && standardWorkWindow(ref) {
			w := &FullscreenWindow{ref: ref, pid: pid}
			if focused {
				return w, nil
			}
			if full, _ := w.State(); full {
				if preferred != nil {
					preferred.Close()
				}
				return w, nil
			}
			if preferred == nil {
				preferred = w
				continue
			}
		}
		cfDrop(ref)
	}
	if focused {
		return nil, ErrWorkWindowUnavailable
	}
	windows, code := axGet(app, "AXWindows")
	if windows != 0 {
		defer cfDrop(windows)
	}
	if code == 0 && windows != 0 && cfType(windows) == cfArrayType() {
		count := cfCount(windows)
		for i := int64(0); i < count; i++ {
			ref := cfAt(windows, i)
			if ref == 0 || !standardWorkWindow(ref) {
				continue
			}
			w := &FullscreenWindow{ref: ref, pid: pid}
			full, _ := w.State()
			if full || (preferred == nil && count == 1) {
				if preferred != nil {
					preferred.Close()
				}
				// The array owns the element until we retain a separate reference.
				cfRetain(ref)
				return w, nil
			}
		}
	}
	if preferred != nil {
		return preferred, nil
	}
	return nil, ErrWorkWindowUnavailable
}

func standardWorkWindow(ref uintptr) bool {
	if axString(ref, "AXRole") != "AXWindow" || axString(ref, "AXSubrole") != "AXStandardWindow" {
		return false
	}
	v, code := axGet(ref, "AXMinimized")
	if v != 0 {
		defer cfDrop(v)
	}
	return code == 0 && v != 0 && cfType(v) == cfBoolType() && !cfBool(v)
}

func (w *FullscreenWindow) State() (bool, error) {
	v, code := axGet(w.ref, fullscreenAttribute)
	if v != 0 {
		defer cfDrop(v)
	}
	if code == -25202 { // kAXErrorInvalidUIElement
		return false, ErrWorkWindowGone
	}
	if code != 0 || v == 0 || cfType(v) != cfBoolType() {
		return false, fmt.Errorf("无法读取窗口的原生全屏状态（AX %d）", code)
	}
	return cfBool(v), nil
}

func (w *FullscreenWindow) Writable() error {
	key := cfString(0, fullscreenAttribute, 0x08000100)
	defer cfDrop(key)
	var writable uint8
	if code := axSettable(w.ref, key, &writable); code != 0 || writable == 0 {
		return fmt.Errorf("该窗口不支持自动切换原生全屏（AX %d）", code)
	}
	return nil
}

func (w *FullscreenWindow) Set(fullscreen bool) error {
	key := cfString(0, fullscreenAttribute, 0x08000100)
	defer cfDrop(key)
	value := cfFalse
	if fullscreen {
		value = cfTrue
	}
	if w.Changed() {
		return errors.New("窗口状态刚刚改变，未发送全屏操作")
	}
	if code := axSet(w.ref, key, value); code != 0 {
		return fmt.Errorf("原生全屏请求未确认（AX %d）", code)
	}
	return nil
}

func (w *FullscreenWindow) Matches(other *FullscreenWindow) bool {
	return w.pid == other.pid && cfEqual(w.ref, other.ref)
}

func (w *FullscreenWindow) Title() string { return axString(w.ref, "AXTitle") }

var fullscreenObservers sync.Map
var fullscreenCallback = sync.OnceValue(func() uintptr {
	return purego.NewCallback(func(observer, element, name, context uintptr) {
		if value, ok := fullscreenObservers.Load(observer); ok {
			value.(*FullscreenWindow).revision.Add(1)
		}
	})
})

// A later resize/value change is conservatively treated as an external edit.
// Only uninterrupted same-window observations authorize an automatic undo.
func (w *FullscreenWindow) Watch() error {
	var observer uintptr
	if code := axObserverCreate(w.pid, fullscreenCallback(), &observer); code != 0 || observer == 0 {
		return fmt.Errorf("无法观察窗口变化（AX %d）", code)
	}
	for _, name := range []string{"AXResized", "AXUIElementDestroyed"} {
		key := cfString(0, name, 0x08000100)
		code := axObserverAdd(observer, w.ref, key, 0)
		cfDrop(key)
		if code != 0 {
			cfDrop(observer)
			return fmt.Errorf("窗口不支持安全的恢复观察（AX %d）", code)
		}
	}
	fullscreenObservers.Store(observer, w)
	w.observer = observer
	cfAddSource(cfMainLoop(), axObserverSource(observer), cfCommonModes)
	return nil
}

func (w *FullscreenWindow) Revision() uint64 { return w.revision.Load() }
func (w *FullscreenWindow) Changed() bool    { return w.Revision() != w.ownedRevision }
func (w *FullscreenWindow) Watching() bool   { return w.observer != 0 }
func (w *FullscreenWindow) ArmRestore(revision uint64) {
	w.ownedRevision = revision
}

func (w *FullscreenWindow) Close() {
	if w.observer != 0 {
		fullscreenObservers.Delete(w.observer)
		cfRemoveSource(cfMainLoop(), axObserverSource(w.observer), cfCommonModes)
		cfDrop(w.observer)
		w.observer = 0
	}
	if w.ref != 0 {
		cfDrop(w.ref)
		w.ref = 0
	}
}

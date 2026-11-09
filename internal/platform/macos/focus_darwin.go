//go:build darwin

package macos

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"focus.local/zhuanzhuli/internal/notification"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// The AX adapter is deliberately strict: unknown Control Center structures are
// unsupported, never a reason to fall back to coordinates or private APIs.
// The separately authorized Escape path requires exact ownership and keyboard focus.
var axOnce sync.Once
var axErr error
var axTrusted func() bool
var axApp func(int32) uintptr
var axCopy func(uintptr, uintptr, *uintptr) int32
var axSet func(uintptr, uintptr, uintptr) int32
var axSettable func(uintptr, uintptr, *uint8) int32
var axObserverCreate func(int32, uintptr, *uintptr) int32
var axObserverAdd func(uintptr, uintptr, uintptr, uintptr) int32
var axObserverSource func(uintptr) uintptr
var axActions func(uintptr, *uintptr) int32
var axPerform func(uintptr, uintptr) int32
var axTimeout func(uintptr, float32) int32
var cfDrop func(uintptr)
var cfRetain func(uintptr) uintptr
var cfCount func(uintptr) int64
var cfAt func(uintptr, int64) uintptr
var cfType func(uintptr) uint64
var cfStringType, cfNumberType, cfBoolType, cfArrayType func() uint64
var cfString func(uintptr, string, uint32) uintptr
var cfCString func(uintptr, *byte, int64, uint32) bool
var cfNumber func(uintptr, int32, *int64) bool
var cfBool func(uintptr) bool
var cfEqual func(uintptr, uintptr) bool
var cfMainLoop func() uintptr
var cfAddSource, cfRemoveSource func(uintptr, uintptr, uintptr)
var cfTrue, cfFalse, cfCommonModes uintptr

func loadAX() error {
	axOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			axErr = err
			return
		}
		core, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			axErr = err
			return
		}
		purego.RegisterLibFunc(&axTrusted, lib, "AXIsProcessTrusted")
		purego.RegisterLibFunc(&axApp, lib, "AXUIElementCreateApplication")
		purego.RegisterLibFunc(&axCopy, lib, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&axSet, lib, "AXUIElementSetAttributeValue")
		purego.RegisterLibFunc(&axSettable, lib, "AXUIElementIsAttributeSettable")
		purego.RegisterLibFunc(&axObserverCreate, lib, "AXObserverCreate")
		purego.RegisterLibFunc(&axObserverAdd, lib, "AXObserverAddNotification")
		purego.RegisterLibFunc(&axObserverSource, lib, "AXObserverGetRunLoopSource")
		purego.RegisterLibFunc(&axActions, lib, "AXUIElementCopyActionNames")
		purego.RegisterLibFunc(&axPerform, lib, "AXUIElementPerformAction")
		purego.RegisterLibFunc(&axTimeout, lib, "AXUIElementSetMessagingTimeout")
		purego.RegisterLibFunc(&cfDrop, core, "CFRelease")
		purego.RegisterLibFunc(&cfRetain, core, "CFRetain")
		purego.RegisterLibFunc(&cfCount, core, "CFArrayGetCount")
		purego.RegisterLibFunc(&cfAt, core, "CFArrayGetValueAtIndex")
		purego.RegisterLibFunc(&cfType, core, "CFGetTypeID")
		purego.RegisterLibFunc(&cfStringType, core, "CFStringGetTypeID")
		purego.RegisterLibFunc(&cfNumberType, core, "CFNumberGetTypeID")
		purego.RegisterLibFunc(&cfBoolType, core, "CFBooleanGetTypeID")
		purego.RegisterLibFunc(&cfArrayType, core, "CFArrayGetTypeID")
		purego.RegisterLibFunc(&cfString, core, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfCString, core, "CFStringGetCString")
		purego.RegisterLibFunc(&cfNumber, core, "CFNumberGetValue")
		purego.RegisterLibFunc(&cfBool, core, "CFBooleanGetValue")
		purego.RegisterLibFunc(&cfEqual, core, "CFEqual")
		purego.RegisterLibFunc(&cfMainLoop, core, "CFRunLoopGetMain")
		purego.RegisterLibFunc(&cfAddSource, core, "CFRunLoopAddSource")
		purego.RegisterLibFunc(&cfRemoveSource, core, "CFRunLoopRemoveSource")
		var copyMemory func(*uintptr, uintptr, uintptr) uintptr
		purego.RegisterLibFunc(&copyMemory, purego.RTLD_DEFAULT, "memcpy")
		for _, v := range []struct {
			name string
			dest *uintptr
		}{{"kCFBooleanTrue", &cfTrue}, {"kCFBooleanFalse", &cfFalse}, {"kCFRunLoopCommonModes", &cfCommonModes}} {
			symbol, err := purego.Dlsym(core, v.name)
			if err != nil {
				axErr = err
				return
			}
			copyMemory(v.dest, symbol, unsafe.Sizeof(uintptr(0)))
		}
	})
	return axErr
}

// FocusAccess checks only this process's existing grant. It never prompts.
func FocusAccess() (bool, error) {
	if err := loadAX(); err != nil {
		return false, err
	}
	return axTrusted(), nil
}

// ControlCenterPID is called on the main thread; it never launches the process.
func ControlCenterPID() int32 {
	apps := Send(Class("NSRunningApplication"), "runningApplicationsWithBundleIdentifier:", NS("com.apple.controlcenter"))
	if objc.Send[uint64](apps, S("count")) != 1 {
		return 0
	}
	return objc.Send[int32](Send(apps, "objectAtIndex:", uint64(0)), S("processIdentifier"))
}
func OpenAccessibilitySettings() {
	u := Send(Class("NSURL"), "URLWithString:", NS("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"))
	Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "openURL:", u)
}

type FocusDriver struct {
	PID int32
	// Create a fresh single-use closer before each open, including Acquire's
	// separate read-only preflight. Never reuse one across panel operations.
	NewCloser func() (*PanelCloser, error)
	// Enable serializes the final policy check and AX request with the main
	// thread's pause/end transition. Restore is permitted after the session ends.
	Enable func(func() error) error
}

func axText(ref uintptr) string {
	if ref == 0 || cfType(ref) != cfStringType() {
		return ""
	}
	b := make([]byte, 4096)
	if !cfCString(ref, &b[0], int64(len(b)), 0x08000100) {
		return ""
	}
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return ""
}
func axGet(ref uintptr, key string) (uintptr, int32) {
	k := cfString(0, key, 0x08000100)
	defer cfDrop(k)
	var value uintptr
	code := axCopy(ref, k, &value)
	return value, code
}
func axString(ref uintptr, key string) string {
	v, _ := axGet(ref, key)
	if v == 0 {
		return ""
	}
	defer cfDrop(v)
	return axText(v)
}
func axActionNames(ref uintptr) []string {
	var array uintptr
	if axActions(ref, &array) != 0 || array == 0 {
		return nil
	}
	defer cfDrop(array)
	if cfType(array) != cfArrayType() {
		return nil
	}
	var result []string
	for i := int64(0); i < cfCount(array); i++ {
		result = append(result, axText(cfAt(array, i)))
	}
	return result
}
func axDo(ref uintptr, action string) error {
	for _, name := range axActionNames(ref) {
		if name == action {
			a := cfString(0, action, 0x08000100)
			defer cfDrop(a)
			if code := axPerform(ref, a); code != 0 {
				return fmt.Errorf("系统专注操作未确认（AX %d）", code)
			}
			return nil
		}
	}
	return errors.New("系统没有提供所需的辅助功能操作")
}

type axTree struct {
	refs   []uintptr
	arrays []uintptr
}

func (t *axTree) close() {
	for _, r := range t.arrays {
		cfDrop(r)
	}
}
func (t *axTree) walk(ref uintptr, depth int) error {
	if depth > 18 || len(t.refs) > 2048 {
		return errors.New("控制中心结构超出支持范围")
	}
	t.refs = append(t.refs, ref)
	v, code := axGet(ref, "AXChildren")
	if v != 0 {
		t.arrays = append(t.arrays, v)
	}
	if code == -25205 || code == -25212 {
		return nil
	} // unsupported/no value, a leaf
	if code != 0 {
		return fmt.Errorf("无法完整读取控制中心（AX %d）", code)
	}
	if v == 0 || cfType(v) != cfArrayType() {
		return errors.New("控制中心子节点类型无法识别")
	}
	for i := int64(0); i < cfCount(v); i++ {
		if err := t.walk(cfAt(v, i), depth+1); err != nil {
			return err
		}
	}
	return nil
}
func axUnique(t *axTree, match func(uintptr) bool) (uintptr, error) {
	var found uintptr
	for _, r := range t.refs {
		if match(r) {
			if found != 0 {
				return 0, errors.New("控制中心目标不唯一，未操作")
			}
			found = r
		}
	}
	if found == 0 {
		return 0, errors.New("当前系统控制中心结构尚不受支持")
	}
	return found, nil
}

// Return the borrowed owned window, zero if already closed, or refuse an
// unreadable/replaced panel. Both arrays remain retained by WithPanel.
func panelWindowToClose(opened, current uintptr, code int32) (uintptr, error) {
	if current == 0 || code != 0 || cfType(current) != cfArrayType() {
		return 0, errors.New("控制中心关闭状态未知，未操作")
	}
	if cfCount(current) == 0 {
		return 0, nil
	}
	if cfCount(current) != 1 || opened == 0 || cfType(opened) != cfArrayType() || cfCount(opened) != 1 || !cfEqual(cfAt(current, 0), cfAt(opened, 0)) {
		return 0, errors.New("控制中心面板归属未知或已改变，未自动关闭")
	}
	return cfAt(opened, 0), nil
}

func (d FocusDriver) WithPanel(fn func(notification.Panel) error) (err error) {
	trusted, err := FocusAccess()
	if err != nil {
		return err
	}
	if !trusted {
		return errors.New("未授予本应用辅助功能权限；通知联动未开启")
	}
	if d.PID < 1 {
		return errors.New("控制中心进程不可用")
	}
	if d.NewCloser == nil {
		return errors.New("缺少控制中心归属检查，未打开面板")
	}
	closer, e := d.NewCloser()
	if e != nil {
		return e
	}
	if closer == nil {
		return errors.New("控制中心关闭器不可用，未打开面板")
	}
	defer closer.Release()
	if closer.pid != d.PID {
		return errors.New("控制中心进程不匹配，未打开面板")
	}
	app := axApp(d.PID)
	if app == 0 {
		return errors.New("控制中心不可访问")
	}
	defer cfDrop(app)
	if axTimeout(app, 0.4) != 0 {
		return errors.New("无法设置控制中心响应时限")
	}
	windows, code := axGet(app, "AXWindows")
	if windows != 0 {
		defer cfDrop(windows)
	}
	if code != 0 || windows == 0 || cfType(windows) != cfArrayType() || cfCount(windows) != 0 {
		return errors.New("控制中心正在使用或状态未知；未接管系统面板")
	}
	menu, code := axGet(app, "AXExtrasMenuBar")
	if code != 0 || menu == 0 {
		return errors.New("控制中心菜单不可识别")
	}
	defer cfDrop(menu)
	tree := &axTree{}
	defer tree.close()
	if err = tree.walk(menu, 0); err != nil {
		return err
	}
	button, err := axUnique(tree, func(r uintptr) bool {
		if axString(r, "AXRole") != "AXMenuBarItem" {
			return false
		}
		return axString(r, "AXIdentifier") == "com.apple.menuextra.controlcenter"
	})
	if err != nil {
		return err
	}
	if err = axDo(button, "AXPress"); err != nil {
		return err
	}
	// Keep the exact panel identity. Only close our own still-open panel; a
	// remembered Focus subpanel has no AXCancel action on macOS 15.
	var opened uintptr
	deadline := time.Now().Add(2 * time.Second)
	for {
		opened, _ = axGet(app, "AXWindows")
		if opened != 0 && cfType(opened) == cfArrayType() && cfCount(opened) == 1 {
			break
		}
		if opened != 0 {
			cfDrop(opened)
			opened = 0
		}
		if time.Now().After(deadline) {
			return errors.New("控制中心面板未打开，未切换专注模式")
		}
		time.Sleep(80 * time.Millisecond)
	}
	if opened != 0 {
		defer cfDrop(opened)
	}
	defer func() {
		// Close only our still-open, unchanged panel.
		w, c := axGet(app, "AXWindows")
		if w != 0 {
			defer cfDrop(w)
		}
		window, e := panelWindowToClose(opened, w, c)
		if e != nil {
			err = errors.Join(err, e)
			return
		}
		if window != 0 {
			if e := closer.Close(app, window); e != nil {
				err = errors.Join(err, e)
				return
			}
			until := time.Now().Add(2 * time.Second)
			for {
				remaining, rc := axGet(app, "AXWindows")
				closed := remaining != 0 && rc == 0 && cfType(remaining) == cfArrayType() && cfCount(remaining) == 0
				if remaining != 0 {
					cfDrop(remaining)
				}
				if closed {
					return
				}
				if time.Now().After(until) {
					err = errors.Join(err, errors.New("控制中心面板关闭未确认"))
					return
				}
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
	panel := &focusPanel{app: app, enable: d.Enable}
	// Opening a Focus module with AXPress can toggle DND. Use only its explicitly
	// advertised details action; if unavailable, do not guess or click coordinates.
	if err = panel.details(); err != nil {
		return err
	}
	return fn(panel)
}

type focusPanel struct {
	app    uintptr
	enable func(func() error) error
}

func (p *focusPanel) tree() (*axTree, error) {
	t := &axTree{}
	v, code := axGet(p.app, "AXWindows")
	if v != 0 {
		t.arrays = append(t.arrays, v)
	}
	if code != 0 || v == 0 || cfType(v) != cfArrayType() || cfCount(v) != 1 {
		t.close()
		return nil, errors.New("专注模式面板已关闭或不唯一")
	}
	if err := t.walk(cfAt(v, 0), 0); err != nil {
		t.close()
		return nil, err
	}
	return t, nil
}
func (p *focusPanel) details() error {
	deadline := time.Now().Add(2 * time.Second)
	reason := ""
	requested := false
	for {
		t, err := p.tree()
		if err == nil {
			// Control Center may reopen the last-used details page directly.
			for _, r := range t.refs {
				if axString(r, "AXIdentifier") == "focus-modes-header" {
					t.close()
					return nil
				}
			}
			ref, e := axUnique(t, func(r uintptr) bool {
				d := axString(r, "AXDescription")
				return axString(r, "AXIdentifier") == "controlcenter-focus-modes" || d == "专注模式" || d == "Focus"
			})
			if e == nil && !requested {
				reason = fmt.Sprintf("（入口 %s，操作 %v）", axString(ref, "AXRole"), axActionNames(ref))
				for _, name := range axActionNames(ref) {
					lower := strings.ToLower(name)
					if strings.Contains(lower, "show details") || strings.Contains(name, "显示详细") || strings.Contains(name, "显示更多") {
						e = axDo(ref, name)
						if e != nil {
							t.close()
							return e
						}
						requested = true
						break
					}
				}
			}
			t.close()
		}
		if time.Now().After(deadline) {
			return errors.New("未找到可安全展开的专注模式面板；通知联动未生效" + reason)
		}
		time.Sleep(80 * time.Millisecond)
	}
}
func modeValue(r uintptr) (bool, error) {
	v, code := axGet(r, "AXValue")
	if v != 0 {
		defer cfDrop(v)
	}
	if code != 0 || v == 0 {
		return false, errors.New("专注模式没有明确开关状态")
	}
	if cfType(v) == cfBoolType() {
		return cfBool(v), nil
	}
	if cfType(v) == cfNumberType() {
		var n int64
		if cfNumber(v, 4, &n) && (n == 0 || n == 1) {
			return n == 1, nil
		}
	}
	return false, errors.New("专注模式开关状态无法识别")
}
func (p *focusPanel) modes() (map[string]bool, *axTree, map[string]uintptr, error) {
	t, err := p.tree()
	if err != nil {
		return nil, nil, nil, err
	}
	values, refs := map[string]bool{}, map[string]uintptr{}
	for _, r := range t.refs {
		if !strings.HasPrefix(axString(r, "AXIdentifier"), "focus-mode-activity-") {
			continue
		}
		name := axString(r, "AXTitle")
		if name == "" {
			name = axString(r, "AXDescription")
		}
		if name == "" || refs[name] != 0 {
			t.close()
			return nil, nil, nil, errors.New("专注模式名称为空或不唯一")
		}
		v, e := modeValue(r)
		if e != nil {
			t.close()
			return nil, nil, nil, e
		}
		values[name], refs[name] = v, r
	}
	if len(values) == 0 {
		t.close()
		return nil, nil, nil, errors.New("未读取到明确的专注模式开关")
	}
	return values, t, refs, nil
}
func (p *focusPanel) Modes() (map[string]bool, error) {
	v, t, _, err := p.modes()
	if t != nil {
		t.close()
	}
	return v, err
}
func (p *focusPanel) Set(name string, on bool) error {
	v, t, refs, err := p.modes()
	if err != nil {
		return err
	}
	defer t.close()
	current, ok := v[name]
	if !ok {
		return errors.New("专注模式已移除")
	}
	if current == on {
		return errors.New("专注模式已被其他操作改变；未再次切换")
	}
	for n, active := range v {
		if active && (on || n != name) {
			return errors.New("已有其他专注模式；未覆盖")
		}
	}
	press := func() error { return axDo(refs[name], "AXPress") }
	if on {
		if p.enable == nil {
			return errors.New("没有会话切换保护，未开启系统专注模式")
		}
		err = p.enable(press)
	} else {
		err = press()
	}
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		modes, e := p.Modes()
		if e == nil {
			if actual, exists := modes[name]; exists && actual == on {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("专注模式切换结果未确认，需核对系统状态")
		}
		time.Sleep(80 * time.Millisecond)
	}
}

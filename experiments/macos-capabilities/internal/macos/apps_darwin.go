//go:build darwin

// Package macos implements only the identity-checked probe boundary.
package macos

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"focus.local/zhuanzhuli/pkg/journal"
	probe "focus.local/zhuanzhuli/pkg/observe"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var once sync.Once
var initErr error
var screenAccess func() bool
var windowDescriptions func(uintptr) uintptr
var cfArrayCreate func(uintptr, *uintptr, int64, uintptr) uintptr
var cfRelease func(uintptr)

func Init() error {
	once.Do(func() {
		_, initErr = purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if initErr != nil {
			return
		}
		var lib uintptr
		lib, initErr = purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if initErr != nil {
			return
		}
		purego.RegisterLibFunc(&screenAccess, lib, "CGPreflightScreenCaptureAccess")
		purego.RegisterLibFunc(&windowDescriptions, lib, "CGWindowListCreateDescriptionFromArray")
		core, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			initErr = err
			return
		}
		purego.RegisterLibFunc(&cfRelease, core, "CFRelease")
		purego.RegisterLibFunc(&cfArrayCreate, core, "CFArrayCreate")
	})
	return initErr
}

var selectors sync.Map

func sel(name string) objc.SEL {
	if s, ok := selectors.Load(name); ok {
		return s.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selectors.Store(name, s)
	return s
}
func send(id objc.ID, name string, args ...any) objc.ID { return id.Send(sel(name), args...) }
func stringOf(id objc.ID) string {
	if id == 0 {
		return ""
	}
	return objc.Send[string](id, sel("UTF8String"))
}
func ns(s string) objc.ID {
	return send(objc.ID(objc.GetClass("NSString")), "stringWithUTF8String:", s)
}
func application(pid int) objc.ID {
	return send(objc.ID(objc.GetClass("NSRunningApplication")), "runningApplicationWithProcessIdentifier:", int32(pid))
}
func describe(app objc.ID, label, token string) (journal.Ref, error) {
	if app == 0 || objc.Send[bool](app, sel("isTerminated")) {
		return journal.Ref{}, errors.New("identity-changed: application exited")
	}
	date := send(app, "launchDate")
	if date == 0 {
		return journal.Ref{}, errors.New("unobservable: launch date")
	}
	path := stringOf(send(send(app, "bundleURL"), "path"))
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return journal.Ref{}, err
	}
	return journal.Ref{Label: label, Token: token, PID: int(objc.Send[int32](app, sel("processIdentifier"))), Launched: objc.Send[float64](date, sel("timeIntervalSince1970")), Path: real, BundleID: stringOf(send(app, "bundleIdentifier"))}, nil
}
func Self(label, token string) (journal.Ref, error) {
	if err := Init(); err != nil {
		return journal.Ref{}, err
	}
	return describe(application(os.Getpid()), label, token)
}
func validate(r journal.Ref) (objc.ID, error) {
	if err := Init(); err != nil {
		return 0, err
	}
	if r.Label != "A" && r.Label != "B" {
		return 0, errors.New("invalid: not a fixture")
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	// .../.build/bundles/Host.app/Contents/MacOS/Host, sibling fixtures only.
	base := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(exe))))
	wanted, err := filepath.EvalSymlinks(filepath.Join(base, "Fixture"+r.Label+".app"))
	if err != nil {
		return 0, err
	}
	if r.Token == "" || r.Path != wanted || r.BundleID != "local.focusprobe.fixture."+map[string]string{"A": "a", "B": "b"}[r.Label] {
		return 0, errors.New("identity-changed: fixture boundary")
	}
	app := application(r.PID)
	got, err := describe(app, r.Label, r.Token)
	if err != nil {
		return 0, err
	}
	if got != r {
		return 0, errors.New("identity-changed: instance replaced")
	}
	return app, nil
}

type State struct {
	Hidden bool `json:"hidden"`
	Active bool `json:"active"`
}

// WindowState inspects only the caller-owned NSWindow handle on the main thread.
func WindowState(handle uintptr) map[string]any {
	w := objc.ID(handle)
	return map[string]any{"visible": objc.Send[bool](w, sel("isVisible")), "onActiveSpace": objc.Send[bool](w, sel("isOnActiveSpace")), "occlusionState": objc.Send[uint64](w, sel("occlusionState")), "level": objc.Send[int64](w, sel("level"))}
}

func Inspect(r journal.Ref) (State, error) {
	app, err := validate(r)
	if err != nil {
		return State{}, err
	}
	return State{objc.Send[bool](app, sel("isHidden")), objc.Send[bool](app, sel("isActive"))}, nil
}
func RequestHidden(r journal.Ref, hidden bool) error {
	app, err := validate(r)
	if err != nil {
		return err
	}
	name := "unhide"
	if hidden {
		name = "hide"
	}
	if !objc.Send[bool](app, sel(name)) {
		return probe.Unacknowledged
	}
	return nil
}
func RequestActivate(r journal.Ref) error {
	app, err := validate(r)
	if err != nil {
		return err
	}
	if !objc.Send[bool](app, sel("activateWithOptions:"), uint64(0)) {
		return probe.Unacknowledged
	}
	return nil
}
func SelfState() State {
	app := application(os.Getpid())
	return State{objc.Send[bool](app, sel("isHidden")), objc.Send[bool](app, sel("isActive"))}
}
func Windows(handle uintptr) []int {
	if handle == 0 {
		return []int{}
	}
	win := objc.ID(handle)
	id := int(objc.Send[int64](win, sel("windowNumber")))
	if id > 0 && objc.Send[bool](win, sel("isVisible")) {
		return []int{id}
	}
	return []int{}
}
func CaptureAllowed() (bool, error) {
	if err := Init(); err != nil {
		return false, err
	}
	return screenAccess(), nil
}
func Owner(window int) (int, error) {
	if err := Init(); err != nil {
		return 0, err
	}
	// CGWindowListCopyWindowInfo's include flag does not select one metadata
	// row. Ask only for the known own window ID, not a global window list.
	id := uintptr(window)
	ids := cfArrayCreate(0, &id, 1, 0)
	if ids == 0 {
		return 0, errors.New("unobservable: window selection")
	}
	defer cfRelease(ids)
	arr := windowDescriptions(ids)
	if arr == 0 {
		return 0, errors.New("unobservable: window")
	}
	defer cfRelease(arr)
	if objc.Send[uint64](objc.ID(arr), sel("count")) != 1 {
		return 0, errors.New("unobservable: window count")
	}
	dict := send(objc.ID(arr), "objectAtIndex:", uint64(0))
	return int(objc.Send[int32](send(dict, "objectForKey:", ns("kCGWindowOwnerPID")), sel("intValue"))), nil
}

// One callback class per process; observers are explicitly removed on shutdown.
func Observe(refs []journal.Ref, fn func(string, string)) (func(), error) {
	if err := Init(); err != nil {
		return nil, err
	}
	class, err := objc.RegisterClass("FocusProbeObserver", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{{Cmd: sel("changed:"), Fn: func(_ objc.ID, _ objc.SEL, n objc.ID) {
		name := stringOf(send(n, "name"))
		app := send(send(n, "userInfo"), "objectForKey:", ns("NSWorkspaceApplicationKey"))
		if app == 0 {
			return
		}
		pid := int(objc.Send[int32](app, sel("processIdentifier")))
		for _, r := range refs {
			if r.PID == pid {
				if _, err := validate(r); err == nil {
					fn(r.Label, name)
				}
			}
		}
	}}})
	if err != nil {
		return nil, err
	}
	observer := send(objc.ID(class), "new")
	center := send(send(objc.ID(objc.GetClass("NSWorkspace")), "sharedWorkspace"), "notificationCenter")
	for _, name := range []string{"NSWorkspaceDidActivateApplicationNotification", "NSWorkspaceDidHideApplicationNotification", "NSWorkspaceDidUnhideApplicationNotification"} {
		send(center, "addObserver:selector:name:object:", observer, sel("changed:"), ns(name), objc.ID(0))
	}
	return func() { send(center, "removeObserver:", observer); send(observer, "release") }, nil
}

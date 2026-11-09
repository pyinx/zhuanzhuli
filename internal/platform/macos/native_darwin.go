//go:build darwin

// Package macos contains the AppKit boundary; all functions run on the main thread.
package macos

import (
	"errors"
	"fmt"
	"focus.local/zhuanzhuli/internal/store"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

type ID = objc.ID
type Rect struct{ X, Y, W, H float64 }

func R(x, y, w, h float64) Rect { return Rect{X: x, Y: y, W: w, H: h} }

var initRect func(ID, objc.SEL, Rect) ID
var setRect func(ID, objc.SEL, Rect)
var frameWindow func(ID, objc.SEL, Rect, bool)
var initWindow func(ID, objc.SEL, Rect, uint64, uint64, bool) ID
var getRect func(ID, objc.SEL) Rect
var indexedRect func(ID, objc.SEL, int64) Rect
var convertRect func(ID, objc.SEL, Rect, ID) Rect
var transformRect func(ID, objc.SEL, Rect) Rect
var setSize func(ID, objc.SEL, Point)
var getSize func(ID, objc.SEL) Point

type Point struct{ X, Y float64 }

var setPoint func(ID, objc.SEL, Point)
var targets = map[ID]func(){}
var actionTarget ID

func S(s string) objc.SEL                     { return objc.RegisterName(s) }
func Send(id ID, name string, args ...any) ID { return id.Send(S(name), args...) }
func NS(s string) ID                          { return Send(ID(objc.GetClass("NSString")), "stringWithUTF8String:", s) }
func Str(id ID) string {
	if id == 0 {
		return ""
	}
	return objc.Send[string](id, S("UTF8String"))
}
func Class(s string) ID { return ID(objc.GetClass(s)) }
func Init() error {
	for _, fw := range []string{"AppKit", "AVFoundation", "AVKit"} {
		if _, e := purego.Dlopen("/System/Library/Frameworks/"+fw+".framework/"+fw, purego.RTLD_NOW|purego.RTLD_GLOBAL); e != nil {
			return e
		}
	}
	lib, e := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if e != nil {
		return e
	}
	addr, e := purego.Dlsym(lib, "objc_msgSend")
	if e != nil {
		return e
	}
	rectReturn := addr
	if runtime.GOARCH == "amd64" {
		// Intel returns CGRect (32 bytes) through a caller-provided buffer.
		// Only methods returning Rect need the matching Objective-C entry point.
		rectReturn, e = purego.Dlsym(lib, "objc_msgSend_stret")
		if e != nil {
			return e
		}
	}
	purego.RegisterFunc(&initRect, addr)
	purego.RegisterFunc(&setRect, addr)
	purego.RegisterFunc(&frameWindow, addr)
	purego.RegisterFunc(&initWindow, addr)
	purego.RegisterFunc(&getRect, rectReturn)
	purego.RegisterFunc(&indexedRect, rectReturn)
	purego.RegisterFunc(&convertRect, rectReturn)
	purego.RegisterFunc(&transformRect, rectReturn)
	purego.RegisterFunc(&setSize, addr)
	purego.RegisterFunc(&getSize, addr)
	purego.RegisterFunc(&setPoint, addr)
	purego.RegisterFunc(&showDatePopover, addr)
	c, e := objc.RegisterClass("FocusNativeActions", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{{Cmd: S("perform:"), Fn: func(_ ID, _ objc.SEL, sender ID) {
		if f := targets[sender]; f != nil {
			f()
		}
	}}, {Cmd: S("controlTextDidChange:"), Fn: func(_ ID, _ objc.SEL, note ID) {
		if f := targets[Send(note, "object")]; f != nil {
			f()
		}
	}}})
	if e != nil {
		return e
	}
	actionTarget = Send(ID(c), "new")
	return nil
}
func View(class string, r Rect) ID {
	return initRect(Send(Class(class), "alloc"), S("initWithFrame:"), r)
}
func Frame(id ID, r Rect)  { setRect(id, S("setFrame:"), r) }
func Bounds(id ID) Rect    { return getRect(id, S("bounds")) }
func Add(parent, child ID) { Send(parent, "addSubview:", child) }
func Release(id ID) {
	if id != 0 {
		Send(id, "release")
	}
}
func Bind(id ID, fn func()) {
	targets[id] = fn
	Send(id, "setTarget:", actionTarget)
	Send(id, "setAction:", S("perform:"))
}
func UnbindTree(id ID) {
	a := Send(id, "subviews")
	for i := uint64(0); i < objc.Send[uint64](a, S("count")); i++ {
		UnbindTree(Send(a, "objectAtIndex:", i))
	}
	delete(targets, id)
}
func Label(parent ID, text string, r Rect, size float64) ID {
	id := Send(Class("NSTextField"), "wrappingLabelWithString:", NS(text))
	Frame(id, r)
	Send(id, "setFont:", Send(Class("NSFont"), "systemFontOfSize:", size))
	Add(parent, id)
	return id
}
func Secondary(id ID) { Send(id, "setTextColor:", Send(Class("NSColor"), "secondaryLabelColor")) }

// Native typography and grouped surfaces use semantic AppKit appearances.
// Geometry belongs to each window; these shared style values have one owner.
const GroupRadius = 8.0

// CanvasColor owns the content/list background role for every ordinary page.
// Dynamic NSColor instances follow effectiveAppearance, including contrast.
func CanvasColor() ID { return Send(Class("NSColor"), "textBackgroundColor") }

func Heading(parent ID, text string, r Rect, size float64) ID {
	id := Label(parent, text, r, size)
	Send(id, "setFont:", Send(Class("NSFont"), "systemFontOfSize:weight:", size, float64(0.23)))
	return id
}
func Group(parent ID, r Rect) {
	id := View("NSBox", r)
	Send(id, "setBoxType:", uint64(4)) // NSBoxCustom
	Send(id, "setTitlePosition:", uint64(0))
	Send(id, "setBorderWidth:", float64(0))
	Send(id, "setCornerRadius:", GroupRadius)
	Send(id, "setFillColor:", CanvasColor())
	Add(parent, id)
	Release(id)
}
func ObserveText(id ID, change func()) {
	targets[id] = change
	Send(id, "setDelegate:", actionTarget)
}
func KeyLoop(handle uintptr) {
	Send(ID(handle), "setAutorecalculatesKeyViewLoop:", true)
	Send(ID(handle), "recalculateKeyViewLoop")
}
func Segmented(parent ID, labels []string, index int, r Rect, change func(int)) ID {
	id := View("NSSegmentedControl", r)
	Send(id, "setSegmentCount:", int64(len(labels)))
	Send(id, "setTrackingMode:", int64(0))
	for i, title := range labels {
		Send(id, "setLabel:forSegment:", NS(title), int64(i))
		Send(id, "setWidth:forSegment:", float64(0), int64(i))
	}
	Send(id, "setSelectedSegment:", int64(index))
	Bind(id, func() { change(int(objc.Send[int64](id, S("selectedSegment")))) })
	Add(parent, id)
	Release(id)
	return id
}
func LimitGoalLabel(id ID) {
	Send(id, "setMaximumNumberOfLines:", int64(2))
	Send(Send(id, "cell"), "setTruncatesLastVisibleLine:", true)
	Send(id, "setToolTip:", NS(Value(id)))
}
func Text(id ID, s string)  { Send(id, "setStringValue:", NS(s)) }
func Value(id ID) string    { return Str(Send(id, "stringValue")) }
func Enabled(id ID, b bool) { Send(id, "setEnabled:", b) }
func Hidden(id ID, b bool)  { Send(id, "setHidden:", b) }
func Field(parent ID, value, placeholder string, r Rect) ID {
	id := Send(Class("NSTextField"), "textFieldWithString:", NS(value))
	Send(id, "setPlaceholderString:", NS(placeholder))
	Frame(id, r)
	Send(id, "setFont:", Send(Class("NSFont"), "systemFontOfSize:", float64(15)))
	Send(id, "setBezelStyle:", uint64(1))
	Add(parent, id)
	return id
}
func Button(parent ID, title string, r Rect, fn func()) ID {
	id := Send(Class("NSButton"), "buttonWithTitle:target:action:", NS(title), actionTarget, S("perform:"))
	Frame(id, r)
	Send(id, "setBezelStyle:", uint64(1))
	Send(id, "setFont:", Send(Class("NSFont"), "systemFontOfSize:", float64(13)))
	targets[id] = fn
	Add(parent, id)
	return id
}
func Primary(id ID) { Send(id, "setKeyEquivalent:", NS("\r")); Send(id, "setBezelStyle:", uint64(1)) }
func Check(parent ID, title string, on bool, r Rect, fn func(bool)) ID {
	id := Button(parent, title, r, nil)
	Send(id, "setButtonType:", uint64(3))
	Send(id, "setState:", int64(0))
	if on {
		Send(id, "setState:", int64(1))
	}
	targets[id] = func() { fn(Checked(id)) }
	return id
}
func Checked(id ID) bool { return objc.Send[int64](id, S("state")) == 1 }
func Popup(parent ID, titles []string, index int, r Rect, fn func(int)) ID {
	id := View("NSPopUpButton", r)
	for _, s := range titles {
		Send(id, "addItemWithTitle:", NS(s))
	}
	Send(id, "selectItemAtIndex:", int64(index))
	Bind(id, func() { fn(int(objc.Send[int64](id, S("indexOfSelectedItem")))) })
	Add(parent, id)
	Release(id)
	return id
}
func Slider(parent ID, value float64, r Rect, fn func(float64)) ID {
	id := Send(Class("NSSlider"), "sliderWithValue:minValue:maxValue:target:action:", value, float64(0), float64(1), actionTarget, S("perform:"))
	Frame(id, r)
	targets[id] = func() { fn(objc.Send[float64](id, S("doubleValue"))) }
	Send(id, "setAccessibilityLabel:", NS("环境声音音量"))
	Add(parent, id)
	return id
}
func MinuteSlider(parent ID, value int, marks []int, change func(int)) (ID, ID) {
	if objc.GetClass("FocusMinuteSlider") == 0 {
		_, err := objc.RegisterClass("FocusMinuteSliderCell", objc.GetClass("NSSliderCell"), nil, nil, []objc.MethodDef{
			// AppKit uses all 180 positions for snapping. The adjacent
			// ruler draws only the four useful marks.
			{Cmd: S("drawTickMarks"), Fn: func(ID, objc.SEL) {}},
		})
		if err != nil {
			panic(err)
		}
		_, err = objc.RegisterClass("FocusMinuteSlider", objc.GetClass("NSSlider"), nil, nil, []objc.MethodDef{
			{Cmd: S("mouseDown:"), Fn: func(id ID, sel objc.SEL, event ID) {
				Send(Send(id, "window"), "makeFirstResponder:", id)
				id.SendSuper(sel, event)
			}},
			{Cmd: S("keyDown:"), Fn: func(id ID, sel objc.SEL, event ID) {
				key := objc.Send[uint16](event, S("keyCode"))
				switch key {
				case 123, 125:
					Send(id, "setIntegerValue:", max(int64(1), objc.Send[int64](id, S("integerValue"))-1))
				case 124, 126:
					Send(id, "setIntegerValue:", min(int64(180), objc.Send[int64](id, S("integerValue"))+1))
				default:
					id.SendSuper(sel, event)
					return
				}
				if fn := targets[id]; fn != nil {
					fn()
				}
			}},
		})
		if err != nil {
			panic(err)
		}
		_, err = objc.RegisterClass("FocusMinuteRuler", objc.GetClass("NSView"), nil, nil, []objc.MethodDef{
			{Cmd: S("layout"), Fn: func(id ID, sel objc.SEL) {
				id.SendSuper(sel)
				width := Bounds(id).W
				children := Send(id, "subviews")
				for i := uint64(0); i < objc.Send[uint64](children, S("count")); i++ {
					tick := Send(children, "objectAtIndex:", i)
					minute, _ := strconv.Atoi(Str(Send(tick, "accessibilityIdentifier")))
					x := 9 + max(0, width-18)*float64(minute-1)/179
					Frame(tick, R(x-1, 1, 2, 5))
				}
			}},
		})
		if err != nil {
			panic(err)
		}
	}
	id := View("FocusMinuteSlider", R(0, 0, 260, 28))
	cell := Send(Class("FocusMinuteSliderCell"), "new")
	Send(id, "setCell:", cell)
	Release(cell)
	Send(id, "setMinValue:", float64(1))
	Send(id, "setMaxValue:", float64(180))
	Send(id, "setNumberOfTickMarks:", int64(180))
	Send(id, "setAllowsTickMarkValuesOnly:", true)
	Send(id, "setContinuous:", true)
	Send(id, "setIntegerValue:", int64(value))
	Send(id, "setAccessibilityLabel:", NS("专注时长，分钟"))
	Bind(id, func() {
		n := int(objc.Send[float64](id, S("doubleValue")) + .5)
		Send(id, "setDoubleValue:", float64(n))
		change(n)
	})
	Add(parent, id)
	Release(id)
	ruler := View("FocusMinuteRuler", R(0, 0, 260, 6))
	for _, minute := range marks {
		tick := View("NSBox", R(0, 1, 2, 5))
		Send(tick, "setBoxType:", uint64(4))
		Send(tick, "setTitlePosition:", uint64(0))
		Send(tick, "setBorderWidth:", float64(0))
		Send(tick, "setFillColor:", Send(Class("NSColor"), "secondaryLabelColor"))
		Send(tick, "setAccessibilityIdentifier:", NS(strconv.Itoa(minute)))
		Send(tick, "setToolTip:", NS(fmt.Sprintf("%d 分钟", minute)))
		Add(ruler, tick)
		Release(tick)
	}
	Add(parent, ruler)
	Release(ruler)
	return id, ruler
}
func Image(parent ID, path string, r Rect) ID {
	id := View("NSImageView", r)
	img := Send(Send(Class("NSImage"), "alloc"), "initWithContentsOfFile:", NS(path))
	if img != 0 {
		Send(id, "setImage:", img)
		Release(img)
	}
	Send(id, "setImageScaling:", uint64(3))
	Send(id, "setWantsLayer:", true)
	Send(Send(id, "layer"), "setCornerRadius:", GroupRadius)
	Send(Send(id, "layer"), "setMasksToBounds:", true)
	Send(id, "setAccessibilityLabel:", NS("自然场景预览"))
	Add(parent, id)
	Release(id)
	return id
}
func Backdrop(parent ID, path string, r Rect) bool {
	view := View("NSView", r)
	Send(view, "setWantsLayer:", true)
	layer := Send(view, "layer")
	image := Send(Send(Class("NSImage"), "alloc"), "initWithContentsOfFile:", NS(path))
	loaded := false
	if image != 0 {
		cg := Send(image, "CGImageForProposedRect:context:hints:", uintptr(0), ID(0), ID(0))
		loaded = cg != 0
		Send(layer, "setContents:", cg)
		Send(layer, "setContentsGravity:", NS("resizeAspectFill"))
		Send(layer, "setMasksToBounds:", true)
		Release(image)
	}
	Add(parent, view)
	Release(view)
	return loaded
}
func Scroll(parent ID, r Rect, documentHeight float64) ID {
	scroll := View("NSScrollView", r)
	Send(scroll, "setHasVerticalScroller:", true)
	Send(scroll, "setDrawsBackground:", false)
	doc := View("NSView", Rect{0, 0, r.W - 18, documentHeight})
	Send(scroll, "setDocumentView:", doc)
	top := documentHeight - r.H
	if top < 0 {
		top = 0
	}
	clip := Send(scroll, "contentView")
	setPoint(clip, S("scrollToPoint:"), Point{X: 0, Y: top})
	Send(scroll, "reflectScrolledClipView:", clip)
	Add(parent, scroll)
	Release(scroll)
	Release(doc)
	return doc
}

// Surface overlays a native NSView on MyGo's empty GPU surface. Window lifecycle stays in MyGo.
func Surface(handle uintptr) ID {
	container := Send(ID(handle), "contentView")
	r := Bounds(container)
	v := View("NSVisualEffectView", Rect{0, 0, r.W, r.H})
	Send(v, "setMaterial:", int64(21)) // NSVisualEffectMaterialWindowBackground
	Send(v, "setBlendingMode:", int64(1))
	Send(v, "setState:", int64(0))
	Send(v, "setAutoresizingMask:", uint64(18))
	Add(container, v)
	Release(v)
	return v
}

var ErrSelectionCancelled = errors.New("application selection cancelled")
var ErrActivationDenied = errors.New("系统未允许切换到专注应用，请从 Dock 选择该应用")

func ChooseApp() (store.App, error) {
	// Use the framework's modal runner so a quit request is resumed after
	// abortModal unwinds. A direct runModal bypasses that lifecycle.
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "选择允许使用的应用", DefaultPath: "/Applications"})
	if err != nil {
		return store.App{}, err
	}
	if len(paths) == 0 {
		return store.App{}, ErrSelectionCancelled
	}
	return AppAt(paths[0])
}
func AppAt(path string) (store.App, error) {
	path, e := filepath.EvalSymlinks(path)
	if e != nil {
		return store.App{}, e
	}
	if filepath.Ext(path) != ".app" {
		return store.App{}, errors.New("请选择 .app 应用程序")
	}
	b := Send(Class("NSBundle"), "bundleWithPath:", NS(path))
	bid := Str(Send(b, "bundleIdentifier"))
	if bid == "" {
		return store.App{}, errors.New("无法读取应用标识")
	}
	name := Str(Send(b, "objectForInfoDictionaryKey:", NS("CFBundleDisplayName")))
	if name == "" {
		name = filepath.Base(path[:len(path)-4])
	}
	return store.App{ID: bid, Name: name, Path: path}, nil
}
func Describe(app ID) (journal.Ref, error) {
	return describeSource(app, journal.IdentityBSDStart)
}

func currentApplicationURL() ID {
	app := Send(Class("NSRunningApplication"), "currentApplication")
	url := Send(app, "bundleURL")
	if url == 0 || !objc.Send[bool](url, S("isFileURL")) {
		return 0
	}
	return url
}

func CurrentApplicationPath() string {
	path, err := bundlePath(Str(Send(currentApplicationURL(), "path")))
	if err != nil {
		return ""
	}
	return path
}

func RevealCurrentApplication() {
	if url := currentApplicationURL(); url != 0 {
		urls := Send(Class("NSArray"), "arrayWithObject:", url)
		Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "activateFileViewerSelectingURLs:", urls)
	}
}

// NSRunningApplication's bundle URL is an identity value, not a request to scan
// every application's directory. Avoid filesystem reads (and folder prompts)
// on the main thread. Normalize the macOS /var alias without traversing files.
func bundlePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("应用位置不可识别")
	}
	path = filepath.Clean(path)
	if path == "/" {
		return "", errors.New("应用位置不可识别")
	}
	if strings.HasPrefix(path, "/var/") {
		path = "/private" + path
	}
	return path, nil
}

type AppIdentityError struct {
	PID                   int
	BundleID, Path, Label string
}

func (e *AppIdentityError) Error() string {
	return e.Label + "：无法确认应用实例，未操作该应用"
}
func (e *AppIdentityError) Key() string {
	return fmt.Sprintf("identity:%s:%d:%s", e.BundleID, e.PID, e.Path)
}
func identityIssue(app ID) *AppIdentityError {
	return &AppIdentityError{int(objc.Send[int32](app, S("processIdentifier"))), Str(Send(app, "bundleIdentifier")), Str(Send(Send(app, "bundleURL"), "path")), Str(Send(app, "localizedName"))}
}
func Running() ([]journal.Ref, error) {
	arr := Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "runningApplications")
	out := []journal.Ref{}
	var errs []error
	for i := uint64(0); i < objc.Send[uint64](arr, S("count")); i++ {
		a := Send(arr, "objectAtIndex:", i)
		if objc.Send[int64](a, S("activationPolicy")) != 0 {
			continue
		}
		if int(objc.Send[int32](a, S("processIdentifier"))) == os.Getpid() {
			continue
		}
		system := SystemInteraction(Str(Send(a, "bundleIdentifier")))
		r, e := Describe(a)
		if e == nil && r.PID != os.Getpid() {
			out = append(out, r)
		} else if e != nil && !system && !errors.Is(e, ErrInstanceGone) {
			errs = append(errs, identityIssue(a))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, errors.Join(errs...)
}
func Instance(r journal.Ref) (ID, error) {
	app := Send(Class("NSRunningApplication"), "runningApplicationWithProcessIdentifier:", int32(r.PID))
	got, e := describeSource(app, r.IdentitySource)
	if e != nil {
		return 0, e
	}
	if !sameInstance(got, r) {
		return 0, ErrInstanceChanged
	}
	return app, nil
}
func IsHidden(r journal.Ref) (bool, error) {
	a, e := Instance(r)
	if e != nil {
		return false, e
	}
	return objc.Send[bool](a, S("isHidden")), nil
}
func Hide(r journal.Ref, hidden bool) error {
	a, e := Instance(r)
	if e != nil {
		return e
	}
	selector := "unhide"
	if hidden {
		selector = "hide"
	}
	Send(a, selector)
	return nil
}
func Activate(r journal.Ref) error {
	a, e := Instance(r)
	if e != nil {
		return e
	}
	// Public cooperative activation (macOS 14+). Do not first activate our
	// hidden window merely to give the foreground away again. A successful
	// request still requires the caller to observe the target becoming active.
	app := Send(Class("NSApplication"), "sharedApplication")
	Send(app, "yieldActivationToApplication:", a)
	from := Send(Class("NSRunningApplication"), "currentApplication")
	if !objc.Send[bool](a, S("activateFromApplication:options:"), from, uint64(0)) {
		return ErrActivationDenied
	}
	return nil
}
func Active(r journal.Ref) (bool, error) {
	a, e := Instance(r)
	if e != nil {
		return false, e
	}
	return objc.Send[bool](a, S("isActive")), nil
}
func SelfActive() bool {
	return objc.Send[bool](Send(Class("NSRunningApplication"), "currentApplication"), S("isActive"))
}
func OpenApp(a store.App) error {
	current, e := AppAt(a.Path)
	if e != nil {
		return e
	}
	if current.ID != a.ID {
		return errors.New("应用标识已改变，请重新选择")
	}
	url := Send(Class("NSURL"), "fileURLWithPath:", NS(a.Path))
	if !objc.Send[bool](Send(Class("NSWorkspace"), "sharedWorkspace"), S("openURL:"), url) {
		return errors.New("无法打开所选应用")
	}
	return nil
}
func AppForID(id string) (store.App, error) {
	url := Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "URLForApplicationWithBundleIdentifier:", NS(id))
	if url == 0 {
		return store.App{}, errors.New("未找到本次允许的应用")
	}
	a, e := AppAt(Str(Send(url, "path")))
	if e != nil {
		return store.App{}, e
	}
	if a.ID != id {
		return store.App{}, errors.New("应用标识已改变")
	}
	return a, nil
}
func SystemInteraction(id string) bool {
	switch id {
	case "com.yunsiweilai.zhuanzhuli", "com.apple.finder", "com.apple.systempreferences", "com.apple.loginwindow", "com.apple.SecurityAgent", "com.apple.notificationcenterui", "com.apple.controlcenter":
		return true
	}
	return false
}
func Observe(fn func(journal.Ref, string), unobservable func(*AppIdentityError)) func() {
	c, e := objc.RegisterClass("FocusWorkspaceObserver", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{{Cmd: S("changed:"), Fn: func(_ ID, _ objc.SEL, n ID) {
		app := Send(Send(n, "userInfo"), "objectForKey:", NS("NSWorkspaceApplicationKey"))
		if app == 0 || objc.Send[int64](app, S("activationPolicy")) != 0 {
			return
		}
		if int(objc.Send[int32](app, S("processIdentifier"))) == os.Getpid() || objc.Send[bool](app, S("isTerminated")) || SystemInteraction(Str(Send(app, "bundleIdentifier"))) {
			return
		}
		r, e := Describe(app)
		if e == nil {
			fn(r, Str(Send(n, "name")))
		} else {
			unobservable(identityIssue(app))
		}
	}}})
	if e != nil {
		panic(e)
	}
	observer := Send(ID(c), "new")
	center := Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "notificationCenter")
	for _, n := range []string{"NSWorkspaceDidActivateApplicationNotification", "NSWorkspaceDidLaunchApplicationNotification", "NSWorkspaceDidUnhideApplicationNotification", "NSWorkspaceDidHideApplicationNotification"} {
		Send(center, "addObserver:selector:name:object:", observer, S("changed:"), NS(n), ID(0))
	}
	return func() { Send(center, "removeObserver:", observer); Release(observer) }
}

type Audio struct{ Player ID }

func (a *Audio) Stop() {
	if a.Player != 0 {
		Send(a.Player, "stop")
		Release(a.Player)
		a.Player = 0
	}
}
func (a *Audio) Play(path string, volume float64) error {
	return a.play(path, volume, -1)
}
func (a *Audio) PlayOnce(path string, volume float64) error { return a.play(path, volume, 0) }
func (a *Audio) load(path string) error {
	a.Stop()
	url := Send(Class("NSURL"), "fileURLWithPath:", NS(path))
	a.Player = Send(Send(Class("AVAudioPlayer"), "alloc"), "initWithContentsOfURL:error:", url, uintptr(0))
	if a.Player == 0 {
		return fmt.Errorf("环境声音资源无法打开")
	}
	if !objc.Send[bool](a.Player, S("prepareToPlay")) {
		a.Stop()
		return errors.New("环境声音无法解码")
	}
	return nil
}

// ValidateAudio uses the same decoder as playback without starting a sound.
func ValidateAudio(path string) error {
	var audio Audio
	defer audio.Stop()
	return audio.load(path)
}

func (a *Audio) play(path string, volume float64, loops int64) error {
	if err := a.load(path); err != nil {
		return err
	}
	Send(a.Player, "setNumberOfLoops:", loops)
	Send(a.Player, "setVolume:", float32(volume))
	if !objc.Send[bool](a.Player, S("play")) {
		a.Stop()
		return errors.New("无法播放环境声音")
	}
	return nil
}
func (a *Audio) Volume(v float64) {
	if a.Player != 0 {
		Send(a.Player, "setVolume:", float32(v))
	}
}
func ReducedMotion() bool {
	return objc.Send[bool](Send(Class("NSWorkspace"), "sharedWorkspace"), S("accessibilityDisplayShouldReduceMotion"))
}
func OpenSettings() {
	Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "openURL:", Send(Class("NSURL"), "URLWithString:", NS("x-apple.systempreferences:com.apple.Focus-Settings.extension")))
}

// SymbolPNG renders a system-owned image for MyGo's existing template tray API.
func SymbolPNG(name string) ([]byte, error) {
	image := Send(Class("NSImage"), "imageWithSystemSymbolName:accessibilityDescription:", NS(name), NS("专注"))
	if image == 0 {
		return nil, errors.New("系统图形不可用")
	}
	image = Send(image, "copy")
	defer Release(image)
	setSize(image, S("setSize:"), Point{16, 16})
	rep := Send(Class("NSBitmapImageRep"), "imageRepWithData:", Send(image, "TIFFRepresentation"))
	if rep == 0 {
		return nil, errors.New("系统图形无法转换")
	}
	data := Send(rep, "representationUsingType:properties:", uint64(4), Send(Class("NSDictionary"), "dictionary"))
	length := objc.Send[uint64](data, S("length"))
	if length == 0 || length > 1024*1024 {
		return nil, errors.New("系统图形数据无效")
	}
	pointer := objc.Send[*byte](data, S("bytes"))
	return append([]byte(nil), unsafe.Slice(pointer, int(length))...), nil
}

func KeyWindow() ID { return Send(Send(Class("NSApplication"), "sharedApplication"), "keyWindow") }
func EditingText(window ID) bool {
	r := Send(window, "firstResponder")
	return r != 0 && objc.Send[bool](r, S("isKindOfClass:"), Class("NSTextView")) && objc.Send[bool](r, S("isEditable"))
}
func NativeUndo() {
	Send(Send(Class("NSApplication"), "sharedApplication"), "sendAction:to:from:", S("undo:"), ID(0), ID(0))
}

// IME marked text retains Escape; other Escape actions stay inside this app.
func Escape(fn func(uintptr) bool) func() {
	block := objc.NewBlock(func(_ objc.Block, e ID) ID {
		if objc.Send[uint16](e, S("keyCode")) != 53 {
			return e
		}
		w := Send(e, "window")
		responder := Send(w, "firstResponder")
		if responder != 0 && objc.Send[bool](responder, S("respondsToSelector:"), S("hasMarkedText")) && objc.Send[bool](responder, S("hasMarkedText")) {
			return e
		}
		if fn(uintptr(w)) {
			return 0
		}
		return e
	})
	monitor := Send(Class("NSEvent"), "addLocalMonitorForEventsMatchingMask:handler:", uint64(1<<10), uintptr(block))
	block.Release()
	return func() {
		if monitor != 0 {
			Send(Class("NSEvent"), "removeMonitor:", monitor)
		}
	}
}

type Video struct {
	Player     ID
	View       ID
	observer   ID
	generation uint64
	Completed  bool
}

func (v *Video) Play(parent ID, path string, r Rect, volume float64) error {
	v.Stop()
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("入场短片无法打开，可重试或跳过入场")
	}
	url := Send(Class("NSURL"), "fileURLWithPath:", NS(path))
	v.Player = Send(Send(Class("AVPlayer"), "alloc"), "initWithURL:", url)
	if v.Player == 0 {
		return errors.New("入场播放器不可用，可重试或跳过入场")
	}
	v.View = View("AVPlayerView", r)
	Send(v.View, "setControlsStyle:", int64(0))
	Send(v.View, "setVideoGravity:", NS("AVLayerVideoGravityResizeAspect"))
	Send(v.View, "setPlayer:", v.Player)
	Add(parent, v.View)
	v.Volume(volume)
	generation := v.generation
	block := objc.NewBlock(func(_ objc.Block, _ ID) {
		if v.generation == generation {
			v.Completed = true
		}
	})
	v.observer = Send(Send(Class("NSNotificationCenter"), "defaultCenter"), "addObserverForName:object:queue:usingBlock:", NS("AVPlayerItemDidPlayToEndTimeNotification"), Send(v.Player, "currentItem"), Send(Class("NSOperationQueue"), "mainQueue"), uintptr(block))
	Send(v.observer, "retain")
	block.Release()
	Send(v.Player, "play")
	return nil
}

// Read the real AVPlayerItem state; allocation alone is not a successful load.
func (v *Video) State() (ready bool, err error) {
	if v.Player == 0 {
		return false, errors.New("入场短片未加载，可重试或跳过入场")
	}
	item := Send(v.Player, "currentItem")
	if item == 0 {
		return false, errors.New("入场资源不可用，可重试或跳过入场")
	}
	switch objc.Send[int64](item, S("status")) {
	case 1:
		return true, nil
	case 2:
		return false, errors.New("入场短片无法播放，可重试或跳过入场")
	default:
		return false, nil
	}
}
func (v *Video) Volume(volume float64) {
	if v.Player != 0 {
		Send(v.Player, "setMuted:", volume <= 0)
		Send(v.Player, "setVolume:", float32(volume))
	}
}
func (v *Video) Stop() {
	v.generation++
	v.Completed = false
	if v.observer != 0 {
		Send(Send(Class("NSNotificationCenter"), "defaultCenter"), "removeObserver:", v.observer)
		Release(v.observer)
		v.observer = 0
	}
	if v.Player != 0 {
		Send(v.Player, "pause")
		Release(v.Player)
		v.Player = 0
	}
	if v.View != 0 {
		Send(v.View, "removeFromSuperview")
		Release(v.View)
		v.View = 0
	}
}

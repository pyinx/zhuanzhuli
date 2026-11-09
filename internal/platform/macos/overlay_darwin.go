//go:build darwin

package macos

import (
	"strconv"

	"github.com/ebitengine/purego/objc"
)

// Display is captured at the user's start action, not from later mouse movement.
type Display uint32

func CaptureDisplay(window uintptr) Display {
	screen := Send(ID(window), "screen")
	if screen == 0 {
		screen = Send(Class("NSScreen"), "mainScreen")
	}
	return displayID(screen)
}
func displayID(screen ID) Display {
	return Display(objc.Send[uint32](Send(Send(screen, "deviceDescription"), "objectForKey:", NS("NSScreenNumber")), S("unsignedIntValue")))
}
func displayScreen(display Display) ID {
	screens := Send(Class("NSScreen"), "screens")
	for i := uint64(0); i < objc.Send[uint64](screens, S("count")); i++ {
		s := Send(screens, "objectAtIndex:", i)
		if displayID(s) == display {
			return s
		}
	}
	return Send(Class("NSScreen"), "mainScreen")
}
func DisplayFrame(display Display, visible bool) Rect {
	selector := "frame"
	if visible {
		selector = "visibleFrame"
	}
	return getRect(displayScreen(display), S(selector))
}
func PresentEntry(window uintptr, display Display) uint64 {
	app := Send(Class("NSApplication"), "sharedApplication")
	previous := objc.Send[uint64](app, S("presentationOptions"))
	Send(ID(window), "setStyleMask:", uint64(0))
	frameWindow(ID(window), S("setFrame:display:"), DisplayFrame(display, false), true)
	// App-local auto-hide only; never writes a system preference or creates a Space.
	Send(app, "setPresentationOptions:", uint64(1|4))
	return previous
}
func RestorePresentation(previous uint64) {
	Send(Send(Class("NSApplication"), "sharedApplication"), "setPresentationOptions:", previous)
}

type CounterOverlay struct {
	Window, Label ID
	Display       Display
	due           bool
	centered      bool
}

func (o *CounterOverlay) Open(display Display) {
	o.open(display, false)
}

func (o *CounterOverlay) open(display Display, centered bool) {
	if o.Window != 0 {
		return
	}
	if objc.GetClass("FocusCounterPanel") == 0 {
		_, err := objc.RegisterClass("FocusCounterPanel", objc.GetClass("NSPanel"), nil, nil, []objc.MethodDef{
			{Cmd: S("canBecomeKeyWindow"), Fn: func(ID, objc.SEL) bool { return false }},
			{Cmd: S("canBecomeMainWindow"), Fn: func(ID, objc.SEL) bool { return false }},
		})
		if err != nil {
			panic(err)
		}
	}
	size := R(0, 0, 152, 32)
	if centered {
		size = R(0, 0, 152, 152)
	}
	o.Window = initWindow(Send(Class("FocusCounterPanel"), "alloc"), S("initWithContentRect:styleMask:backing:defer:"), size, 128, 2, false)
	o.Display = display
	o.centered = centered
	Send(o.Window, "setReleasedWhenClosed:", false)
	Send(o.Window, "setOpaque:", false)
	Send(o.Window, "setBackgroundColor:", Send(Class("NSColor"), "clearColor"))
	Send(o.Window, "setHasShadow:", false)
	Send(o.Window, "setIgnoresMouseEvents:", true)
	Send(o.Window, "setHidesOnDeactivate:", false)
	Send(o.Window, "setLevel:", int64(3))
	Send(o.Window, "setCollectionBehavior:", uint64(1|16|64|256))
	if centered {
		Send(o.Window, "setTitle:", NS("专注倒数"))
		root := Send(o.Window, "contentView")
		Send(root, "setWantsLayer:", true)
		layer := Send(root, "layer")
		Send(layer, "setCornerRadius:", float64(30))
		color := Send(Class("NSColor"), "colorWithWhite:alpha:", float64(.06), float64(.76))
		Send(layer, "setBackgroundColor:", Send(color, "CGColor"))
		o.Label = Send(Class("NSTextField"), "labelWithString:", NS(""))
		Send(o.Label, "setFont:", Send(Class("NSFont"), "monospacedDigitSystemFontOfSize:weight:", float64(98), float64(.35)))
		Send(o.Label, "setTextColor:", Send(Class("NSColor"), "whiteColor"))
		Add(root, o.Label)
		o.Reposition()
		return
	}
	Send(o.Window, "setTitle:", NS("专注计时"))
	o.Label = Label(Send(o.Window, "contentView"), "", R(6, 5, 140, 22), 17)
	Send(o.Label, "setAlignment:", int64(2))
	Send(o.Label, "setFont:", Send(Class("NSFont"), "monospacedDigitSystemFontOfSize:weight:", float64(17), float64(.15)))
	Send(o.Label, "setTextColor:", Send(Send(Class("NSColor"), "labelColor"), "colorWithAlphaComponent:", float64(.72)))
	shadow := Send(Class("NSShadow"), "new")
	Send(shadow, "setShadowColor:", Send(Send(Class("NSColor"), "windowBackgroundColor"), "colorWithAlphaComponent:", float64(.85)))
	Send(shadow, "setShadowBlurRadius:", float64(3))
	Send(o.Label, "setShadow:", shadow)
	Release(shadow)
	o.Reposition()
	Send(o.Window, "orderFrontRegardless")
}
func (o *CounterOverlay) ShowCountdown(display Display, number int) {
	o.open(display, true)
	text := strconv.Itoa(number)
	if Value(o.Label) == text {
		return
	}
	Text(o.Label, text)
	Send(o.Label, "sizeToFit")
	b := Bounds(o.Label)
	content := Bounds(Send(o.Window, "contentView"))
	Frame(o.Label, R((content.W-b.W)/2, (content.H-b.H)/2, b.W, b.H))
	if !ReducedMotion() {
		Send(o.Label, "setWantsLayer:", true)
		animation := Send(Class("CABasicAnimation"), "animationWithKeyPath:", NS("transform.scale"))
		Send(animation, "setFromValue:", Send(Class("NSNumber"), "numberWithDouble:", float64(.9)))
		Send(animation, "setToValue:", Send(Class("NSNumber"), "numberWithDouble:", float64(1)))
		Send(animation, "setDuration:", float64(.22))
		Send(Send(o.Label, "layer"), "addAnimation:forKey:", animation, NS("countdown"))
	}
	Send(o.Window, "orderFrontRegardless")
}
func (o *CounterOverlay) Reposition() {
	if o.Window == 0 {
		return
	}
	screen := displayScreen(o.Display)
	o.Display = displayID(screen)
	if o.centered {
		r := DisplayFrame(o.Display, false)
		frameWindow(o.Window, S("setFrame:display:"), R(r.X+(r.W-152)/2, r.Y+(r.H-152)/2, 152, 152), true)
		return
	}
	r := DisplayFrame(o.Display, true)
	frameWindow(o.Window, S("setFrame:display:"), R(r.X+r.W-152-24, r.Y+r.H-32-16, 152, 32), true)
}
func (o *CounterOverlay) Update(text string, due bool) {
	if o.Label != 0 {
		Text(o.Label, text)
		if o.due != due {
			o.due = due
			alpha := .72
			if due {
				alpha = 1
			}
			Send(o.Label, "setTextColor:", Send(Send(Class("NSColor"), "labelColor"), "colorWithAlphaComponent:", float64(alpha)))
			Send(o.Window, "setBackgroundColor:", Send(Class("NSColor"), "clearColor"))
			if due {
				Send(o.Window, "setBackgroundColor:", Send(Class("NSColor"), "windowBackgroundColor"))
			}
		}
	}
}
func (o *CounterOverlay) Close() {
	if o.Window == 0 {
		return
	}
	Send(o.Window, "orderOut:", ID(0))
	Send(o.Window, "close")
	Release(o.Window)
	o.Window = 0
	o.Label = 0
	o.due = false
}

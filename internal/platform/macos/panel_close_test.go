//go:build darwin

package macos

import (
	"errors"
	"testing"

	"github.com/ebitengine/purego"
)

// Real CF arrays exercise the same type/count/equality boundary used by the
// cleanup path, without contacting any application's windows.
func TestPanelCleanupUnknownIsNotReportedClosed(t *testing.T) {
	if err := loadAX(); err != nil {
		t.Fatal(err)
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	var create func(uintptr, *uintptr, int64, uintptr) uintptr
	purego.RegisterLibFunc(&create, lib, "CFArrayCreate")
	a, b := cfString(0, "own window", 0x08000100), cfString(0, "other window", 0x08000100)
	defer cfDrop(a)
	defer cfDrop(b)
	array := func(refs ...uintptr) uintptr {
		var p *uintptr
		if len(refs) > 0 {
			p = &refs[0]
		}
		r := create(0, p, int64(len(refs)), 0)
		if r == 0 {
			t.Fatal("CF allocation failed")
		}
		t.Cleanup(func() { cfDrop(r) })
		return r
	}
	owned := array(a)
	for _, tc := range []struct {
		name    string
		current uintptr
		code    int32
		wantErr bool
		want    uintptr
	}{
		{"missing", 0, 0, true, 0}, {"AX-error", owned, -25202, true, 0}, {"wrong-type", a, 0, true, 0},
		{"closed", array(), 0, false, 0}, {"same", array(a), 0, false, a},
		{"replaced", array(b), 0, true, 0}, {"multiple", array(a, b), 0, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			window, e := panelWindowToClose(owned, tc.current, tc.code)
			if (e != nil) != tc.wantErr || window != tc.want {
				t.Fatalf("window=%d err=%v; want error=%v", window, e, tc.wantErr)
			}
		})
	}
}

// Policy checks are separate from the real UI probe; they do not prove macOS
// delivers an event or closes a panel.
func TestPanelEscapeRejectsEveryUnsafeBoundary(t *testing.T) {
	valid := panelEscapeState{trusted: true, instanceSame: true, windowSame: true, focusedWindowSame: true, pid: 100, ownedPID: 100, focusedPID: 100, windows: 1}
	cases := map[string]func(*panelEscapeState){
		"permission":           func(s *panelEscapeState) { s.trusted = false },
		"process-restarted":    func(s *panelEscapeState) { s.instanceSame = false },
		"invalid-pid":          func(s *panelEscapeState) { s.pid = 0 },
		"other-owner":          func(s *panelEscapeState) { s.ownedPID = 101 },
		"closed":               func(s *panelEscapeState) { s.windows = 0 },
		"multiple":             func(s *panelEscapeState) { s.windows = 2 },
		"unreadable":           func(s *panelEscapeState) { s.windows = -1 },
		"window-replaced":      func(s *panelEscapeState) { s.windowSame = false },
		"other-foreground":     func(s *panelEscapeState) { s.focusedPID = 101 },
		"unknown-foreground":   func(s *panelEscapeState) { s.focusedPID = 0 },
		"other-focused-window": func(s *panelEscapeState) { s.focusedWindowSame = false },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := valid
			change(&s)
			c := &PanelCloser{}
			posts := 0
			if err := c.sendOnce(s, func() error { posts++; return nil }); err == nil || posts != 0 || c.SentPairs != 0 {
				t.Fatalf("unsafe send accepted: %v posts=%d", err, posts)
			}
			if err := c.sendOnce(valid, func() error { posts++; return nil }); err == nil || posts != 0 {
				t.Fatal("refused attempt retried")
			}
		})
	}
	c := &PanelCloser{}
	posts := 0
	post := func() error { posts++; return nil }
	if err := c.sendOnce(valid, post); err != nil {
		t.Fatal(err)
	}
	if err := c.sendOnce(valid, post); err == nil || posts != 1 || c.SentPairs != 1 {
		t.Fatal("more than one pair allowed")
	}
	c = &PanelCloser{}
	if err := c.sendOnce(valid, func() error { return errors.New("allocation failed") }); err == nil || c.SentPairs != 0 {
		t.Fatal("failed post counted as sent")
	}
}

// Validate the public C ABI by creating real events without posting any event.
func TestPanelEscapeEventABIWithoutPosting(t *testing.T) {
	if err := loadPanelEscape(); err != nil {
		t.Fatal(err)
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	var getType func(uintptr) uint32
	var getFlags func(uintptr) uint64
	var getField func(uintptr, uint32) int64
	purego.RegisterLibFunc(&getType, lib, "CGEventGetType")
	purego.RegisterLibFunc(&getFlags, lib, "CGEventGetFlags")
	purego.RegisterLibFunc(&getField, lib, "CGEventGetIntegerValueField")
	for _, down := range []bool{true, false} {
		e := cgKey(0, 0x35, down)
		if e == 0 {
			t.Fatal("event allocation failed")
		}
		cgFlags(e, 0)
		kind := uint32(11)
		if down {
			kind = 10
		}
		if getType(e) != kind || getFlags(e) != 0 || getField(e, 9) != 0x35 {
			cfDrop(e)
			t.Fatal("keyboard event ABI mismatch")
		}
		cfDrop(e)
	}
}

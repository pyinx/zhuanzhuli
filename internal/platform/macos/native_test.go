//go:build darwin

package macos

import (
	"runtime"
	"testing"
)

func TestBundleIdentityDoesNotRequireReadingDirectories(t *testing.T) {
	for _, input := range []string{"relative.app", "/", ""} {
		if _, e := bundlePath(input); e == nil {
			t.Fatal("invalid path accepted", input)
		}
	}
	got, e := bundlePath("/var/folders/nonexistent-test/application.app")
	if e != nil || got != "/private/var/folders/nonexistent-test/application.app" {
		t.Fatal(got, e)
	}
	got, e = bundlePath("/Applications/Editor.app")
	if e != nil || got != "/Applications/Editor.app" {
		t.Fatal(got, e)
	}
}

func TestNativeRectRoundTrip(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	pool := Send(Class("NSAutoreleasePool"), "new")
	defer Send(pool, "drain")

	// Foundation's NSValue exercises the same CGRect ABI without creating
	// an AppKit view outside the application's main thread.
	want := R(17, 29, 81, 63)
	value := initRect(Class("NSValue"), S("valueWithRect:"), want)
	if got := getRect(value, S("rectValue")); got != want {
		t.Fatalf("stored rect = %+v, want %+v", got, want)
	}
}

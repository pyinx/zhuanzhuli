//go:build darwin

package macos

import (
	"github.com/ebitengine/purego"
	"testing"
)

// Exercise the actual C ABI and ownership used to inspect AX attributes, without
// requesting permission or addressing any app's UI. This is not a Focus switch test.
func TestFocusAccessAndCoreFoundationBindings(t *testing.T) {
	if _, err := FocusAccess(); err != nil {
		t.Fatal(err)
	}
	text := cfString(0, "专注力 Focus", 0x08000100)
	if text == 0 {
		t.Fatal("CF string allocation failed")
	}
	defer cfDrop(text)
	if axText(text) != "专注力 Focus" {
		t.Fatal("UTF-8 ABI round trip failed")
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	var create func(uintptr, int32, *int64) uintptr
	purego.RegisterLibFunc(&create, lib, "CFNumberCreate")
	n := int64(1)
	number := create(0, 4, &n)
	if number == 0 {
		t.Fatal("CF number allocation failed")
	}
	defer cfDrop(number)
	var value int64
	if cfType(number) != cfNumberType() || !cfNumber(number, 4, &value) || value != 1 || axText(number) != "" {
		t.Fatal("CF number/type ABI failed")
	}
	var arrayCreate func(uintptr, *uintptr, int64, uintptr) uintptr
	purego.RegisterLibFunc(&arrayCreate, lib, "CFArrayCreate")
	refs := []uintptr{text, number}
	array := arrayCreate(0, &refs[0], 2, 0)
	if array == 0 {
		t.Fatal("CF array allocation failed")
	}
	defer cfDrop(array)
	if cfType(array) != cfArrayType() || cfCount(array) != 2 || axText(cfAt(array, 0)) != "专注力 Focus" || cfAt(array, 1) != number {
		t.Fatal("CF array ABI failed")
	}
}

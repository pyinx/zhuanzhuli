//go:build darwin

package macos

import (
	"focus.local/zhuanzhuli/pkg/journal"
	"os"
	"testing"
)

func TestSystemProcessStartIsStableAndRefusesMissingProcess(t *testing.T) {
	a, err := processStartTime(os.Getpid())
	if err != nil || a <= 0 {
		t.Fatal(a, err)
	}
	b, err := processStartTime(os.Getpid())
	if err != nil || a != b {
		t.Fatal("process identity changed during one lifetime", a, b, err)
	}
	if _, err = processStartTime(2147483647); err == nil {
		t.Fatal("invented identity for nonexistent process")
	}
}

func TestInstanceIdentityIncludesBirthPathBundleAndSource(t *testing.T) {
	r := journal.Ref{PID: 123, Launched: 123.5, Path: "/test/App.app", BundleID: "test.app", IdentitySource: journal.IdentityBSDStart}
	if !sameInstance(r, r) {
		t.Fatal("same instance rejected")
	}
	for _, change := range []func(*journal.Ref){func(x *journal.Ref) { x.PID++ }, func(x *journal.Ref) { x.Launched++ }, func(x *journal.Ref) { x.Path = "/test/Replacement.app" }, func(x *journal.Ref) { x.BundleID = "test.other" }, func(x *journal.Ref) { x.IdentitySource = "" }} {
		x := r
		change(&x)
		if sameInstance(r, x) {
			t.Fatal("replacement accepted", x)
		}
	}
}

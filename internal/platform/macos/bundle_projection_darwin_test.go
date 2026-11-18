//go:build darwin

package macos

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrappedReadOnlyBundleProjection(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "Reading.app")
	wrapper := filepath.Join(selected, "Wrapper")
	child := filepath.Join(wrapper, "Reader.app")
	mount := filepath.Join(root, "mount")
	running := filepath.Join(mount, "d", "Wrapper", "Reader.app")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(selected, "WrappedBundle")
	if err := os.Symlink("Wrapper/Reader.app", link); err != nil {
		t.Fatal(err)
	}
	if source, ok := projectedBundleSource(selected, running, wrapper, mount); !ok || source != child {
		t.Fatalf("same wrapper projection should match its child: %q, %t", source, ok)
	}
	direct := filepath.Join(mount, "d", "Reading.app")
	if source, ok := projectedBundleSource(selected, direct, selected, mount); !ok || source != selected {
		t.Fatal("the original whole-bundle projection no longer matches")
	}
	if _, ok := projectedBundleSource(selected, filepath.Join(mount, "d", "Wrapper", "Another.app"), wrapper, mount); ok {
		t.Fatal("a different child matched")
	}
	outside := filepath.Join(root, "Other.app")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if _, ok := projectedBundleSource(selected, filepath.Join(mount, "d", "Other.app"), outside, mount); ok {
		t.Fatal("an unrelated mount source matched")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, ok := projectedBundleSource(selected, filepath.Join(mount, "d", "Wrapper", "Other.app"), wrapper, mount); ok {
		t.Fatal("a wrapper link that escapes the selected app matched")
	}
}

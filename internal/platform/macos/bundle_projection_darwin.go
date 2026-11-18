//go:build darwin

package macos

import (
	"path/filepath"
	"syscall"
)

// SameReadOnlyBundleProjection accepts a different runtime path only when the
// kernel reports a read-only projection of the selected bundle itself. Keep
// the real process path in its journal.Ref; this only matches the selection.
func SameReadOnlyBundleProjection(selected, running string) bool {
	if !filepath.IsAbs(selected) || !filepath.IsAbs(running) {
		return false
	}
	var mount syscall.Statfs_t
	const readOnly = 0x1 // MNT_RDONLY in <sys/mount.h>
	if syscall.Statfs(running, &mount) != nil || mount.Flags&readOnly == 0 || mountString(mount.Fstypename[:]) != "nullfs" {
		return false
	}
	from, on := mountString(mount.Mntfromname[:]), mountString(mount.Mntonname[:])
	if !filepath.IsAbs(from) || !filepath.IsAbs(on) {
		return false
	}
	sourcePath, ok := projectedBundleSource(selected, running, from, on)
	if !ok {
		return false
	}
	var source, projection syscall.Stat_t
	if syscall.Lstat(sourcePath, &source) != nil || syscall.Lstat(running, &projection) != nil {
		return false
	}
	return source.Mode&syscall.S_IFMT == syscall.S_IFDIR &&
		projection.Mode&syscall.S_IFMT == syscall.S_IFDIR &&
		source.Dev != projection.Dev && source.Ino == projection.Ino
}

func projectedBundleSource(selected, running, from, on string) (string, bool) {
	canonical, err := filepath.EvalSymlinks(from)
	if err != nil || canonical != from {
		return "", false
	}
	on, err = bundlePath(on)
	if err != nil {
		return "", false
	}
	source := from
	projection := filepath.Join(on, "d", filepath.Base(from))
	if from != selected {
		if from != filepath.Join(selected, "Wrapper") {
			return "", false
		}
		source, err = filepath.EvalSymlinks(filepath.Join(selected, "WrappedBundle"))
		if err != nil || filepath.Dir(source) != from || filepath.Ext(source) != ".app" {
			return "", false
		}
		projection = filepath.Join(projection, filepath.Base(source))
	}
	return source, projection == running
}

func mountString(value []int8) string {
	data := make([]byte, 0, len(value))
	for _, c := range value {
		if c == 0 {
			return string(data)
		}
		data = append(data, byte(c))
	}
	return "" // Reject truncated paths.
}

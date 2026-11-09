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
	canonical, err := filepath.EvalSymlinks(from)
	if err != nil || canonical != from || canonical != selected {
		return false
	}
	on, err = bundlePath(on)
	if err != nil || filepath.Join(on, "d", filepath.Base(from)) != running {
		return false
	}
	var source, projection syscall.Stat_t
	if syscall.Lstat(from, &source) != nil || syscall.Lstat(running, &projection) != nil {
		return false
	}
	return source.Mode&syscall.S_IFMT == syscall.S_IFDIR &&
		projection.Mode&syscall.S_IFMT == syscall.S_IFDIR &&
		source.Dev != projection.Dev && source.Ino == projection.Ino
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

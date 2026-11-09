//go:build darwin

package macos

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	ErrInstanceGone        = errors.New("应用实例已退出")
	ErrInstanceChanged     = errors.New("应用实例已改变，保留当前状态")
	ErrIdentityUnavailable = errors.New("无法确认应用实例，未改变该应用")
	procOnce               sync.Once
	procErr                error
	procPIDInfo            func(int32, int32, uint64, unsafe.Pointer, int32) int32
)

// The 64-bit macOS SDK proc_bsdinfo is 136 bytes, with pid at 12 and start at 120.
// This is a system query, not a wall clock or an inferred launch timestamp.
func processStartTime(pid int) (float64, error) {
	if pid < 1 {
		return 0, ErrInstanceGone
	}
	procOnce.Do(func() {
		var lib uintptr
		lib, procErr = purego.Dlopen("/usr/lib/libproc.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if procErr == nil {
			purego.RegisterLibFunc(&procPIDInfo, lib, "proc_pidinfo")
		}
	})
	if procErr != nil {
		return 0, fmt.Errorf("%w: %v", ErrIdentityUnavailable, procErr)
	}
	type bsdInfo struct {
		Flags, Status, XStatus, PID uint32
		Other                       [104]byte
		StartSeconds, StartMicros   uint64
	}
	var info bsdInfo
	if unsafe.Sizeof(info) != 136 || procPIDInfo(int32(pid), 3, 0, unsafe.Pointer(&info), 136) != 136 || int(info.PID) != pid || info.StartSeconds == 0 || info.StartMicros >= 1000000 {
		return 0, ErrIdentityUnavailable
	}
	return float64(info.StartSeconds) + float64(info.StartMicros)/1000000, nil
}

func describeSource(app ID, source string) (journal.Ref, error) {
	if app == 0 || objc.Send[bool](app, S("isTerminated")) {
		return journal.Ref{}, ErrInstanceGone
	}
	pid := int(objc.Send[int32](app, S("processIdentifier")))
	path, e := bundlePath(Str(Send(Send(app, "bundleURL"), "path")))
	if e != nil {
		return journal.Ref{}, fmt.Errorf("%w: %v", ErrIdentityUnavailable, e)
	}
	bid := Str(Send(app, "bundleIdentifier"))
	if bid == "" {
		return journal.Ref{}, ErrIdentityUnavailable
	}
	var start float64
	switch source {
	case "":
		date := Send(app, "launchDate")
		if date == 0 {
			return journal.Ref{}, ErrIdentityUnavailable
		}
		start = objc.Send[float64](date, S("timeIntervalSince1970"))
	case journal.IdentityBSDStart:
		start, e = processStartTime(pid)
		if e != nil {
			return journal.Ref{}, e
		}
	default:
		return journal.Ref{}, ErrIdentityUnavailable
	}
	if objc.Send[bool](app, S("isTerminated")) {
		return journal.Ref{}, ErrInstanceGone
	}
	label := Str(Send(app, "localizedName"))
	if label == "" {
		label = bid
	}
	return journal.Ref{PID: pid, Launched: start, Path: path, BundleID: bid, Label: label, Token: "production-instance", IdentitySource: source}, nil
}

func sameInstance(a, b journal.Ref) bool {
	return a.PID == b.PID && a.Launched == b.Launched && a.Path == b.Path && a.BundleID == b.BundleID && a.IdentitySource == b.IdentitySource
}

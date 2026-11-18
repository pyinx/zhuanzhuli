//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Exercise the real AX adapter and recovery ledger against a window owned by
// this disposable host. Self AX works without widening system permissions.
func (a *application) nativeFullscreenCheck(done func(bool, error)) {
	const title = "原生全屏验收窗口"
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: title, Width: 540, Height: 360, Hidden: true,
		Content: ui.View(func(c *ui.Context) { ui.Column(c).Fill().Children(func() {}) }),
	})
	if w.NativeHandle() == 0 {
		w.Close()
		done(false, errors.New("cannot create an owned native fullscreen window"))
		return
	}
	m.Send(m.ID(w.NativeHandle()), "setCollectionBehavior:", uint64(1<<7)) // NSWindowCollectionBehaviorFullScreenPrimary
	ref, err := m.Describe(m.Send(m.Class("NSRunningApplication"), "currentApplication"))
	if err != nil {
		w.Close()
		done(false, err)
		return
	}
	a.main.Hide()
	entered, left := make(chan struct{}, 1), make(chan struct{}, 1)
	w.OnEnterFullScreen(func() {
		select {
		case entered <- struct{}{}:
		default:
		}
	})
	w.OnLeaveFullScreen(func() {
		select {
		case left <- struct{}{}:
		default:
		}
	})
	w.Show()
	w.Focus()
	trusted, _ := m.FocusAccess()
	a.effect(func() error {
		var window *m.FullscreenWindow
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			candidate, err := m.FocusedFullscreenWindow(int32(os.Getpid()))
			if err == nil {
				if candidate.Title() == title {
					window = candidate
					break
				}
				candidate.Close()
			}
		}
		if window == nil {
			return errors.New("owned native fullscreen test window did not receive keyboard focus")
		}
		defer func() {
			if a.fullscreenLeases[ref] != window {
				// A successful restore closes the owned handle.
				window.Close()
			}
		}()
		if err := window.Writable(); err != nil {
			return err
		}
		// The real window has no observation lease yet, so the production
		// entry must reject it without sending a request and record an ERROR.
		a.debug.setEnabled(true)
		missingWatchErr := a.fullscreenTarget(immersionTarget{ref: ref, window: window}, func() bool { return true }, newDebugAttempt())
		a.debug.setEnabled(false)
		if missingWatchErr == nil || a.immersionJournal.Pending() != 0 {
			return errors.New("unobserved fullscreen target was not rejected safely")
		}
		if err := window.Watch(); err != nil {
			return err
		}
		if !trusted {
			// Self AX reads work without a grant, but AX writes do not. Toggle
			// the test's own NSWindow and verify the production observer.
			mygo.RunOnMain(func() { w.SetFullScreen(true) })
			select {
			case <-entered:
			case <-time.After(4 * time.Second):
				return errors.New("own window did not finish entering fullscreen")
			}
			if err := waitFullscreen(window, true); err != nil {
				return fmt.Errorf("own window could not enter fullscreen (NSWindow fullscreen=%t): %w", w.IsFullScreen(), err)
			}
			if err := a.fullscreenTarget(immersionTarget{ref: ref, window: window}, func() bool { return true }, ""); err != nil {
				return err
			}
			if a.immersionJournal.Pending() != 0 {
				return errors.New("pre-existing fullscreen was acquired")
			}
			window.ArmRestore(window.Revision())
			mygo.RunOnMain(func() { w.SetFullScreen(false) })
			select {
			case <-left:
			case <-time.After(4 * time.Second):
				return errors.New("own window did not finish leaving fullscreen")
			}
			if err := waitFullscreen(window, false); err != nil {
				return fmt.Errorf("own window could not leave fullscreen (NSWindow fullscreen=%t): %w", w.IsFullScreen(), err)
			}
			if !window.Changed() {
				return errors.New("native observer missed an external fullscreen change")
			}
			// Simulate a lost acknowledgement. A still-windowed sample cannot
			// rule out a delayed AX request; recovery must keep the ledger.
			if err := a.immersionJournal.Begin("native-uncertain-write", ref, false); err != nil {
				return err
			}
			if a.fullscreenLeases == nil {
				a.fullscreenLeases = map[journal.Ref]*m.FullscreenWindow{}
			}
			a.fullscreenLeases[ref] = window
			if err := a.restoreImmersion(); err == nil || a.immersionJournal.Pending() != 1 {
				return errors.New("unconfirmed native request lost its recovery record")
			}
			if err := a.immersionJournal.Abandon(ref, "owned fixture reviewed"); err != nil {
				return err
			}
			return nil
		}
		target := immersionTarget{ref: ref, window: window}
		if err := a.fullscreenTarget(target, func() bool { return true }, ""); err != nil {
			return fmt.Errorf("enter owned native fullscreen: %w", err)
		}
		if a.immersionJournal.Pending() != 1 {
			return errors.New("owned native fullscreen did not record recovery")
		}
		if err := a.restoreImmersion(); err != nil {
			return fmt.Errorf("restore owned native fullscreen: %w", err)
		}
		current, err := m.WorkFullscreenWindow(int32(os.Getpid()))
		if err != nil {
			return err
		}
		defer current.Close()
		if full, err := current.State(); err != nil || full {
			return fmt.Errorf("window was not restored: full=%t, err=%v", full, err)
		}
		if err := current.Set(true); err != nil {
			return err
		}
		if err := waitFullscreen(current, true); err != nil {
			return err
		}
		if err := a.fullscreenTarget(immersionTarget{ref: ref, window: current}, func() bool { return true }, ""); err != nil {
			return err
		}
		if a.immersionJournal.Pending() != 0 {
			return errors.New("pre-existing fullscreen was acquired")
		}
		if err := a.restoreImmersion(); err != nil {
			return err
		}
		if full, err := current.State(); err != nil || !full {
			return errors.New("pre-existing fullscreen was changed")
		}
		if err := current.Set(false); err != nil {
			return err
		}
		if err := waitFullscreen(current, false); err != nil {
			return err
		}
		// A user can leave and re-enter fullscreen; that is no longer our edit.
		if err := current.Watch(); err != nil {
			return err
		}
		if err := a.fullscreenTarget(immersionTarget{ref: ref, window: current}, func() bool { return true }, ""); err != nil {
			return err
		}
		external, err := m.WorkFullscreenWindow(int32(os.Getpid()))
		if err != nil {
			return err
		}
		defer external.Close()
		for _, desired := range []bool{false, true} {
			if err := external.Set(desired); err != nil {
				return err
			}
			if err := waitFullscreen(external, desired); err != nil {
				return err
			}
		}
		if err := a.restoreImmersion(); err == nil || a.immersionJournal.Pending() != 1 {
			return errors.New("user's later fullscreen should be left for manual review")
		}
		if full, err := external.State(); err != nil || !full {
			return errors.New("user's later fullscreen was changed")
		}
		if err := external.Set(false); err != nil {
			return err
		}
		return a.immersionJournal.Abandon(ref, "owned fixture reviewed")
	}, func(err error) {
		w.Close()
		// Let AppKit complete closing the dedicated test window before quit.
		time.AfterFunc(80*time.Millisecond, func() { mygo.RunOnMain(func() { done(trusted && err == nil, err) }) })
	})
}

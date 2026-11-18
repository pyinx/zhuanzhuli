//go:build darwin

package main

import (
	"errors"
	"fmt"
	"time"

	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/egoist/mygo"
)

type immersionTarget struct {
	app        session.App
	ref        journal.Ref
	window     *m.FullscreenWindow
	fullscreen bool
}

var errImmersionAppNotRunning = errors.New("尚未找到正在运行的应用")

type immersionOutcome struct {
	selected, fullscreen int
	accessErr            error
	handedOff            bool
}

func (r immersionOutcome) caption() string {
	switch {
	case errors.Is(r.accessErr, errImmersionPermission):
		return "未获辅助功能权限，本次未调整全屏。"
	case r.accessErr != nil:
		return "无法确认辅助功能权限，本次未调整全屏。"
	case r.fullscreen == 0:
		return "本次未能调整全屏，计时照常。"
	case r.fullscreen < r.selected:
		return "部分窗口未全屏，计时照常。"
	case !r.handedOff:
		return "未能切回专注应用，计时照常。"
	default:
		return ""
	}
}

func immersionRunningApp(app session.App, refs []journal.Ref, scanErr error) (journal.Ref, error) {
	for _, err := range protectionLeaves(scanErr) {
		var issue *m.AppIdentityError
		if errors.As(err, &issue) && issue.BundleID == app.ID {
			return journal.Ref{}, errors.New("检测到同一应用正在运行，但无法核对运行实例。请退出该应用，再从所选位置重新打开后重试")
		}
	}
	var match journal.Ref
	otherLocation := false
	for _, r := range refs {
		if r.BundleID != app.ID {
			continue
		}
		if r.Path != app.Path && !m.SameReadOnlyBundleProjection(app.Path, r.Path) {
			otherLocation = true
			continue
		}
		if match.PID != 0 {
			return journal.Ref{}, errors.New("所选应用有多个运行实例，请先退出多余的实例")
		}
		match = r
	}
	if match.PID != 0 {
		return match, nil
	}
	if otherLocation {
		return journal.Ref{}, errors.New("正在从与所选位置不同的路径运行。请退出该应用，再从所选位置重新打开后重试")
	}
	return journal.Ref{}, errImmersionAppNotRunning
}

func openMissingImmersionApps(targets []immersionTarget, refs []journal.Ref, scanErr error, open func(session.App) error) map[string]error {
	skipped := make(map[string]error)
	for _, t := range targets {
		_, err := immersionRunningApp(t.app, refs, scanErr)
		if errors.Is(err, errImmersionAppNotRunning) {
			err = open(t.app)
		}
		if err != nil {
			skipped[t.app.ID] = err
		}
	}
	return skipped
}

func (a *application) pendingEnvironment() int {
	count := 0
	for _, j := range []*journal.Journal{a.journal, a.immersionJournal} {
		if j != nil {
			count += j.Pending()
		}
	}
	return count
}

// All native AppKit process checks are on main; AX messages to other processes
// run on the existing serial environment worker, never on the UI thread.
func immersionInstance(ref journal.Ref) error {
	var err error
	mygo.RunOnMain(func() { _, err = m.Instance(ref) })
	return err
}

var errImmersionPermission = errors.New("当前版本未获辅助功能权限")
var errImmersionPermissionCheck = errors.New("无法核对辅助功能权限")

func (a *application) checkImmersionAccess() error {
	read := a.readImmersionAccess
	if read == nil {
		read = m.FocusAccess
	}
	trusted, err := read()
	if err != nil {
		return fmt.Errorf("%w：%w", errImmersionPermissionCheck, err)
	}
	if !trusted {
		return errImmersionPermission
	}
	return nil
}

func debugAXCode(err error) int32 {
	var ax *m.FullscreenAXError
	if errors.As(err, &ax) {
		return ax.Code
	}
	return 0
}

// Permission is needed for AX window operations, not for the session clock or
// AppKit foreground handoff. Keep an unavailable permission distinct from cancellation.
func (a *application) tryImmersionWindows(result *immersionOutcome, targets []immersionTarget, valid func() bool, attempt string, try func(*immersionTarget) error) error {
	if !valid() {
		return errHideCancelled
	}
	result.accessErr = a.checkImmersionAccessFor(attempt)
	if result.accessErr != nil {
		return nil
	}
	for i := range targets {
		if !valid() {
			return errHideCancelled
		}
		t := &targets[i]
		err := try(t)
		if errors.Is(err, errHideCancelled) {
			return errHideCancelled
		}
		t.fullscreen = err == nil
		if t.fullscreen {
			result.fullscreen++
		}
	}
	if !valid() {
		return errHideCancelled
	}
	return nil
}

func (a *application) enterImmersiveWork(s session.State, valid func() bool, attempt string) (immersionOutcome, error) {
	result := immersionOutcome{selected: len(s.AllowedIDs())}
	isCurrent := func() bool {
		var ok bool
		mygo.RunOnMain(func() { ok = valid() })
		return ok
	}
	if !isCurrent() {
		return result, errHideCancelled
	}
	if s.Config == nil {
		return result, nil
	}
	targets := make([]immersionTarget, 0, result.selected)
	defer func() {
		for _, t := range targets {
			if t.window != nil && a.fullscreenLeases[t.ref] != t.window {
				t.window.Close()
			}
		}
	}()
	selected := append(append([]session.App{}, s.Config.Apps...), s.TemporaryApps...)
	seen := map[string]bool{}
	for _, item := range selected {
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		var canonical session.App
		var err error
		mygo.RunOnMain(func() { canonical, err = m.AppAt(item.Path) })
		if err != nil || canonical.ID != item.ID || (a.fixtureRoot != "" && !a.ownFixture(journal.Ref{BundleID: item.ID, Path: canonical.Path})) {
			a.debugEvent(debugTargetFailed, attempt, item.ID, 0)
			continue
		}
		canonical.Name = item.Name
		if canonical.Name == "" {
			canonical.Name = item.ID
		}
		targets = append(targets, immersionTarget{app: canonical})
	}

	var initialRefs []journal.Ref
	var initialErr error
	mygo.RunOnMain(func() { initialRefs, initialErr = m.Running() })
	skipped := openMissingImmersionApps(targets, initialRefs, initialErr, func(app session.App) error {
		var openErr error
		mygo.RunOnMain(func() {
			if !valid() {
				openErr = errHideCancelled
				return
			}
			openErr = m.OpenApp(app)
		})
		return openErr
	})
	// Resolve processes together, so multiple applications starting at the
	// same time share one grace period. Permanent failures don't delay others.
	deadline := time.Now().Add(4 * time.Second)
	for {
		if !isCurrent() {
			return result, errHideCancelled
		}
		var refs []journal.Ref
		var scanErr error
		mygo.RunOnMain(func() { refs, scanErr = m.Running() })
		waiting := false
		for i := range targets {
			t := &targets[i]
			if skipped[t.app.ID] != nil {
				continue
			}
			ref, err := immersionRunningApp(t.app, refs, scanErr)
			t.ref = ref
			if errors.Is(err, errImmersionAppNotRunning) && time.Now().Before(deadline) {
				waiting = true
			} else if err != nil {
				skipped[t.app.ID] = err
			}
		}
		if !waiting {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for id, err := range skipped {
		if errors.Is(err, errHideCancelled) {
			return result, errHideCancelled
		}
		a.debugEvent(debugTargetFailed, attempt, id, debugAXCode(err))
	}

	// Windows created during application startup share a short grace period.
	// Inspect each instance again before addressing its PID via AX.
	windowDeadline := time.Now().Add(4 * time.Second)
	err := a.tryImmersionWindows(&result, targets, isCurrent, attempt, func(t *immersionTarget) error {
		if t.ref.PID == 0 {
			return errImmersionAppNotRunning
		}
		if a.immersionJournal == nil {
			a.debugEvent(debugStorageFailed, attempt, t.app.ID, 0)
			return errors.New("全屏恢复记录不可用")
		}
		for {
			if !isCurrent() {
				return errHideCancelled
			}
			err := immersionInstance(t.ref)
			if err == nil {
				t.window, err = m.WorkFullscreenWindow(int32(t.ref.PID))
			}
			if err == nil {
				break
			}
			if !errors.Is(err, m.ErrWorkWindowUnavailable) || !time.Now().Before(windowDeadline) {
				a.debugEvent(debugTargetFailed, attempt, t.app.ID, debugAXCode(err))
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
		a.debugEvent(debugWindowReady, attempt, t.app.ID, 0)
		full, err := t.window.State()
		if err == nil && !full {
			err = t.window.Writable()
			if err == nil {
				err = t.window.Watch()
			}
		}
		if err != nil {
			a.debugEvent(debugWindowUnsupported, attempt, t.app.ID, debugAXCode(err))
			return err
		}
		if err := a.fullscreenTarget(*t, isCurrent, attempt); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if result.fullscreen > 0 {
		for i := range targets {
			t := &targets[i]
			if !t.fullscreen {
				continue
			}
			if err := immersionInstance(t.ref); err != nil {
				a.debugEvent(debugHandoffFailed, attempt, t.app.ID, 0)
				t.fullscreen = false
				result.fullscreen--
			} else if full, err := t.window.State(); err != nil || !full {
				a.debugEvent(debugFullscreenFailed, attempt, t.app.ID, debugAXCode(err))
				t.fullscreen = false
				result.fullscreen--
			}
		}
		if result.fullscreen > 0 {
			if err := a.activateImmersiveTarget(targets, valid, true); err == nil {
				result.handedOff = true
				a.debugEvent(debugHandoffConfirmed, attempt, "", 0)
				return result, nil
			} else if errors.Is(err, errHideCancelled) {
				return result, err
			}
		}
	}
	if err := a.activateImmersiveTarget(targets, valid, false); err == nil {
		result.handedOff = true
		a.debugEvent(debugWindowedHandoff, attempt, "", 0)
	} else if errors.Is(err, errHideCancelled) {
		return result, err
	} else {
		a.debugEvent(debugHandoffFailed, attempt, "", debugAXCode(err))
	}
	return result, nil
}

func (a *application) activateImmersiveTarget(targets []immersionTarget, valid func() bool, fullscreen bool) error {
	var ordered []immersionTarget
	var cancelled bool
	mygo.RunOnMain(func() {
		cancelled = !valid()
		if cancelled {
			return
		}
		var foreground, last *immersionTarget
		var rest []immersionTarget
		for _, t := range targets {
			if t.ref.PID == 0 || (fullscreen && !t.fullscreen) {
				continue
			}
			active, err := m.Active(t.ref)
			if err != nil {
				continue
			}
			switch {
			case active && foreground == nil:
				foreground = &t
			case a.lastWork != nil && *a.lastWork == t.ref:
				last = &t
			default:
				rest = append(rest, t)
			}
		}
		if foreground != nil {
			ordered = append(ordered, *foreground)
		}
		if last != nil {
			ordered = append(ordered, *last)
		}
		ordered = append(ordered, rest...)
	})
	if cancelled {
		return errHideCancelled
	}
	return tryImmersionHandoffs(ordered, func(target immersionTarget) error {
		return a.confirmImmersiveHandoff(target, valid, fullscreen)
	})
}

func tryImmersionHandoffs(targets []immersionTarget, handoff func(immersionTarget) error) error {
	if len(targets) == 0 {
		return errors.New("没有可交接的专注应用")
	}
	var lastErr error
	for _, target := range targets {
		lastErr = handoff(target)
		if lastErr == nil || errors.Is(lastErr, errHideCancelled) {
			return lastErr
		}
	}
	return lastErr
}

func (a *application) confirmImmersiveHandoff(target immersionTarget, valid func() bool, fullscreen bool) error {
	var err error
	mygo.RunOnMain(func() {
		if !valid() {
			err = errHideCancelled
			return
		}
		var active bool
		active, err = m.Active(target.ref)
		if err == nil && !active {
			err = m.Activate(target.ref)
		}
	})
	if err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var active bool
		mygo.RunOnMain(func() {
			if !valid() {
				err = errHideCancelled
				return
			}
			active, err = m.Active(target.ref)
		})
		if err != nil {
			return err
		}
		if active {
			focused := !fullscreen || a.fullscreenLeases[target.ref] == nil
			if !focused {
				if current, focusErr := m.FocusedFullscreenWindow(int32(target.ref.PID)); focusErr == nil {
					focused = target.window.Matches(current)
					current.Close()
				}
			}
			// For pre-existing fullscreen, do not force macOS to switch
			// away from another focused window of the same app.
			if focused {
				if fullscreen {
					if full, stateErr := target.window.State(); stateErr != nil || !full {
						return errors.New("交接时工作窗口已离开原生全屏")
					}
				}
				mygo.RunOnMain(func() {
					if !valid() {
						err = errHideCancelled
					} else {
						a.lastWork = &target.ref
					}
				})
				return err
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("未确认切回专注应用")
}

func (a *application) fullscreenTarget(t immersionTarget, valid func() bool, attempt string) error {
	w := t.window
	fullscreenFailure := func(err error) error {
		a.debugEvent(debugFullscreenFailed, attempt, t.ref.BundleID, debugAXCode(err))
		return err
	}
	storage := func(err error) error {
		if err != nil {
			a.debugEvent(debugStorageFailed, attempt, t.ref.BundleID, 0)
		}
		return err
	}
	if !valid() {
		return errHideCancelled
	}
	if err := immersionInstance(t.ref); err != nil {
		return fullscreenFailure(err)
	}
	full, err := w.State()
	if err != nil {
		return fullscreenFailure(err)
	}
	if full {
		a.debugEvent(debugFullscreenPreserved, attempt, t.ref.BundleID, 0)
		return nil
	}
	if !w.Watching() || w.Changed() {
		return fullscreenFailure(errors.New("工作窗口在准备时发生变化，请重试"))
	}
	id := fmt.Sprintf("fullscreen-%d-%d", t.ref.PID, time.Now().UnixNano())
	if err := a.immersionJournal.Begin(id, t.ref, false); err != nil {
		return storage(err)
	}
	if !valid() {
		return errors.Join(errHideCancelled, storage(a.immersionJournal.Abandon(t.ref, "cancelled before request")))
	}
	full, err = w.State()
	if err == nil && full {
		a.debugEvent(debugFullscreenPreserved, attempt, t.ref.BundleID, 0)
		return storage(a.immersionJournal.Abandon(t.ref, "window entered fullscreen before our request"))
	}
	if err != nil || w.Changed() {
		return errors.Join(fullscreenFailure(errors.Join(errors.New("请求前窗口状态已改变，请重试"), err)),
			storage(a.immersionJournal.Abandon(t.ref, "window changed before request")))
	}
	if a.fullscreenLeases == nil {
		a.fullscreenLeases = map[journal.Ref]*m.FullscreenWindow{}
	}
	a.fullscreenLeases[t.ref] = w
	a.debugEvent(debugFullscreenRequest, attempt, t.ref.BundleID, 0)
	if err := enterFullscreenAndArmRestore(w); err != nil {
		return fullscreenFailure(err)
	}
	a.debugEvent(debugFullscreenConfirmed, attempt, t.ref.BundleID, 0)
	return storage(a.immersionJournal.Applied(id))
}

type fullscreenObservation interface {
	State() (bool, error)
	Revision() uint64
}

func enterFullscreenAndArmRestore(w interface {
	fullscreenObservation
	Set(bool) error
	ArmRestore(uint64)
}) error {
	if err := w.Set(true); err != nil {
		return err // A failed reply may still have applied the native request.
	}
	revision, err := settleFullscreen(w, true)
	if err != nil {
		return err
	}
	// AX resize events can arrive before the reply, during the animation,
	// or be coalesced. Only start watching for later edits once the same
	// window has reached a stable fullscreen state after a successful write.
	w.ArmRestore(revision)
	return nil
}

func waitFullscreen(w *m.FullscreenWindow, desired bool) error {
	_, err := settleFullscreen(w, desired)
	return err
}

func settleFullscreen(w fullscreenObservation, desired bool) (uint64, error) {
	until := time.Now().Add(4 * time.Second)
	var stable time.Time
	var revision uint64
	seen := false
	for time.Now().Before(until) {
		before := w.Revision()
		full, err := w.State()
		if err != nil {
			return 0, err
		}
		after := w.Revision()
		if full != desired {
			if seen {
				return 0, errors.New("全屏转换期间窗口再次改变，请手动核对")
			}
			stable = time.Time{}
		} else {
			seen = true
			if stable.IsZero() || before != after || revision != after {
				revision, stable = after, time.Now()
			} else if time.Since(stable) >= 900*time.Millisecond {
				return revision, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, errors.New("未观察到原生全屏转换完成")
}

func (a *application) restoreImmersion() error {
	if a.immersionJournal == nil {
		return nil
	}
	var errs []error
	for _, entry := range a.immersionJournal.Snapshot().Entries {
		if entry.Status != "pending" && entry.Status != "applied" {
			continue
		}
		r := entry.Ref
		if a.fixtureRoot != "" && !a.ownFixture(r) {
			return errors.New("隔离验收的全屏恢复记录超出允许范围")
		}
		w := a.fullscreenLeases[r]
		err := a.restoreFullscreen(entry, w, immersionInstance(r))
		if err != nil {
			a.debugEvent(debugRestoreFailed, "", r.BundleID, debugAXCode(err))
			errs = append(errs, fmt.Errorf("「%s」的全屏待处理：%w", r.Label, err))
		} else {
			a.debugEvent(debugRestoreConfirmed, "", r.BundleID, 0)
		}
		if w != nil && (err == nil || w.Changed()) {
			w.Close()
			delete(a.fullscreenLeases, r)
		}
	}
	return errors.Join(errs...)
}

func (a *application) restoreFullscreen(entry journal.Entry, w *m.FullscreenWindow, instanceErr error) error {
	r := entry.Ref
	if instanceErr != nil {
		if errors.Is(instanceErr, m.ErrInstanceGone) || errors.Is(instanceErr, m.ErrInstanceChanged) {
			return a.immersionJournal.Abandon(r, "process ended or replaced")
		}
		return instanceErr
	}
	if w == nil {
		return errors.New("没有本轮的窗口标识，保留现状")
	}
	full, err := w.State()
	if errors.Is(err, m.ErrWorkWindowGone) {
		return errors.New("原工作窗口无法识别，保留现状")
	}
	if err != nil {
		return err
	}
	if entry.Status != "applied" {
		return errors.New("全屏请求结果未确认，保留现状")
	}
	if !full {
		return a.immersionJournal.Abandon(r, "window no longer fullscreen")
	}
	if w.Changed() {
		return errors.New("无法确认当前全屏仍由本应用控制，保留现状")
	}
	if err := w.Set(false); err != nil {
		return err
	}
	if err := waitFullscreen(w, false); err != nil {
		return err
	}
	return a.immersionJournal.Release(r)
}

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
	app    session.App
	ref    journal.Ref
	window *m.FullscreenWindow
}

var errImmersionAppNotRunning = errors.New("尚未找到正在运行的应用")

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

func openMissingImmersionApps(targets []immersionTarget, refs []journal.Ref, scanErr error, open func(session.App) error) error {
	var missing []session.App
	var errs []error
	for _, t := range targets {
		_, err := immersionRunningApp(t.app, refs, scanErr)
		if errors.Is(err, errImmersionAppNotRunning) {
			missing = append(missing, t.app)
		} else if err != nil {
			errs = append(errs, fmt.Errorf("「%s」：%w", t.app.Name, err))
		}
	}
	// Reopening an already-running bundle would not establish the identity
	// of the existing process. Leave it untouched and explain the mismatch.
	if len(errs) != 0 {
		return errors.Join(errs...)
	}
	for _, app := range missing {
		if err := open(app); err != nil {
			errs = append(errs, fmt.Errorf("「%s」无法打开：%w", app.Name, err))
		}
	}
	return errors.Join(errs...)
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

func immersionPermissionIssue(err error) bool {
	return errors.Is(err, errImmersionPermission) || errors.Is(err, errImmersionPermissionCheck)
}

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

func (a *application) enterImmersiveWork(s session.State, valid func() bool) error {
	isCurrent := func() bool {
		var ok bool
		mygo.RunOnMain(func() { ok = valid() })
		return ok
	}
	if err := a.checkImmersionAccess(); err != nil {
		return err
	}
	if a.immersionJournal == nil || s.Config == nil {
		return errors.New("全屏恢复记录或本次应用配置不可用")
	}
	targets := make([]immersionTarget, 0, len(s.AllowedIDs()))
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
			return fmt.Errorf("「%s」的应用位置或身份已变化，请重新选择", item.Name)
		}
		canonical.Name = item.Name
		if canonical.Name == "" {
			canonical.Name = item.ID
		}
		targets = append(targets, immersionTarget{app: canonical})
	}
	if len(targets) == 0 || len(targets) != len(s.AllowedIDs()) {
		return errors.New("本轮有专注应用无法核对位置，请重新选择")
	}

	// Open missing applications once, then allow their windows to finish loading.
	var initialRefs []journal.Ref
	var initialErr error
	mygo.RunOnMain(func() { initialRefs, initialErr = m.Running() })
	if !isCurrent() {
		return errHideCancelled
	}
	openErr := openMissingImmersionApps(targets, initialRefs, initialErr, func(app session.App) error {
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
	if openErr != nil {
		return openErr
	}

	// Resolve all windows before mutating any. Do not use a similarly named app,
	// another copy of the bundle, or an arbitrary member of a multi-window list.
	deadline := time.Now().Add(4 * time.Second)
	for {
		if !isCurrent() {
			return errHideCancelled
		}
		var refs []journal.Ref
		var scanErr error
		mygo.RunOnMain(func() { refs, scanErr = m.Running() })
		var missing []error
		for i := range targets {
			t := &targets[i]
			if t.window != nil {
				continue
			}
			ref, err := immersionRunningApp(t.app, refs, scanErr)
			if err != nil {
				missing = append(missing, fmt.Errorf("「%s」：%w", t.app.Name, err))
				continue
			}
			w, err := m.WorkFullscreenWindow(int32(ref.PID))
			if err != nil {
				missing = append(missing, fmt.Errorf("「%s」：%w", t.app.Name, err))
				continue
			}
			t.ref, t.window = ref, w
		}
		if len(missing) == 0 {
			break
		}
		if time.Now().After(deadline) {
			return errors.Join(missing...)
		}
		time.Sleep(100 * time.Millisecond)
	}

	var preflight []error
	for _, t := range targets {
		full, err := t.window.State()
		if err == nil && !full {
			err = t.window.Writable()
			if err == nil {
				err = t.window.Watch()
			}
		}
		if err != nil {
			preflight = append(preflight, fmt.Errorf("「%s」：%w", t.app.Name, err))
		}
	}
	if len(preflight) != 0 {
		return errors.Join(preflight...)
	}
	for i, t := range targets {
		if err := a.fullscreenTarget(t, isCurrent); err != nil {
			failed := []error{fmt.Errorf("「%s」：%w", t.app.Name, err)}
			for _, skipped := range targets[i+1:] {
				failed = append(failed, fmt.Errorf("「%s」：本轮已停止，未继续准备", skipped.app.Name))
			}
			return errors.Join(failed...)
		}
	}
	for _, t := range targets {
		if err := immersionInstance(t.ref); err != nil {
			return fmt.Errorf("「%s」在交接前退出或更换：%w", t.app.Name, err)
		}
		if full, err := t.window.State(); err != nil || !full {
			return fmt.Errorf("「%s」在交接前未保持原生全屏", t.app.Name)
		}
	}
	return a.activateImmersiveTarget(targets, valid)
}

func (a *application) activateImmersiveTarget(targets []immersionTarget, valid func() bool) error {
	target := targets[0]
	var err error
	mygo.RunOnMain(func() {
		if !valid() {
			err = errHideCancelled
			return
		}
		for _, t := range targets {
			if active, _ := m.Active(t.ref); active {
				target = t
				return
			}
			if a.lastWork != nil && *a.lastWork == t.ref {
				target = t
			}
		}
		err = m.Activate(target.ref)
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
			// An existing fullscreen belongs to the user. macOS may keep a
			// different window of that app focused; do not force it to switch.
			if a.fullscreenLeases[target.ref] == nil {
				if full, stateErr := target.window.State(); stateErr == nil && full {
					mygo.RunOnMain(func() { a.lastWork = &target.ref })
					return nil
				}
			}
			if current, focusErr := m.FocusedFullscreenWindow(int32(target.ref.PID)); focusErr == nil {
				match := target.window.Matches(current)
				current.Close()
				if match {
					if full, stateErr := target.window.State(); stateErr != nil || !full {
						return fmt.Errorf("「%s」已离开原生全屏", target.app.Name)
					}
					mygo.RunOnMain(func() { a.lastWork = &target.ref })
					return nil
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("macOS 未将前台交给选定的全屏应用")
}

func (a *application) fullscreenTarget(t immersionTarget, valid func() bool) error {
	w := t.window
	if !valid() {
		return errHideCancelled
	}
	if err := immersionInstance(t.ref); err != nil {
		return err
	}
	full, err := w.State()
	if err != nil || full {
		return err
	}
	if !w.Watching() || w.Changed() {
		return errors.New("工作窗口在准备时发生变化，请重试")
	}
	id := fmt.Sprintf("fullscreen-%d-%d", t.ref.PID, time.Now().UnixNano())
	if err := a.immersionJournal.Begin(id, t.ref, false); err != nil {
		return err
	}
	if !valid() {
		return errors.Join(errHideCancelled, a.immersionJournal.Abandon(t.ref, "cancelled before request"))
	}
	full, err = w.State()
	if err == nil && full {
		return a.immersionJournal.Abandon(t.ref, "window entered fullscreen before our request")
	}
	if err != nil || w.Changed() {
		return errors.Join(errors.New("请求前窗口状态已改变，请重试"), err, a.immersionJournal.Abandon(t.ref, "window changed before request"))
	}
	if a.fullscreenLeases == nil {
		a.fullscreenLeases = map[journal.Ref]*m.FullscreenWindow{}
	}
	a.fullscreenLeases[t.ref] = w
	if err := enterFullscreenAndArmRestore(w); err != nil {
		return err
	}
	return a.immersionJournal.Applied(id)
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
			errs = append(errs, fmt.Errorf("「%s」的全屏待处理：%w", r.Label, err))
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

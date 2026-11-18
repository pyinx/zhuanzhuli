//go:build darwin

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"focus.local/zhuanzhuli/internal/notification"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"focus.local/zhuanzhuli/pkg/journal"
)

func TestImmersionContinuesWithoutAccessibility(t *testing.T) {
	readError := errors.New("permission API unavailable")
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{{"denied", nil, errImmersionPermission}, {"query error", readError, errImmersionPermissionCheck}} {
		t.Run(tc.name, func(t *testing.T) {
			a := &application{readImmersionAccess: func() (bool, error) { return false, tc.err }}
			outcome := immersionOutcome{selected: 1}
			err := a.tryImmersionWindows(&outcome, []immersionTarget{{}}, func() bool { return true }, "", func(*immersionTarget) error {
				t.Fatal("AX operation was attempted without a verified grant")
				return nil
			})
			if err != nil || !errors.Is(outcome.accessErr, tc.want) || outcome.fullscreen != 0 || outcome.caption() == "" {
				t.Fatalf("missing permission blocked or misreported the attempt: %+v, %v", outcome, err)
			}
		})
	}
}

func TestImmersionContinuesAfterWindowFailures(t *testing.T) {
	a := &application{readImmersionAccess: func() (bool, error) { return true, nil }}
	targets := []immersionTarget{{app: session.App{ID: "first"}}, {app: session.App{ID: "second"}}}
	for _, succeeds := range []bool{true, false} {
		outcome := immersionOutcome{selected: len(targets)}
		var attempted []string
		err := a.tryImmersionWindows(&outcome, targets, func() bool { return true }, "", func(target *immersionTarget) error {
			attempted = append(attempted, target.app.ID)
			if succeeds && target.app.ID == "second" {
				return nil
			}
			return errors.New("window vanished")
		})
		want := 0
		if succeeds {
			want = 1
		}
		if err != nil || strings.Join(attempted, ",") != "first,second" || outcome.fullscreen != want || outcome.caption() == "" {
			t.Fatalf("a window failure blocked another target or the session: %v, %+v, %v", attempted, outcome, err)
		}
	}
	cancelled := false
	err := a.tryImmersionWindows(&immersionOutcome{}, targets, func() bool { return !cancelled }, "", func(target *immersionTarget) error {
		if target.app.ID != "first" {
			t.Fatal("an ended session attempted another window")
		}
		cancelled = true
		return errors.New("window vanished")
	})
	if !errors.Is(err, errHideCancelled) {
		t.Fatalf("cancellation must still stop the attempt: %v", err)
	}

	var handedOff string
	if err := tryImmersionHandoffs(targets, func(target immersionTarget) error {
		if target.app.ID == "first" {
			return errors.New("first verified application disappeared during handoff")
		}
		handedOff = target.app.ID
		return nil
	}); err != nil || handedOff != "second" {
		t.Fatalf("the next verified application was not tried: %q, %v", handedOff, err)
	}
}

func TestOldEnvironmentRecordsAreIgnoredWithoutReplayingEffects(t *testing.T) {
	dir := t.TempDir()
	open := func(name string) *journal.Journal {
		t.Helper()
		j, err := journal.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	hidden, fullscreen := open("environment.json"), open("fullscreen.json")
	ref := journal.Ref{PID: os.Getpid(), Launched: 1, Token: "earlier-run"}
	for _, j := range []*journal.Journal{hidden, fullscreen} {
		if err := j.Begin("unknown", ref, false); err != nil {
			t.Fatal(err)
		}
		if err := j.Begin("applied", ref, false); err != nil {
			t.Fatal(err)
		}
		if err := j.Applied("applied"); err != nil {
			t.Fatal(err)
		}
	}
	lease := filepath.Join(dir, "notifications.json")
	if err := os.WriteFile(lease, []byte(`{"version":1,"profile":"上次的模式","status":"applied"}`), 0600); err != nil {
		t.Fatal(err)
	}
	link, err := notification.Open(lease)
	if err != nil {
		t.Fatal(err)
	}
	a := &application{journal: hidden, immersionJournal: fullscreen, notifications: link}
	if err := a.ignoreEnvironment(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"environment.json", "fullscreen.json"} {
		entries := open(name).Snapshot().Entries
		if len(entries) != 2 || entries[0].Status != "abandoned" || entries[1].Status != "abandoned" {
			t.Fatalf("old %s records are still actionable or were deleted: %v", name, entries)
		}
	}
	if link, err = notification.Open(lease); err != nil || link.Pending() {
		t.Fatalf("old Focus mode remains owned after restart: %v", err)
	}
	if err := hidden.Begin("new", ref, false); err != nil || hidden.Pending() != 1 {
		t.Fatalf("new effects cannot be recorded after ignoring old effects: %v", err)
	}
}

func TestImmersiveLaunchRefusesAlternateRunningLocation(t *testing.T) {
	editor := session.App{ID: "org.example.editor", Name: "编辑器", Path: "/Applications/Editor.app"}
	browser := session.App{ID: "org.example.browser", Name: "浏览器", Path: "/Applications/Browser.app"}
	refs := []journal.Ref{{PID: 7, BundleID: editor.ID, Path: "/private/var/folders/run/AppTranslocation/abc/d/Editor.app"}}
	targets := []immersionTarget{{app: browser}, {app: editor}}
	var launched []session.App
	open := func(app session.App) error { launched = append(launched, app); return nil }
	skipped := openMissingImmersionApps(targets, refs, nil, open)
	if err := skipped[editor.ID]; err == nil || !strings.Contains(err.Error(), "所选位置不同") || len(launched) != 1 || launched[0] != browser {
		t.Fatalf("only the alternate copy must be skipped: launched=%v, skipped=%v", launched, skipped)
	}
	launched = nil
	unidentified := &m.AppIdentityError{PID: 7, BundleID: editor.ID}
	skipped = openMissingImmersionApps(targets, nil, errors.Join(unidentified), open)
	if err := skipped[editor.ID]; err == nil || !strings.Contains(err.Error(), "无法核对运行实例") || len(launched) != 1 || launched[0] != browser {
		t.Fatalf("an unverified selected process must not be relaunched: launched=%v, skipped=%v", launched, skipped)
	}
	launched = nil
	refs = append(refs, journal.Ref{PID: 8, BundleID: editor.ID, Path: editor.Path})
	unrelated := &m.AppIdentityError{PID: 9, BundleID: "org.example.unrelated"}
	if skipped := openMissingImmersionApps(targets, refs, unrelated, open); len(skipped) != 0 || len(launched) != 1 || launched[0] != browser {
		t.Fatalf("the selected running copy should be reused; only a missing app should open: launched=%v, skipped=%v", launched, skipped)
	}
	refs = append(refs, journal.Ref{PID: 9, BundleID: editor.ID, Path: editor.Path})
	if _, err := immersionRunningApp(editor, refs, nil); err == nil {
		t.Fatal("ambiguous matching instances must not be targeted")
	}
}

type fullscreenTrace struct {
	states     []bool
	revision   uint64
	resize     bool
	armed      *uint64
	requestErr error
}

func (w *fullscreenTrace) Set(bool) error {
	if w.resize {
		w.revision += 2 // Resize notifications can precede the reply.
	}
	return w.requestErr
}
func (w *fullscreenTrace) State() (bool, error) {
	full := w.states[0]
	if len(w.states) > 1 {
		if w.resize {
			w.revision++
		}
		w.states = w.states[1:]
	}
	return full, nil
}
func (w *fullscreenTrace) Revision() uint64      { return w.revision }
func (w *fullscreenTrace) ArmRestore(rev uint64) { w.armed = &rev }

func TestFullscreenAcquisitionArmsOnlyAfterAcknowledgedStableTransition(t *testing.T) {
	t.Run("normal animation with multiple events", func(t *testing.T) {
		w := &fullscreenTrace{states: []bool{false, true, true, true}, resize: true}
		if err := enterFullscreenAndArmRestore(w); err != nil || w.armed == nil || *w.armed != w.revision {
			t.Fatalf("the completed transition must be eligible for restoration: armed=%v, err=%v", w.armed, err)
		}
	})
	t.Run("coalesced events", func(t *testing.T) {
		w := &fullscreenTrace{states: []bool{false, true}}
		if err := enterFullscreenAndArmRestore(w); err != nil || w.armed == nil {
			t.Fatalf("a stable acknowledged write without notifications still needs recovery: %v", err)
		}
	})
	t.Run("observed reversal", func(t *testing.T) {
		w := &fullscreenTrace{states: []bool{true, false}}
		if err := enterFullscreenAndArmRestore(w); err == nil || w.armed != nil {
			t.Fatalf("a reversed transition must remain unconfirmed: armed=%v, err=%v", w.armed, err)
		}
	})
	t.Run("unacknowledged write", func(t *testing.T) {
		w := &fullscreenTrace{requestErr: errors.New("AX reply was lost")}
		if err := enterFullscreenAndArmRestore(w); err == nil || w.armed != nil {
			t.Fatalf("an uncertain write must remain unconfirmed: armed=%v, err=%v", w.armed, err)
		}
	})
}

func TestApplicationConsentDoesNotAcceptOtherFailures(t *testing.T) {
	app := protectionIssue("应用限制", "editor:123:start1", errors.New("Editor 无法确认收起"))
	consent := map[string]bool{"editor:123:start1": true}
	if !consentedApplications(app, consent) {
		t.Fatal("explicitly listed application should be accepted")
	}
	for _, other := range []error{protectionIssue("通知联动", "", errors.New("改动待核对")), protectionIssue("存储", "", errors.New("磁盘已满")), protectionIssue("应用限制", "editor:123:start2", errors.New("新实例")), errors.New("未知失败")} {
		if consentedApplications(errors.Join(app, other), consent) {
			t.Fatalf("application consent swallowed %v", other)
		}
	}
}
func TestResumePlanKeepsClockPausedAndRejectsCancelledOperation(t *testing.T) {
	now := time.Now()
	c := session.New(func() time.Time { return now })
	if err := c.Start(session.State{ID: "one", Goal: "写作", Minutes: 15, Allowed: []string{"editor"}, Partial: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Enter(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	c.Pause()
	a := &application{controller: c, store: &store.Store{Data: store.Defaults()}}
	a.store.Data.Preferences.FocusLinked = true
	a.store.Data.Preferences.FocusProfile = "本次名称"
	defaults := []string{"editor"}
	p := a.newProtectionPlan(true, defaults, nil)
	defaults[0] = "changed"
	a.store.Data.Preferences.FocusLinked = false
	a.store.Data.Preferences.FocusProfile = "下次名称"
	if !p.focusLinked || p.focusProfile != "本次名称" {
		t.Fatal("notification settings changed inside the captured preparation")
	}
	now = now.Add(30 * time.Second)
	if c.Snapshot().Elapsed != time.Second || !a.planValid(p) || p.allowed[0] != "editor" {
		t.Fatal("preparation changed clock or captured mutable input")
	}
	a.protectionGeneration++
	if a.planValid(p) {
		t.Fatal("cancelled completion still valid")
	}
	q := a.newProtectionPlan(true, []string{"editor"}, nil)
	if !a.planValid(q) {
		t.Fatal("new retry invalid")
	}
	if err := c.ResumeWithApps(true, q.allowed); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Elapsed != time.Second || a.planValid(q) {
		t.Fatal("resume included preparation time or allowed duplicate completion")
	}
}

func TestImmersionNeverInheritsPreviouslyEnabledSystemFocus(t *testing.T) {
	c := session.New(time.Now)
	if err := c.Start(session.State{ID: "immersive", Goal: "写作", Minutes: 15, Allowed: []string{"editor"}, Partial: true, Immersion: true}); err != nil {
		t.Fatal(err)
	}
	a := &application{controller: c, store: &store.Store{Data: store.Defaults()}}
	a.store.Data.Preferences.FocusLinked = true
	a.store.Data.Preferences.FocusProfile = "旧模式"
	check := func(p *protectionPlan) {
		t.Helper()
		if !p.immersion || p.focusLinked || p.focusProfile != "" {
			t.Fatal("immersive preparation inherited notification linkage")
		}
	}
	check(a.newProtectionPlan(false, []string{"editor"}, nil))
	c.Pause()
	check(a.newProtectionPlan(true, []string{"editor"}, nil))
	if err := c.UseOrdinaryFocus(func(session.State) error { return errors.New("write failed") }); err == nil || !c.Snapshot().Immersion {
		t.Fatal("failed save changed the session's protection mode")
	}
	if err := c.UseOrdinaryFocus(func(s session.State) error {
		if s.Immersion || s.Phase != "paused" || s.Elapsed != 0 {
			t.Fatal("fallback should only disable immersion without starting the clock")
		}
		return nil
	}); err != nil || c.Snapshot().Immersion || c.Snapshot().Elapsed != 0 {
		t.Fatal("fallback did not preserve the paused timer")
	}
}

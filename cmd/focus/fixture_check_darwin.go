//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"focus.local/zhuanzhuli/pkg/journal"
	probe "focus.local/zhuanzhuli/pkg/observe"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"path/filepath"
	"strings"
	"time"
)

func (a *application) ownFixture(r journal.Ref) bool {
	for _, label := range []string{"A", "B", "C"} {
		bid := "local.focusprobe.fixture." + strings.ToLower(label)
		if r.Path == filepath.Join(a.fixtureRoot, "Fixture"+label+".app") && r.BundleID == bid {
			return true
		}
	}
	return false
}

func (a *application) checkFixturePickerRepeat(first, second journal.Ref) (err error) {
	a.ensureFocusDraft()
	original := a.focusDraft.config.Clone()
	defer func() {
		if a.appPicker != nil {
			a.appPicker.Close()
		}
		a.focusDraft.config = original
		a.renderReady()
	}()
	apps := []store.App{{ID: first.BundleID, Name: first.Label, Path: first.Path}, {ID: second.BundleID, Name: second.Label, Path: second.Path}}
	a.focusDraft.config.Apps = apps
	a.openMain()
	a.renderReady()
	a.focusForm.apps.Reload(0)
	a.openApplicationPicker(a.focusForm)
	if a.appPicker == nil || a.pickerList == nil {
		return fmt.Errorf("owned picker did not open")
	}
	if !a.pickerList.SelectID(first.BundleID) {
		return fmt.Errorf("owned first app missing from picker")
	}
	root := m.Send(m.ID(a.appPicker.NativeHandle()), "contentView")
	m.Send(nativeButton(root, "添加所选"), "performClick:", m.ID(0))
	if a.appPicker != nil || len(a.focusDraft.config.Apps) != 2 {
		return fmt.Errorf("repeat selection lost an existing application")
	}
	found := false
	for _, v := range a.focusDraft.config.Apps {
		if v.ID == first.BundleID {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("repeated app was not retained")
	}
	return nil
}

func (a *application) validateFixtureRecovery() error {
	if a.fixtureRoot == "" {
		return nil
	}
	for _, entry := range a.journal.Snapshot().Entries {
		if (entry.Status == "pending" || entry.Status == "applied") && !a.ownFixture(entry.Ref) {
			return fmt.Errorf("fixture recovery contains an out-of-scope record; original records were not changed")
		}
	}
	return nil
}

// Runs production effects against only identity-checked fixtures. Inputs are
// scripted; activation is restricted to this host and its two owned fixtures.
// No notification switching, synthesized input or third-party-app effects.
func (a *application) fixtureCheck() {
	result := map[string]any{"evidenceKind": "automated-real-app-effects", "userInteractionVerified": false}
	var refs []journal.Ref
	mygo.RunOnMain(func() {
		running, _ := m.Running()
		for _, r := range running {
			if a.ownFixture(r) {
				refs = append(refs, r)
			}
		}
	})
	var first, second journal.Ref
	for _, r := range refs {
		if r.BundleID == "local.focusprobe.fixture.a" {
			first = r
		} else {
			second = r
		}
	}
	defer func() {
		if e := a.restore(); e != nil {
			result["cleanupError"] = e.Error()
		}
		result["pendingAtEnd"] = a.journal.Pending()
		if e := journal.WriteJSON(filepath.Join(a.dir, "fixture-check.json"), result); e != nil {
			fmt.Println(e)
		}
		mygo.RunOnMain(func() { a.controller.End(); a.quit() })
	}()
	if len(refs) != 2 || first.PID == 0 || second.PID == 0 {
		result["error"] = "two identity-checked fixtures required"
		return
	}
	// LaunchServices activation is asynchronous. A real user starts from the
	// visible foreground window; do not begin that scenario before it exists.
	initialContext, cancelInitial := context.WithTimeout(context.Background(), 5*time.Second)
	initialTick := time.NewTicker(40 * time.Millisecond)
	initialStarted := time.Now()
	_, initialErr := probe.AwaitChange(initialContext, func() error {
		mygo.RunOnMain(func() { mygo.App.Focus(); a.main.Focus() })
		return nil
	}, func() (bool, error) {
		var ready bool
		mygo.RunOnMain(func() { ready = m.SelfActive() && a.main.IsVisible() && a.main.IsFocused() })
		return ready, nil
	}, initialTick.C)
	initialTick.Stop()
	cancelInitial()
	result["initialForegroundWaitMS"] = time.Since(initialStarted).Milliseconds()
	if initialErr != nil {
		result["error"] = "fixture host foreground precondition: " + initialErr.Error()
		return
	}
	var pickerErr error
	mygo.RunOnMain(func() { pickerErr = a.checkFixturePickerRepeat(first, second) })
	result["pickerRepeatKeepsSelection"] = pickerErr == nil
	if pickerErr != nil {
		result["error"] = pickerErr.Error()
		return
	}
	// A saved selection may no longer resolve to a running or launchable app.
	// Report that directly instead of searching for 12 seconds without an open request.
	mygo.RunOnMain(func() {
		e := a.controller.Start(session.State{ID: "unavailable-return", Goal: "应用已不可用返回回归", Minutes: 30, Scene: "forest", Allowed: []string{"local.focusprobe.fixture.missing"}, Partial: true})
		if e == nil {
			e = a.controller.Enter()
		}
		if e != nil {
			result["error"] = e.Error()
		}
	})
	if result["error"] != nil {
		return
	}
	started := time.Now()
	missingErr := a.returnToWork()
	result["unavailableReturnMilliseconds"] = time.Since(started).Milliseconds()
	if missingErr != nil {
		result["unavailableReturnError"] = missingErr.Error()
	}
	result["unavailableReturnPrompt"] = missingErr != nil && strings.Contains(missingErr.Error(), "本次专注应用已不可用") && time.Since(started) < time.Second
	mygo.RunOnMain(func() { a.controller.End() })
	if result["unavailableReturnPrompt"] != true {
		result["error"] = fmt.Sprintf("unavailable return: %v", missingErr)
		return
	}
	hidden := func(r journal.Ref) (bool, error) {
		var v bool
		var e error
		mygo.RunOnMain(func() { v, e = m.IsHidden(r) })
		return v, e
	}
	if h, e := hidden(second); e != nil || h {
		result["error"] = "fixture B must start visible"
		return
	}
	begin := func() error {
		var e error
		mygo.RunOnMain(func() {
			a.stopSceneAudio()
			a.controller.End()
			e = a.controller.Start(session.State{ID: fmt.Sprint(time.Now().UnixNano()), Goal: "自有夹具系统验证", Minutes: 30, Scene: "forest", Allowed: []string{first.BundleID}, Partial: true})
			if e == nil {
				e = a.controller.Enter()
			}
		})
		return e
	}
	check := func(stage string, fn func() error) bool {
		if e := fn(); e != nil {
			result["error"] = stage + ": " + e.Error()
			return false
		}
		return true
	}
	if !check("start", begin) || !check("protect", a.protect) {
		return
	}
	if !check("return to available fixture", a.returnToWork) {
		return
	}
	var returned bool
	mygo.RunOnMain(func() { returned, _ = m.Active(first) })
	result["availableReturnActivated"] = returned
	// Match the production entry path: its control window is hidden and the
	// allowed work app is already frontmost. Returning must not activate the
	// host just to hand focus straight back, or wait for that unnecessary step.
	var hostActivated bool
	var offActivation func()
	mygo.RunOnMain(func() {
		a.main.Hide()
		offActivation = mygo.App.OnDidBecomeActive(func() { hostActivated = true })
	})
	alreadyStarted := time.Now()
	alreadyErr := a.returnToWork()
	result["alreadyActiveReturnMilliseconds"] = time.Since(alreadyStarted).Milliseconds()
	mygo.RunOnMain(func() {
		offActivation()
		active, e := m.Active(first)
		result["alreadyActiveReturnPreserved"] = alreadyErr == nil && e == nil && active && !hostActivated
		result["alreadyActiveReturnHostActivated"] = hostActivated
		a.main.ShowInactive()
	})
	if result["alreadyActiveReturnPreserved"] != true {
		result["error"] = fmt.Sprintf("already active return: %v; host activated: %v", alreadyErr, hostActivated)
		return
	}
	h, e := hidden(second)
	result["excludedHidden"] = e == nil && h
	ah, ae := hidden(first)
	result["allowedVisible"] = ae == nil && !ah
	if !check("restore", a.restore) {
		return
	}
	h, e = hidden(second)
	result["pauseRestores"] = e == nil && !h
	activateOwned := func(ref journal.Ref, observe func() (bool, error)) error {
		activation, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		_, err := probe.AwaitChange(activation, func() error { mygo.RunOnMain(mygo.App.Focus); return nil }, func() (bool, error) {
			var active bool
			mygo.RunOnMain(func() { active = m.SelfActive() })
			return active, nil
		}, tick.C)
		if err != nil {
			return err
		}
		_, err = probe.AwaitChange(activation, func() error {
			var e error
			mygo.RunOnMain(func() { e = m.Activate(ref) })
			return e
		}, observe, tick.C)
		return err
	}
	outside := func(ref journal.Ref) error {
		return activateOwned(ref, func() (bool, error) {
			var ready bool
			var err error
			mygo.RunOnMain(func() {
				h, e := m.IsHidden(ref)
				err = e
				if a.choice != nil && a.pending != nil && a.pending.PID == ref.PID {
					w := m.ID(a.choice.NativeHandle())
					ready = h && objc.Send[bool](w, m.S("isVisible")) && objc.Send[bool](w, m.S("isOnActiveSpace"))
				}
			})
			return ready, err
		})
	}
	e = outside(second)
	result["outsideActivationShowsChoice"] = e == nil
	mygo.RunOnMain(func() { a.pending = nil; a.closeChoice() })
	if e != nil {
		result["error"] = "outside activation: " + e.Error()
		return
	}
	if !check("restore outside B", a.restore) {
		return
	}
	cancelledHide := a.hideFor(second, nil, func() bool { return false })
	visibleAfterCancel, hiddenErr := hidden(second)
	result["supersededHidePreservedVisible"] = errors.Is(cancelledHide, errHideCancelled) && hiddenErr == nil && !visibleAfterCancel && a.journal.Pending() == 0
	if result["supersededHidePreservedVisible"] != true {
		result["error"] = fmt.Sprintf("cancelled outside hide: %v; hidden=%t (%v)", cancelledHide, visibleAfterCancel, hiddenErr)
		return
	}
	// Reverse the allowed set, so both independent application identities
	// cross the same event, hide, observation and cleanup path.
	mygo.RunOnMain(func() {
		a.controller.End()
		e = a.controller.Start(session.State{ID: "outside-reverse", Goal: "验证另一应用", Minutes: 30, Scene: "forest", Allowed: []string{second.BundleID}, Partial: true})
		if e == nil {
			e = a.controller.Enter()
		}
	})
	if e == nil {
		e = outside(first)
	}
	result["outsideAlternateAppHidden"] = e == nil
	mygo.RunOnMain(func() { a.pending = nil; a.closeChoice() })
	if e != nil {
		result["error"] = "outside alternate activation: " + e.Error()
		return
	}
	if !check("restore outside A", a.restore) {
		return
	}
	if !check("activate allowed B", func() error {
		return activateOwned(second, func() (bool, error) {
			var ready bool
			var err error
			mygo.RunOnMain(func() { ready, err = m.Active(second) })
			return ready, err
		})
	}) {
		return
	}
	// A different allowed app can become frontmost during entry, while the
	// last-work cache still points to the previous app. Preserve actual work.
	mygo.RunOnMain(func() {
		a.controller.Allow(second.BundleID)
		a.lastWork = &first
		hostActivated = false
		a.main.Hide()
		offActivation = mygo.App.OnDidBecomeActive(func() { hostActivated = true })
	})
	currentErr := a.returnToWork()
	mygo.RunOnMain(func() {
		offActivation()
		active, e := m.Active(second)
		result["currentAllowedWorkPreserved"] = currentErr == nil && e == nil && active && !hostActivated
		result["currentAllowedReturnHostActivated"] = hostActivated
		a.main.ShowInactive()
	})
	if result["currentAllowedWorkPreserved"] != true {
		result["error"] = fmt.Sprintf("current allowed return: %v; host activated: %v", currentErr, hostActivated)
		return
	}
	mygo.RunOnMain(func() { a.controller.Allow(second.BundleID); a.controller.Pause(); e = a.controller.Resume(true) })
	if e != nil || !check("resume protection", a.protect) {
		return
	}
	h, e = hidden(second)
	result["allowancePreservedOnResume"] = e == nil && !h
	if !check("repeat restore", a.restore) || !check("repeat restore again", a.restore) {
		return
	}
	result["repeatCleanupReleased"] = a.journal.Pending() == 0
	// A return from another foreground fixture should hand off directly to the
	// selected work app. Reactivating the hidden host introduces an unrelated
	// foreground wait (the stage implicated by earlier timeout evidence).
	if !check("direct return setup", begin) {
		return
	}
	mygo.RunOnMain(func() {
		active, err := m.Active(second)
		result["directReturnStartedOutside"] = err == nil && active
		a.main.Hide()
		hostActivated = false
		offActivation = mygo.App.OnDidBecomeActive(func() { hostActivated = true })
	})
	directStarted := time.Now()
	directErr := a.returnToWork()
	mygo.RunOnMain(func() {
		offActivation()
		active, err := m.Active(first)
		outsideActive, outsideErr := m.Active(second)
		result["directReturnActivated"] = directErr == nil && err == nil && active
		result["directReturnRefused"] = directErr != nil && errors.Is(directErr, m.ErrActivationDenied) && err == nil && outsideErr == nil && outsideActive && time.Since(directStarted) < time.Second
		result["directReturnWithoutHostActivation"] = (result["directReturnActivated"] == true || result["directReturnRefused"] == true) && !hostActivated && result["directReturnStartedOutside"] == true
		if directErr != nil {
			result["directReturnError"] = directErr.Error()
		}
		result["directReturnHostActivated"] = hostActivated
		result["directReturnMilliseconds"] = time.Since(directStarted).Milliseconds()
		a.main.ShowInactive()
	})
	if result["directReturnWithoutHostActivation"] != true {
		result["error"] = fmt.Sprintf("direct return: %v; host activated: %v", directErr, hostActivated)
		return
	}
	// Exercise the actual entry callback. Keep its window until the target
	// becomes active, so macOS receives the handoff from the foreground app.
	mygo.RunOnMain(func() {
		a.controller.End()
		cfg := a.store.Data.Preferences.Config()
		cfg.Scene, cfg.Sound, cfg.Volume = "pond", true, .000001
		cfg.Apps = []store.App{{ID: first.BundleID, Name: first.Label, Path: first.Path}}
		e = a.controller.Start(session.State{ID: "entry-handoff", Goal: "入场交接回归", Config: &cfg, Partial: true})
		if e == nil {
			a.beginCountdown(a.beginEntry)
		}
	})
	if e != nil {
		result["error"] = e.Error()
		return
	}
	entryContext, cancelEntryContext := context.WithTimeout(context.Background(), 8*time.Second)
	entryTick := time.NewTicker(40 * time.Millisecond)
	defer cancelEntryContext()
	defer entryTick.Stop()
	var countdown string
	sampleCountdown := func() {
		number := m.Value(a.countdownOverlay.Label)
		if number != "" && !a.entryPresentation && a.video.Player == 0 && a.audio.Player == 0 && a.controller.Snapshot().Elapsed == 0 && !strings.HasSuffix(countdown, number) {
			countdown += number
		}
	}
	_, e = probe.AwaitChange(entryContext, func() error { return nil }, func() (bool, error) {
		var active bool
		mygo.RunOnMain(func() {
			sampleCountdown()
			// NSRunningApplication may report foreground ownership before AppKit
			// delivers activation and makes the entry window key. Observe both
			// before counting subsequent activations as handoff regressions.
			app := m.Send(m.Class("NSApplication"), "sharedApplication")
			readiness := map[string]bool{
				"selfActive": m.SelfActive(), "appActive": objc.Send[bool](app, m.S("isActive")),
				"entryKey": a.entry != nil && a.entry.IsFocused(),
			}
			result["entryForegroundReadiness"] = readiness
			active = readiness["selfActive"] && readiness["appActive"] && readiness["entryKey"]
		})
		return active, nil
	}, entryTick.C)
	if e != nil {
		result["error"] = "entry foreground setup: " + e.Error()
		return
	}
	_, e = probe.AwaitChange(entryContext, func() error { return nil }, func() (bool, error) {
		var ready bool
		mygo.RunOnMain(func() {
			sampleCountdown()
			ready = a.video.Player != 0
		})
		return ready, nil
	}, entryTick.C)
	result["countdownBeforeVideo"] = e == nil && countdown == "321"
	if result["countdownBeforeVideo"] != true {
		result["error"] = fmt.Sprintf("entry countdown sequence %q: %v", countdown, e)
		return
	}
	mygo.RunOnMain(func() {
		result["entryAudioPlayer"] = uintptr(a.audio.Player)
		hostActivated = false
		offActivation = mygo.App.OnDidBecomeActive(func() { hostActivated = true })
		a.enter()
		result["entryWindowRetainedUntilHandoff"] = a.entry != nil && a.entry.IsVisible()
	})
	_, e = probe.AwaitChange(entryContext, func() error { return nil }, func() (bool, error) {
		var done bool
		mygo.RunOnMain(func() { done = !a.busy })
		return done, nil
	}, entryTick.C)
	mygo.RunOnMain(func() {
		offActivation()
		active, err := m.Active(first)
		result["entryHandoffActivatedWork"] = e == nil && err == nil && active && a.entry != nil && !a.entry.IsVisible() && !a.main.IsVisible() && !hostActivated && result["entryWindowRetainedUntilHandoff"] == true
		result["entryAudioContinuesAfterHandoff"] = a.audio.Player != 0 && result["entryAudioPlayer"] == uintptr(a.audio.Player) && a.audioScene == "pond" && objc.Send[bool](a.audio.Player, m.S("isPlaying"))
	})
	if result["entryHandoffActivatedWork"] != true {
		result["error"] = fmt.Sprintf("entry handoff: %v; host activated: %v", e, hostActivated)
		return
	}
	if !check("new session", begin) || !check("setup originally hidden", func() error { return a.waitHidden(second, true) }) {
		return
	}
	defer func() {
		if e := a.waitHidden(second, false); e != nil {
			result["setupCleanupError"] = e.Error()
		}
	}()
	if !check("originally hidden protect", a.protect) || !check("originally hidden restore", a.restore) {
		return
	}
	h, e = hidden(second)
	result["originallyHiddenPreserved"] = e == nil && h && a.journal.Pending() == 0
	if !check("undo setup", func() error { return a.waitHidden(second, false) }) || !check("owned hide", func() error { return a.hide(second) }) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	// No expected marker: synthetic external request, followed by a real OS event.
	_, e = probe.AwaitChange(ctx, func() error { var e error; mygo.RunOnMain(func() { e = m.Hide(second, false) }); return e }, func() (bool, error) { h, e := hidden(second); return !h && a.journal.Pending() == 0, e }, tick.C)
	if e != nil {
		result["error"] = "external change: " + e.Error()
		return
	}
	if !check("preserve external", a.restore) {
		return
	}
	h, e = hidden(second)
	result["externalChangePreserved"] = e == nil && !h
	if !check("end during effect setup", begin) {
		return
	}
	ready := make(chan error, 1)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	mygo.RunOnMain(func() {
		a.effect(func() error {
			err := a.hide(second)
			ready <- err
			if err == nil {
				<-release
			}
			return err
		}, nil)
	})
	select {
	case err := <-ready:
		if err != nil {
			result["error"] = err.Error()
			return
		}
	case <-time.After(8 * time.Second):
		result["error"] = "gated real effect did not start"
		return
	}
	immediate := false
	mygo.RunOnMain(func() { a.end(); a.end(); immediate = a.controller.Snapshot().Phase == "ended" })
	close(release)
	released = true
	drain, cancelDrain := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancelDrain()
	_, e = probe.AwaitChange(drain, func() error { return nil }, func() (bool, error) { var busy bool; mygo.RunOnMain(func() { busy = a.busy }); return !busy, nil }, tick.C)
	h, he := hidden(second)
	var count int
	mygo.RunOnMain(func() { count = len(a.store.Data.History) })
	result["endDuringEffectRestores"] = e == nil && he == nil && !h && immediate && a.journal.Pending() == 0 && count == 1
	// A delayed request must not create another hide after the session ended.
	e = a.waitHidden(second, true)
	h, he = hidden(second)
	result["noHideAfterEnd"] = e != nil && he == nil && !h
	for _, key := range []string{"unavailableReturnPrompt", "availableReturnActivated", "alreadyActiveReturnPreserved", "currentAllowedWorkPreserved", "excludedHidden", "allowedVisible", "pauseRestores", "outsideActivationShowsChoice", "allowancePreservedOnResume", "repeatCleanupReleased", "originallyHiddenPreserved", "externalChangePreserved", "endDuringEffectRestores", "noHideAfterEnd"} {
		if result[key] != true {
			result["error"] = "assertion failed: " + key
			return
		}
	}
}

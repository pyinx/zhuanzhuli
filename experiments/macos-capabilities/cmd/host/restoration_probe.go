package main

import (
	"context"
	"fmt"
	"time"

	"focus.local/macos-capabilities/internal/macos"
	probe "focus.local/zhuanzhuli/pkg/observe"
	"github.com/egoist/mygo"
)

// diagnoseRestoration uses only B. The external event is deliberately generated
// by the test without the host's expected-change marker, not by a human.
func (h *host) diagnoseRestoration() (result map[string]any) {
	result = map[string]any{"evidenceKind": "automated-real", "externalActor": "synthetic test request; real NSWorkspace event"}
	r := h.refs["B"]
	fail := func(stage string, err error) map[string]any {
		result["error"] = fmt.Sprintf("%s: %v", stage, err)
		return result
	}
	defer func() {
		if s, err := h.inspect(r); err == nil && s.Hidden {
			if h.ledger.Pending() > 0 {
				err = h.cleanup()
			} else {
				err = h.request(r, false) // Undo only this test's synthetic baseline.
			}
			if err != nil {
				result["setupCleanupError"] = err.Error()
			}
		}
		result["pendingAtEnd"] = h.ledger.Pending()
	}()
	initial, err := h.inspect(r)
	if err != nil || initial.Hidden || h.ledger.Pending() != 0 {
		return fail("initial baseline must be visible and released", err)
	}
	h.event("restoration-case", "originally-hidden")
	if err = h.request(r, true); err != nil {
		return fail("arrange originally hidden", err)
	}
	entries := len(h.ledger.Snapshot().Entries)
	if err = h.hideB(); err != nil {
		return fail("already-hidden entry", err)
	}
	if err = h.cleanup(); err != nil {
		return fail("already-hidden cleanup", err)
	}
	state, err := h.inspect(r)
	result["originallyHiddenPreserved"] = err == nil && state.Hidden && h.ledger.Pending() == 0 && len(h.ledger.Snapshot().Entries) == entries
	if result["originallyHiddenPreserved"] != true {
		return fail("original hidden state was not preserved", err)
	}
	if err = h.request(r, false); err != nil {
		return fail("reset synthetic baseline", err)
	}
	h.event("restoration-case", "repeat-cleanup")
	if err = h.hideB(); err != nil {
		return fail("repeat setup", err)
	}
	for i := 0; i < 2; i++ {
		if err = h.cleanup(); err != nil {
			return fail("repeat cleanup", err)
		}
	}
	state, err = h.inspect(r)
	result["repeatCleanupReleased"] = err == nil && !state.Hidden && h.ledger.Pending() == 0
	if result["repeatCleanupReleased"] != true {
		return fail("repeat cleanup did not release", err)
	}
	h.event("restoration-case", "external-unhide")
	if err = h.hideB(); err != nil {
		return fail("external event setup", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ticks := time.NewTicker(50 * time.Millisecond)
	defer ticks.Stop()
	_, err = probe.AwaitChange(ctx, func() error {
		var issueErr error
		mygo.RunOnMain(func() { issueErr = macos.RequestHidden(r, false) })
		return issueErr
	}, func() (bool, error) {
		s, inspectErr := h.inspect(r)
		return !s.Hidden && h.ledger.Pending() == 0, inspectErr
	}, ticks.C)
	if err != nil {
		return fail("external event observation", err)
	}
	snapshot := h.ledger.Snapshot()
	result["externalChangeAbandoned"] = len(snapshot.Entries) > 0 && snapshot.Entries[len(snapshot.Entries)-1].Status == "abandoned"
	if err = h.cleanup(); err != nil {
		return fail("external event cleanup", err)
	}
	state, err = h.inspect(r)
	result["externalVisiblePreserved"] = err == nil && !state.Hidden && h.ledger.Pending() == 0
	h.event("restoration-case", "temporary-allowance-pause-resume")
	mygo.RunOnMain(func() {
		for _, kind := range []string{"start", "entry-complete", "outside", "allow"} {
			if commandErr := h.dispatch(kind); commandErr != nil {
				result["error"] = commandErr.Error()
				break
			}
		}
	})
	if result["error"] != nil {
		return result
	}
	for _, kind := range []string{"pause", "resume"} {
		if err = h.scenarioAction(kind); err != nil {
			return fail("allowance "+kind, err)
		}
	}
	state, err = h.inspect(r)
	var allowed bool
	mygo.RunOnMain(func() { allowed = h.controller.Snapshot().AllowB })
	result["allowancePreservedOnResume"] = err == nil && allowed && !state.Hidden
	if err = h.scenarioAction("end"); err != nil {
		return fail("allowance final cleanup", err)
	}
	return result
}

func (h *host) scenarioAction(kind string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	_, err := probe.AwaitChange(ctx, func() error {
		var commandErr error
		mygo.RunOnMain(func() {
			h.busy = false // Dispatch the scripted choice through the real action path.
			commandErr = h.action(kind)
		})
		return commandErr
	}, func() (bool, error) {
		var complete bool
		mygo.RunOnMain(func() { complete = !h.busy })
		return complete, nil
	}, ticks.C)
	return err
}

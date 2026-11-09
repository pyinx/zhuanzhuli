//go:build darwin

package main

import (
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"testing"
)

func TestMainWindowSizesStayInsideAvailableContent(t *testing.T) {
	for _, c := range []struct{ available, initial, minimum m.Point }{
		{m.Point{X: 1400, Y: 900}, m.Point{X: 900, Y: 660}, m.Point{X: 780, Y: 580}},
		{m.Point{X: 600, Y: 400}, m.Point{X: 600, Y: 400}, m.Point{X: 600, Y: 400}},
		{m.Point{X: 760, Y: 900}, m.Point{X: 760, Y: 660}, m.Point{X: 760, Y: 580}},
		{m.Point{X: 800, Y: 500}, m.Point{X: 800, Y: 500}, m.Point{X: 780, Y: 500}},
	} {
		initial, minimum := mainWindowSizes(c.available)
		if initial != c.initial || minimum != c.minimum {
			t.Fatalf("available %v: initial %v, minimum %v", c.available, initial, minimum)
		}
	}
}

func TestSessionPrimaryTitleTracksCurrentPhaseAndFailure(t *testing.T) {
	for _, c := range []struct {
		phase string
		issue bool
		want  string
	}{
		{"active", false, "暂停"}, {"overtime", false, "暂停"},
		{"paused", false, "以部分保护恢复"}, {"paused", true, "处理准备问题…"},
	} {
		if got := sessionPrimaryTitle(session.State{Phase: c.phase}, c.issue); got != c.want {
			t.Fatalf("phase %s: %q", c.phase, got)
		}
	}
}

func TestSessionPrimaryPausesEntryBeforeWorkHasStarted(t *testing.T) {
	a := cleanupTransitionApp(t)
	a.controller.End()
	if err := a.controller.Start(session.State{ID: "entry-pause", Goal: "入场暂停回归", Minutes: 30, Scene: "forest", Allowed: []string{"local.review.fixture"}, Partial: true}); err != nil {
		t.Fatal(err)
	}
	a.sessionPrimaryAction()
	if a.controller.Snapshot().Phase != "paused" || !a.needsRestore || a.store.Data.Current == nil || a.store.Data.Current.Phase != "paused" {
		t.Fatal("entry primary did not pause and checkpoint before environment cleanup")
	}
}

func TestMergingExistingWorkAppsDoesNotRequestAnotherSelection(t *testing.T) {
	existing := []store.App{{ID: "a", Name: "A", Path: "/old/A.app"}, {ID: "b", Name: "B", Path: "/B.app"}}
	updated := store.App{ID: "a", Name: "A", Path: "/new/A.app"}
	next, selectIndex := mergeWorkApps(existing, []store.App{updated, updated})
	if selectIndex != -1 || len(next) != 2 || next[0] != updated || next[1] != existing[1] {
		t.Fatal("duplicate changed selection or list", next, selectIndex)
	}
	if existing[0].Path != "/old/A.app" {
		t.Fatal("merge changed committed preferences before save")
	}
	added := store.App{ID: "c", Name: "C", Path: "/C.app"}
	next, selectIndex = mergeWorkApps(existing, []store.App{updated, added, updated})
	if selectIndex != 2 || len(next) != 3 || next[2] != added {
		t.Fatal("new application was not selected", next, selectIndex)
	}
}

//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
)

func TestFailedWorkCommitCannotCreateAWorkHistoryRecord(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "entry", true: "first-resume"}[resume], func(t *testing.T) {
			now := time.Now()
			c := session.New(func() time.Time { return now })
			if err := c.Start(session.State{ID: "one", Goal: "合成目标", Minutes: 15, Scene: "forest", Allowed: []string{"editor"}, Partial: true}); err != nil {
				t.Fatal(err)
			}
			if resume {
				c.Pause()
			}
			s, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Checkpoint(c.Snapshot()); err != nil {
				t.Fatal(err)
			}
			original := s.Path
			blocked := filepath.Join(t.TempDir(), "blocked")
			if err = os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
			s.Path = filepath.Join(blocked, "data.json")
			if err = c.CommitWork(resume, []string{"editor"}, s.Checkpoint); err == nil {
				t.Fatal("real checkpoint write should fail")
			}
			now = now.Add(20 * time.Second)
			snapshot := c.Snapshot()
			if snapshot.Phase != "paused" || snapshot.Elapsed != 0 || snapshot.WorkStarted == nil || *snapshot.WorkStarted {
				t.Fatal("failed commit credited work", snapshot)
			}
			if s.Data.Current.WorkStarted == nil || *s.Data.Current.WorkStarted {
				t.Fatal("failed checkpoint published work provenance")
			}
			s.Path = original
			if err = s.Finish(snapshot); err != nil {
				t.Fatal(err)
			}
			if len(s.Data.History) != 0 {
				t.Fatal("never-worked session became history")
			}
		})
	}
}

func TestPreferenceFailurePreservesUnresolvedEnvironmentNotice(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A file where the parent directory must be makes a real atomic write fail,
	// including when these tests are run with permission to write read-only files.
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err = os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Path = filepath.Join(blocked, "data.json")
	a := &application{store: s, controller: session.New(time.Now), notice: Notice{noticeRecovery, "环境尚未恢复，请先核对"}}
	if a.updatePreferences(func(p *store.Preferences) { p.Sound = !p.Sound }) {
		t.Fatal("write unexpectedly succeeded")
	}
	if a.notice.Text != "环境尚未恢复，请先核对" {
		t.Fatalf("settings replaced unresolved environment notice: %q", a.notice.Text)
	}
	if s.Data.Preferences.Sound {
		t.Fatal("failed preference escaped into committed state")
	}
}

func TestReselectingDefaultAppUpdatesItsLocationWithoutDuplicatingScope(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Data.Preferences.Apps = []store.App{{ID: "work.a", Name: "A", Path: "/old/A.app"}}
	a := &application{store: s, controller: session.New(time.Now)}
	if err = a.controller.Start(session.State{ID: "scope", Goal: "目标", Minutes: 30, Scene: "forest", Allowed: []string{"work.a"}, Partial: true}); err != nil {
		t.Fatal(err)
	}
	a.controller.Allow("work.b")
	a.acceptWorkApp(store.App{ID: "work.a", Name: "A", Path: "/new/A.app"}, nil)
	if len(s.Data.Preferences.Apps) != 1 || s.Data.Preferences.Apps[0].Path != "/new/A.app" {
		t.Fatal("reselection duplicated or retained stale location", s.Data.Preferences.Apps)
	}
	state := a.controller.Snapshot()
	if len(state.Allowed) != 2 || len(state.TemporaryAllowed) != 1 || state.TemporaryAllowed[0] != "work.b" {
		t.Fatal("default relocation changed current scope", state)
	}
	reopened, err := store.Open(filepath.Dir(s.Path))
	if err != nil || reopened.Data.Preferences.Apps[0].Path != "/new/A.app" {
		t.Fatal("updated location was not persisted", err)
	}
}

func TestHistoryDeleteAndUndoKeepTheirStacksOnRealWriteFailure(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record := session.State{ID: "history", Goal: "保留记录", Started: time.Now(), Phase: "ended"}
	if err = s.Finish(record); err != nil {
		t.Fatal(err)
	}
	a := &application{store: s}
	path := s.Path
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err = os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Path = filepath.Join(blocked, "data.json")
	a.deleteHistoryRecord(record.ID)
	if len(s.Data.History) != 1 || len(a.undo) != 0 {
		t.Fatal("failed deletion changed history or undo")
	}
	s.Path = path
	a.deleteHistoryRecord(record.ID)
	if len(s.Data.History) != 0 || len(a.undo) != 1 {
		t.Fatal("deletion did not become undoable")
	}
	s.Path = filepath.Join(blocked, "data.json")
	a.undoHistoryDeletion()
	if len(s.Data.History) != 0 || len(a.undo) != 1 {
		t.Fatal("failed undo lost its retry")
	}
	s.Path = path
	a.undoHistoryDeletion()
	a.undoHistoryDeletion()
	if len(s.Data.History) != 1 || s.Data.History[0].ID != record.ID || len(a.undo) != 0 {
		t.Fatal("undo duplicated or lost record")
	}
}

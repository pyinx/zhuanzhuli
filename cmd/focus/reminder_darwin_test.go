//go:build darwin

package main

import (
	"errors"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDueReminderIndependentFromBackgroundAudio(t *testing.T) {
	original := playDueReminder
	defer func() { playDueReminder = original }()
	for _, enabled := range []bool{false, true} {
		for _, music := range []bool{false, true} {
			calls := 0
			playDueReminder = func() error { calls++; return errors.New("output refused playback") }
			c := session.New(time.Now)
			cfg := session.Config{Minutes: 1, Scene: "forest", Sound: music, Volume: 0, ReminderSound: enabled, Ambience: &session.AmbienceConfig{Mode: "off", Volume: 0}, Apps: []session.App{{ID: "owned", Path: "/Applications/Owned.app"}}}
			if err := c.Start(session.State{ID: "reminder-output", Goal: "提醒独立", Config: &cfg, Partial: true}); err != nil {
				t.Fatal(err)
			}
			a := &application{controller: c}
			a.gentleReminder()
			want := 0
			if enabled {
				want = 1
			}
			if calls != want || a.reminderUntil.IsZero() {
				t.Fatal("wrong reminder routing", enabled, music, calls)
			}
			if enabled && a.notice.Kind != noticeMedia {
				t.Fatal("playback error hidden", a.notice)
			}
			if !enabled && a.notice.Text != "" {
				t.Fatal("disabled sound created output error", a.notice)
			}
		}
	}
}

func TestDueReminderReportsStorageAndPlaybackFailure(t *testing.T) {
	original := playDueReminder
	defer func() { playDueReminder = original }()
	calls := 0
	playDueReminder = func() error { calls++; return errors.New("playback refused") }
	now := time.Now()
	c := session.New(func() time.Time { return now })
	cfg := session.Config{Minutes: 1, Scene: "forest", Apps: []session.App{{ID: "owned", Path: "/Applications/Owned.app"}}}
	cfg.ReminderSound = true
	if err := c.Start(session.State{ID: "due", Goal: "到点记录失败", Config: &cfg, Partial: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Enter(); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(); err != nil {
		t.Fatal(err)
	}
	s.Path = filepath.Join(s.Path, "blocked.json")
	a := &application{controller: c, store: s, busy: true}
	now = now.Add(time.Minute)
	a.tick()
	if a.reminderUntil.IsZero() {
		t.Fatal("checkpoint failure swallowed the due reminder before it reached presentation")
	}
	if a.notice.Kind != noticeStorage || c.Snapshot().Phase != "paused" {
		t.Fatal("lost the real storage failure", a.notice, c.Snapshot())
	}
	if calls != 1 || !strings.Contains(a.notice.Text, "提示音未播放") || !strings.Contains(a.notice.Text, "无法保存") {
		t.Fatal("combined failures were not both visible", calls, a.notice)
	}
	until := a.reminderUntil
	a.tick()
	if a.reminderUntil != until {
		t.Fatal("repeat tick reissued reminder")
	}
	if calls != 1 {
		t.Fatal("failure retry replayed the reminder")
	}
}

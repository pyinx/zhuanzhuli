package store

import (
	"encoding/json"
	"fmt"
	"focus.local/zhuanzhuli/internal/session"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNatureScenePreferencesTemplatesCheckpointAndHistoryRoundTrip(t *testing.T) {
	for _, id := range []string{"mountain", "snow-mountain", "meadow", "forest", "pond", "garden", "beach", "snow"} {
		t.Run(id, func(t *testing.T) {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg := s.Data.Preferences.Config()
			cfg.Scene = id
			cfg.Apps = []session.App{{ID: "editor", Name: "Editor", Path: "/Applications/Editor.app"}}
			p := s.Data.Preferences
			p.SetConfig(cfg)
			if err := s.SetPreferences(p); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveTemplate(Template{Name: "自然环境", Config: cfg}); err != nil {
				t.Fatal(err)
			}
			state := session.State{ID: "scene-round-trip", Goal: "自然专注", Config: &cfg, Started: time.Now(), Phase: "paused"}
			if err := s.Checkpoint(state); err != nil {
				t.Fatal(err)
			}
			s.Data.History = []session.State{state}
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			loaded, err := Open(filepath.Dir(s.Path))
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Data.Preferences.Scene != id || loaded.Data.Templates[0].Config.Scene != id || loaded.Data.Current.SceneValue() != id || loaded.Data.History[0].SceneValue() != id {
				t.Fatal("scene changed across persistence", loaded.Data)
			}
		})
	}
}

func TestNeverEnteredWorkDoesNotBecomeHistory(t *testing.T) {
	for _, test := range []struct {
		flag    string
		elapsed time.Duration
		want    int
	}{{"false", 0, 0}, {"true", 3 * time.Second, 1}, {"null", 0, 1}} {
		s, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		var v session.State
		body := `{"id":"one","goal":"真实目标","minutes":15,"scene":"forest","started":"2026-11-02T00:00:00Z","phase":"ended","allowed":["editor"],"workStarted":` + test.flag + `}`
		if err = json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatal(err)
		}
		v.Elapsed = test.elapsed
		s.Data.Current = &v
		if err = s.Finish(v); err != nil {
			t.Fatal(err)
		}
		if s.Data.Current != nil || len(s.Data.History) != test.want {
			t.Fatalf("flag=%s: failed preparation polluted work history: %+v", test.flag, s.Data)
		}
		r, err := Open(filepath.Dir(s.Path))
		if err != nil || len(r.Data.History) != test.want || r.Data.Current != nil {
			t.Fatal("history outcome not durable", err)
		}
	}
}

func TestInvalidCheckpointCannotCreateARunningSession(t *testing.T) {
	dir := t.TempDir()
	d := Defaults()
	d.Current = &session.State{ID: "one", Goal: "保留目标", Minutes: 0, Phase: "active"}
	b, _ := json.Marshal(d)
	path := filepath.Join(dir, "data.json")
	os.WriteFile(path, b, 0600)
	if _, e := Open(dir); e == nil {
		t.Fatal("invalid checkpoint accepted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(b) {
		t.Fatal("checkpoint was overwritten")
	}
}

func TestAppSourceCheckpointCompatibilityAndValidation(t *testing.T) {
	for _, source := range [][]string{nil, {}, {"chat"}, {"not-allowed"}} {
		dir := t.TempDir()
		d := Defaults()
		d.Current = &session.State{ID: "one", Goal: "保留目标", Minutes: 30, Scene: "forest", Started: time.Now(), Phase: "paused", Allowed: []string{"editor", "chat"}, TemporaryAllowed: source}
		encoded, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "data.json")
		if err = os.WriteFile(path, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := Open(dir)
		if len(source) > 0 && source[0] == "not-allowed" {
			if err == nil {
				t.Fatal("unknown source broadened scope")
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(encoded) {
				t.Fatal("invalid source overwritten")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if (loaded.Data.Current.TemporaryAllowed == nil) != (source == nil) {
			t.Fatal("lost legacy/new checkpoint distinction")
		}
		// An old reader ignores the new field and retains the same full scope.
		var old struct {
			Current struct {
				Allowed []string `json:"allowed"`
			} `json:"current"`
		}
		if err = json.Unmarshal(encoded, &old); err != nil || len(old.Current.Allowed) != 2 {
			t.Fatal("old reader incompatible", err)
		}
	}
}

func TestPersistenceDeleteUndoAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"one", "two"} {
		if e = s.Finish(session.State{ID: id, Goal: id, Minutes: 30, Started: time.Now(), Phase: "ended"}); e != nil {
			t.Fatal(e)
		}
	}
	v, i, e := s.Delete("two")
	if e != nil {
		t.Fatal(e)
	}
	restarted, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(restarted.Data.History) != 1 || restarted.Data.History[0].ID != "one" {
		t.Fatal("deletion not persisted")
	}
	if e = s.Undo(v, i); e != nil {
		t.Fatal(e)
	}
	if e = s.Undo(v, i); e != nil {
		t.Fatal(e)
	}
	if len(s.Data.History) != 2 {
		t.Fatal("duplicate undo")
	}
	info, _ := os.Stat(s.Path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}
func TestCorruptOrNewerDataIsNotOverwritten(t *testing.T) {
	for _, body := range []string{"bad json", `{"version":999}`} {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.json")
		os.WriteFile(path, []byte(body), 0600)
		if _, e := Open(dir); e == nil {
			t.Fatal("invalid accepted")
		}
		b, _ := os.ReadFile(path)
		if string(b) != body {
			t.Fatal("overwritten")
		}
	}
}
func TestFailedDeletionKeepsMemoryAndRecord(t *testing.T) {
	s, _ := Open(t.TempDir())
	s.Data.History = []session.State{{ID: "one", Goal: "保留"}}
	s.Path = filepath.Join(s.Path, "missing", "data.json")
	if _, _, e := s.Delete("one"); e == nil {
		t.Fatal("expected write error")
	}
	if len(s.Data.History) != 1 {
		t.Fatal("failed delete changed memory")
	}
}
func TestUndoAfterANewSessionPreservesDateOrder(t *testing.T) {
	s, _ := Open(t.TempDir())
	base := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b", "c"} {
		if e := s.Finish(session.State{ID: id, Started: base.Add(time.Duration(i) * time.Hour)}); e != nil {
			t.Fatal(e)
		}
	}
	v, index, e := s.Delete("b")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(session.State{ID: "d", Started: base.Add(3 * time.Hour)}); e != nil {
		t.Fatal(e)
	}
	if e = s.Undo(v, index); e != nil {
		t.Fatal(e)
	}
	for i, id := range []string{"d", "c", "b", "a"} {
		if s.Data.History[i].ID != id {
			t.Fatal("undo reordered sessions", s.Data.History)
		}
	}
}

func TestFailedFinishKeepsCheckpointAndHistory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Data.Current = &session.State{ID: "current", Goal: "保留检查点", Phase: "paused"}
	s.Data.History = []session.State{{ID: "older", Goal: "已有记录"}}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	path := s.Path
	before, _ := os.ReadFile(path)
	s.Path = filepath.Join(path, "blocked.json") // Existing regular file cannot be a directory.
	if s.Finish(session.State{ID: "current", Phase: "ended"}) == nil {
		t.Fatal("expected write failure")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || s.Data.Current == nil || s.Data.Current.ID != "current" || len(s.Data.History) != 1 || s.Data.History[0].ID != "older" {
		t.Fatal("failed finish discarded the checkpoint or modified history")
	}
}

func TestFailedUndoKeepsHistoryAndDisk(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Data.History = []session.State{{ID: "retained", Goal: "已有记录"}}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	path := s.Path
	before, _ := os.ReadFile(path)
	s.Path = filepath.Join(path, "blocked.json")
	if s.Undo(session.State{ID: "deleted", Goal: "待恢复记录"}, 0) == nil {
		t.Fatal("expected write failure")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || len(s.Data.History) != 1 || s.Data.History[0].ID != "retained" {
		t.Fatal("failed undo modified history or disk")
	}
}

func TestFailedPreferencesKeepCommittedValues(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	path := s.Path
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	next := s.Data.Preferences
	next.Entry = false
	next.Apps = []App{{ID: "editor", Name: "合成编辑器", Path: "/Applications/SyntheticEditor.app"}}
	s.Path = filepath.Join(path, "blocked.json")
	if s.SetPreferences(next) == nil {
		t.Fatal("expected write failure")
	}
	if !s.Data.Preferences.Entry || len(s.Data.Preferences.Apps) != 0 {
		t.Fatal("failed preference write changed effective settings")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed preference write modified committed data", err)
	}
	s.Path = path
	if err := s.SetPreferences(next); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(filepath.Dir(path))
	if err != nil || reopened.Data.Preferences.Entry || len(reopened.Data.Preferences.Apps) != 1 {
		t.Fatal("retry did not commit preferences", err)
	}
}

func TestLegacyAudioUpgradePreservesSilenceAndSnapshots(t *testing.T) {
	for _, version := range []int{1, 2} {
		for _, volume := range []float64{0, .3} {
			for _, sound := range []bool{false, true} {
				dir := t.TempDir()
				d := Defaults()
				d.Version = version
				d.Preferences.Ambience = nil
				d.Preferences.Volume = volume
				d.Preferences.Sound = sound
				cfg := d.Preferences.Config()
				d.Templates = []Template{{ID: "old", Name: "旧模版", Config: cfg}}
				d.History = []session.State{{ID: "past", Goal: "旧目标", Config: &cfg, Started: time.Now(), Phase: "ended"}}
				body, _ := json.Marshal(d)
				path := filepath.Join(dir, "data.json")
				os.WriteFile(path, body, 0600)
				s, err := Open(dir)
				if err != nil {
					t.Fatal(version, volume, sound, err)
				}
				if s.Data.Version != 3 || s.Data.Preferences.Volume != volume || s.Data.Preferences.Sound != sound {
					t.Fatal("lost legacy settings")
				}
				a := s.Data.Preferences.Ambience
				if a == nil || (!sound && a.Mode != "off") || (volume == 0 && a.Volume != 0) {
					t.Fatal("legacy mute changed", a)
				}
				if s.Data.History[0].Config.Ambience != nil {
					t.Fatal("fabricated independent audio history")
				}
				backups, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("data.v%d.*.json", version)))
				if len(backups) != 1 {
					t.Fatal("missing exact backup")
				}
				raw, _ := os.ReadFile(backups[0])
				if string(raw) != string(body) {
					t.Fatal("backup changed")
				}
				if _, err = Open(dir); err != nil {
					t.Fatal("upgrade not idempotent", err)
				}
			}
		}
	}
}

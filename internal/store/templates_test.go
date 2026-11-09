package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTemplateSnapshotCRUDAndFailedSave(t *testing.T) {
	s, _ := Open(t.TempDir())
	cfg := s.Data.Preferences.Config()
	cfg.Apps = []App{{ID: "editor", Name: "编辑器", Path: "/Applications/Editor.app"}}
	v, err := s.SaveTemplate(Template{Name: " 写作 ", Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Apps[0].Name = "外部修改"
	if s.Data.Templates[0].Config.Apps[0].Name != "编辑器" {
		t.Fatal("shared template slice")
	}
	if _, err = s.SaveTemplate(Template{Name: "写作", Config: cfg}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	for _, name := range []string{"", "  ", "a\x00b"} {
		if _, err = s.SaveTemplate(Template{Name: name, Config: cfg}); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	path := s.Path
	s.Path = filepath.Join(path, "blocked")
	v.Name = "新名"
	if _, err = s.SaveTemplate(v); err == nil {
		t.Fatal("expected write error")
	}
	if s.Data.Templates[0].Name != "写作" {
		t.Fatal("failed save changed memory")
	}
	s.Path = path
	if _, err = s.SaveTemplate(v); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Dir(path))
	if err != nil || r.Data.Templates[0].Name != "新名" {
		t.Fatal("not durable", err)
	}
	if err = r.DeleteTemplate(v.ID); err != nil {
		t.Fatal(err)
	}
	if len(r.Data.Templates) != 0 {
		t.Fatal("delete failed")
	}
}
func TestV1MigrationPreservesOriginalAndUnknownHistory(t *testing.T) {
	dir := t.TempDir()
	d := Defaults()
	d.Version = 1
	d.Preferences.Appearance = ""
	b, _ := json.Marshal(d)
	// Real old files have no appearance field.
	var raw map[string]any
	json.Unmarshal(b, &raw)
	delete(raw["preferences"].(map[string]any), "appearance")
	b, _ = json.Marshal(raw)
	path := filepath.Join(dir, "data.json")
	os.WriteFile(path, b, 0600)
	s, err := Open(dir)
	if err != nil || s.Data.Version != 3 || s.Data.Preferences.Appearance != "light" {
		t.Fatal(s, err)
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "data.v1.*.json"))
	if len(backups) != 1 {
		t.Fatal("missing backup")
	}
	old, _ := os.ReadFile(backups[0])
	if string(old) != string(b) {
		t.Fatal("backup changed bytes")
	}
	if _, err = Open(dir); err != nil {
		t.Fatal(err)
	}
	again, _ := filepath.Glob(filepath.Join(dir, "data.v1.*.json"))
	if len(again) != 1 {
		t.Fatal("repeated migration")
	}
}
func TestQuickTemplateReferenceIsDurableAndClearedAtomically(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SaveTemplate(Template{Name: "快速工作", Config: s.Data.Preferences.Config()})
	if err != nil {
		t.Fatal(err)
	}
	p := s.Data.Preferences
	p.QuickTemplate = v.ID
	if err = s.SetPreferences(p); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil || s.Data.Preferences.QuickTemplate != v.ID {
		t.Fatalf("shortcut not restored: %v", err)
	}
	if err = s.DeleteTemplate(v.ID); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil || s.Data.Preferences.QuickTemplate != "" || len(s.Data.Templates) != 0 {
		t.Fatalf("shortcut reference survived template deletion: %v", err)
	}
}
func TestSavedTemplateReturnCannotEditCommittedAppPaths(t *testing.T) {
	s, _ := Open(t.TempDir())
	cfg := s.Data.Preferences.Config()
	cfg.Apps = []App{{ID: "editor", Name: "Editor", Path: "/Applications/Editor.app"}}
	saved, err := s.SaveTemplate(Template{Name: "写作", Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	saved.Config.Apps[0].Path = "/unconfirmed/Editor.app"
	if s.Data.Templates[0].Config.Apps[0].Path != "/Applications/Editor.app" {
		t.Fatal("editing returned draft mutated committed template")
	}
	p := s.Data.Preferences
	p.Appearance = "dark"
	if err = s.SetPreferences(p); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Dir(s.Path))
	if err != nil || r.Data.Templates[0].Config.Apps[0].Path != "/Applications/Editor.app" {
		t.Fatal("unconfirmed app path leaked to disk", err)
	}
}
func TestImmutableBackupFailureCanRetryWithoutReplacingForeignContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.json")
	os.Mkdir(path, 0700)
	if immutableBackup(path, []byte("original")) == nil {
		t.Fatal("expected backup failure")
	}
	os.Remove(path)
	if err := immutableBackup(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if immutableBackup(path, []byte("different")) == nil {
		t.Fatal("overwrote a different immutable backup")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "original" {
		t.Fatal("original backup changed")
	}
}

func TestAmbienceTemplateAndSessionSnapshotsRemainIndependent(t *testing.T) {
	s, _ := Open(t.TempDir())
	c := s.Data.Preferences.Config()
	c.Ambience.Mode = "rain"
	c.Ambience.Volume = .2
	saved, err := s.SaveTemplate(Template{Name: "声音独立", Config: c})
	if err != nil {
		t.Fatal(err)
	}
	saved.Config.Ambience.Volume = .8
	c.Ambience.Mode = "waves"
	if s.Data.Templates[0].Config.Ambience.Volume != .2 || s.Data.Templates[0].Config.Ambience.Mode != "rain" {
		t.Fatal("draft changed committed ambience")
	}
	p := s.Data.Preferences.Config()
	p.Ambience.Volume = .9
	if s.Data.Preferences.Ambience.Volume == .9 {
		t.Fatal("preferences alias escaped")
	}
}

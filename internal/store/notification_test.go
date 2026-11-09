package store

import (
	"os"
	"testing"
)

func TestNotificationConfigurationSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Data.Preferences.FocusLinked {
		t.Fatal("linkage enabled without configuration")
	}
	s.Data.Preferences.FocusLinked = true
	s.Data.Preferences.FocusProfile = "专注力"
	if err = s.Save(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil || !s.Data.Preferences.FocusLinked || s.Data.Preferences.FocusProfile != "专注力" {
		t.Fatal("configuration lost", err)
	}
}
func TestInvalidNotificationConfigurationIsPreservedAndRejected(t *testing.T) {
	for _, name := range []string{"", "bad\x00name"} {
		dir := t.TempDir()
		s, _ := Open(dir)
		s.Data.Preferences.FocusLinked = true
		s.Data.Preferences.FocusProfile = name
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(s.Path)
		if _, err := Open(dir); err == nil {
			t.Fatal("invalid enabled configuration accepted")
		}
		after, _ := os.ReadFile(s.Path)
		if string(before) != string(after) {
			t.Fatal("invalid data overwritten")
		}
	}
}

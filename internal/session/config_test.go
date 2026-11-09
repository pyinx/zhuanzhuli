package session

import (
	"testing"
	"time"
)

func TestNatureAndLegacySceneConfiguration(t *testing.T) {
	for _, id := range []string{"mountain", "snow-mountain", "meadow", "forest", "pond", "garden", "beach", "snow"} {
		t.Run(id, func(t *testing.T) {
			c := Config{Minutes: 30, Scene: id, Volume: .3}
			if err := c.Validate(false); err != nil {
				t.Fatalf("known scene rejected: %v", err)
			}
		})
	}
	if err := (Config{Minutes: 30, Scene: "unknown", Volume: .3}).Validate(false); err == nil {
		t.Fatal("unknown scene accepted")
	}
}

func TestConfigSessionRemainsIndependentAcrossEditsAndResume(t *testing.T) {
	now := time.Now()
	c := New(func() time.Time { return now })
	cfg := Config{Minutes: 15, Scene: "forest", Volume: .3, Apps: []App{{ID: "editor", Name: "Editor", Path: "/Applications/Editor.app"}}}
	if err := c.Start(State{ID: "one", Goal: "写作", Config: &cfg, TemplateName: "写作", Partial: true}); err != nil {
		t.Fatal(err)
	}
	cfg.Volume = .1
	cfg.Apps[0].ID = "wrong"
	s := c.Snapshot()
	s.Config.Apps[0].ID = "another"
	if c.Snapshot().Config.Volume != .3 || !c.Allowed("editor") {
		t.Fatal("session follows external config")
	}
	c.Enter()
	c.AllowApp(App{ID: "temporary", Name: "临时", Path: "/Applications/Temporary.app"})
	c.Pause()
	if err := c.ResumeWithApps(true, []string{"unrelated"}); err != nil {
		t.Fatal(err)
	}
	if c.Allowed("unrelated") || !c.Allowed("temporary") {
		t.Fatal("resume used current defaults")
	}
	now = now.Add(16 * time.Minute)
	if c.Snapshot().Phase != "overtime" {
		t.Fatal("config duration ignored")
	}
}
func TestCompletingLegacyRetainsKnownTemporaryApplicationSources(t *testing.T) {
	c := New(time.Now)
	c.Recover(State{ID: "old", Goal: "旧会话", Minutes: 30, Scene: "forest", Allowed: []string{"editor", "chat"}, TemporaryAllowed: []string{"chat"}, Phase: "active"})
	cfg := Config{Minutes: 30, Scene: "forest", Apps: []App{{ID: "editor", Path: "/Applications/Editor.app"}, {ID: "chat", Path: "/Applications/Chat.app"}}}
	if err := c.CompleteLegacy(cfg); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if len(s.Config.Apps) != 1 || s.Config.Apps[0].ID != "editor" || len(s.TemporaryAllowed) != 1 || s.TemporaryAllowed[0] != "chat" || len(s.TemporaryApps) != 1 || s.LegacyAppScope {
		t.Fatal("lost known application provenance", s)
	}
}

func TestAmbienceSelectionAndIndependentVolumes(t *testing.T) {
	for _, mode := range []string{"auto", "off", "rain", "birds", "waves", "stream"} {
		c := Config{Minutes: 30, Scene: "forest", Sound: false, Volume: 0, Ambience: &AmbienceConfig{Mode: mode, Volume: .2}}
		if err := c.Validate(false); err != nil {
			t.Fatal(mode, err)
		}
		copy := c.Clone()
		copy.Ambience.Volume = .8
		if c.Ambience.Volume != .2 {
			t.Fatal("ambience alias escaped config copy")
		}
	}
	for _, tc := range []AmbienceConfig{{Mode: "fake", Volume: .2}, {Mode: "auto", Volume: -.1}, {Mode: "rain", Volume: 1.1}} {
		c := Config{Minutes: 30, Scene: "forest", Ambience: &tc}
		if c.Validate(false) == nil {
			t.Fatal("bad ambience accepted", tc)
		}
	}
	c := Config{Minutes: 30, Scene: "forest", Sound: true, Volume: 0}
	if c.IndependentAudio().Ambience.Volume != 0 || c.Ambience != nil {
		t.Fatal("upgrade opened zero volume or mutated source")
	}
	c.Sound = false
	c.Volume = .8
	if c.IndependentAudio().Ambience.Mode != "off" {
		t.Fatal("legacy silence opened ambience")
	}
}

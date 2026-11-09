//go:build darwin

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"focus.local/zhuanzhuli/internal/media"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"github.com/ebitengine/purego/objc"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var audioTestInit sync.Once
var audioTestInitError error

func audioApplication(t *testing.T) *application {
	t.Helper()
	audioTestInit.Do(func() { audioTestInitError = m.Init() })
	if audioTestInitError != nil {
		t.Fatal(audioTestInitError)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "media"), 0700); err != nil {
		t.Fatal(err)
	}
	// A declared silent PCM decoder fixture, never a product nature resource.
	pcm := make([]byte, 44+44100*2*2)
	copy(pcm, "RIFF")
	binary.LittleEndian.PutUint32(pcm[4:], uint32(len(pcm)-8))
	copy(pcm[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(pcm[16:], 16)
	binary.LittleEndian.PutUint16(pcm[20:], 1)
	binary.LittleEndian.PutUint16(pcm[22:], 2)
	binary.LittleEndian.PutUint32(pcm[24:], 44100)
	binary.LittleEndian.PutUint32(pcm[28:], 176400)
	binary.LittleEndian.PutUint16(pcm[32:], 4)
	binary.LittleEndian.PutUint16(pcm[34:], 16)
	copy(pcm[36:], "data")
	binary.LittleEndian.PutUint32(pcm[40:], uint32(len(pcm)-44))
	sum := sha256.Sum256(pcm)
	file := func(name string) media.File {
		path := "media/" + name + ".wav"
		if err := os.WriteFile(filepath.Join(root, path), pcm, 0600); err != nil {
			t.Fatal(err)
		}
		return media.File{Path: path, SHA256: hex.EncodeToString(sum[:])}
	}
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &application{resources: root, store: s, controller: session.New(time.Now), catalog: media.Catalog{Version: 2, Scenes: []media.Scene{{ID: "forest", Name: "森林", Work: file("music")}}}}
	for _, id := range []string{"rain", "birds", "waves", "stream"} {
		a.catalog.Ambiences = append(a.catalog.Ambiences, media.Ambience{ID: id, Name: media.AmbienceName(id), Audio: file(id)})
	}
	t.Cleanup(a.stopAudition)
	t.Cleanup(a.stopSceneAudio)
	return a
}

func TestIndependentMusicAndAmbiencePlayback(t *testing.T) {
	a := audioApplication(t)
	for _, music := range []bool{false, true} {
		for _, mode := range []string{"off", "auto", "rain", "birds", "waves", "stream"} {
			for _, v := range []float64{0, .000001} {
				c := session.Config{Scene: "forest", Sound: music, Volume: v, Ambience: &session.AmbienceConfig{Mode: mode, Volume: v}}
				a.auditionConfig(c, 0)
				if (a.audition.Player != 0) != (music && v > 0) || (a.auditionNature.Player != 0) != (mode != "off" && v > 0) {
					t.Fatalf("wrong players for %t %s %g", music, mode, v)
				}
				if a.auditionNature.Player != 0 {
					id := mode
					if id == "auto" {
						id = "birds"
					}
					if got := m.Str(m.Send(m.Send(a.auditionNature.Player, "url"), "path")); got != filepath.Join(a.resources, "media", id+".wav") {
						t.Fatal("wrong recording", got)
					}
				}
				a.stopAudition()
			}
		}
	}
}

func TestTwoTrackLifecycleAndAuditionCleanup(t *testing.T) {
	a := audioApplication(t)
	c := session.Config{Minutes: 1, Entry: true, Scene: "forest", Sound: true, Volume: .000001, Ambience: &session.AmbienceConfig{Mode: "waves", Volume: .000001}, Apps: []session.App{{ID: "owned", Path: "/Applications/Owned.app"}}}
	a.auditionConfig(c, 0)
	music, nature := a.audition.Player, a.auditionNature.Player
	if music == 0 || nature == 0 || a.auditionTimer == nil {
		t.Fatal("real audition did not start")
	}
	a.stopAudition()
	if a.audition.Player != 0 || a.auditionNature.Player != 0 || a.auditionTimer != nil {
		t.Fatal("audition retained player/timer")
	}
	if err := a.controller.Start(session.State{ID: "two-track", Goal: "双轨连续性", Config: &c, Partial: true}); err != nil {
		t.Fatal(err)
	}
	// Exercise resource repair within an existing entry owner, which must not
	// leave newly created players at zero or schedule a second entry fade.
	a.audioSession, a.audioScene = "two-track", "forest"
	a.environmentSound()
	music, nature = a.audio.Player, a.nature.Player
	if music == 0 || nature == 0 || objc.Send[float32](nature, m.S("volume")) == 0 {
		t.Fatal("repaired track stayed silent")
	}
	if err := a.controller.Enter(); err != nil {
		t.Fatal(err)
	}
	a.environmentSound()
	if a.audio.Player != music || a.nature.Player != nature {
		t.Fatal("entry to work restarted a track")
	}
	a.controller.Pause()
	a.environmentSound()
	if a.audio.Player != 0 || a.nature.Player != 0 || a.audioSession != "" {
		t.Fatal("pause retained a track")
	}
}

func TestPreviewAsynchronousFailureRemainsVisible(t *testing.T) {
	a := audioApplication(t)
	// A real AVPlayer with no current item reports the same asynchronous-state
	// error boundary as a failed AVPlayerItem, without opening any window.
	a.previewVideo.Player = m.Send(m.Class("AVPlayer"), "new")
	a.previewSceneID = "forest"
	a.refreshAudition()
	if a.previewVideo.Player != 0 || a.auditionError == "" || a.mediaFailureCaptionFor("forest") == "" {
		t.Fatal("preview failure silently dismissed", a.auditionError, a.mediaFailures)
	}
}

func TestAuditionControlsStayWithTheirForms(t *testing.T) {
	a := audioApplication(t)
	focus := m.Button(0, "试听当前组合 5 秒", m.R(0, 0, 200, 30), nil)
	template := m.Button(0, "试听当前组合 5 秒", m.R(0, 0, 200, 30), nil)
	notice := m.Label(0, "", m.R(0, 0, 200, 30), 12)
	defer m.Release(focus)
	defer m.Release(template)
	defer m.Release(notice)
	a.focusForm = &configForm{auditionButton: focus, notice: notice, apps: &m.AppList{}}
	a.templateForm = &configForm{auditionButton: template, apps: &m.AppList{}}
	defer func() { a.focusForm, a.templateForm = nil, nil }()
	c := session.Config{Scene: "forest", Sound: true, Volume: .000001, Ambience: &session.AmbienceConfig{Mode: "off"}}
	a.auditionConfig(c, notice)
	a.refreshAudition()
	if m.Str(m.Send(focus, "title")) != "停止试听" || m.Str(m.Send(template, "title")) != "试听当前组合 5 秒" {
		t.Fatal("audition status went to another form")
	}
	a.stopAudition()
	if m.Str(m.Send(focus, "title")) != "试听当前组合 5 秒" {
		t.Fatal("stopped form retained playing status")
	}
}

func TestExplicitSceneResolutionAndFailuresRemainIndependent(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "media"), 0700); err != nil {
		t.Fatal(err)
	}
	role := func(id string) media.File {
		body := []byte("distinct bytes for " + id)
		sum := sha256.Sum256(body)
		file := media.File{Path: "media/" + id, SHA256: hex.EncodeToString(sum[:])}
		if err := os.WriteFile(filepath.Join(root, file.Path), body, 0600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	forest, pond := role("forest"), role("pond")
	a := &application{resources: root, catalog: media.Catalog{Scenes: []media.Scene{
		{ID: "forest", Name: "森林", Background: forest, Work: forest},
		{ID: "pond", Name: "池塘", Background: pond, Work: pond},
	}}}
	if got := a.mediaFor("pond", ".wav"); got != filepath.Join(root, pond.Path) {
		t.Fatal(got)
	}
	if got := a.mediaFor("forest", ".png"); got != filepath.Join(root, forest.Path) {
		t.Fatal(got)
	}
	if err := os.Remove(filepath.Join(root, pond.Path)); err != nil {
		t.Fatal(err)
	}
	if a.mediaFor("pond", ".wav") != "" || a.mediaFailureCaptionFor("pond") == "" {
		t.Fatal("missing pond not reported")
	}
	if a.mediaFor("forest", ".wav") == "" || a.mediaFailureCaptionFor("forest") != "" {
		t.Fatal("pond failure poisoned forest")
	}
	if a.mediaFor("snow", ".wav") != "" {
		t.Fatal("legacy snow silently substituted")
	}
	role("pond")
	if a.mediaFor("pond", ".wav") == "" || a.mediaFailureCaptionFor("pond") != "" {
		t.Fatal("repaired pond cannot retry")
	}
}

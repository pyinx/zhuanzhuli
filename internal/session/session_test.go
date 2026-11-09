package session

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOrdinaryFallbackPreservesPausedSessionAndRequiresDurableSave(t *testing.T) {
	now := time.Unix(100, 0)
	c := New(func() time.Time { return now })
	if err := c.Start(State{ID: "immersive", Goal: "继续工作", Minutes: 30, Scene: "forest", Allowed: []string{"editor"}, Partial: true, Immersion: true}); err != nil {
		t.Fatal(err)
	}
	c.Enter()
	now = now.Add(42 * time.Second)
	c.Pause()
	before := c.Snapshot()
	if before.Elapsed != 42*time.Second {
		t.Fatalf("test did not establish a nonzero paused timer: %v", before.Elapsed)
	}
	want := before
	want.Immersion = false
	writeFailed := errors.New("磁盘不可写")
	if err := c.UseOrdinaryFocus(func(State) error { return writeFailed }); !errors.Is(err, writeFailed) || !reflect.DeepEqual(c.Snapshot(), before) {
		t.Fatal("a failed checkpoint must preserve the paused immersive session", err)
	}
	var saved State
	if err := c.UseOrdinaryFocus(func(s State) error { saved = s; return nil }); err != nil || !reflect.DeepEqual(saved, want) || !reflect.DeepEqual(c.Snapshot(), want) {
		t.Fatal("the fallback must only change immersion, without resuming the clock", err)
	}
}

func TestTemporaryAllowanceCheckpointKeepsItsSource(t *testing.T) {
	c, _ := setup(t)
	c.Allow("chat")
	encoded, err := json.Marshal(c.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		TemporaryAllowed []string `json:"temporaryAllowed"`
	}
	if err = json.Unmarshal(encoded, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.TemporaryAllowed) != 1 || saved.TemporaryAllowed[0] != "chat" {
		t.Fatalf("checkpoint cannot distinguish a temporary app from defaults: %s", encoded)
	}
}

func TestResumeUsesNewDefaultsAndRetainsExplicitSessionApps(t *testing.T) {
	c, _ := setup(t)
	c.Enter()
	c.Allow("chat")
	c.Pause()
	if err := c.ResumeWithApps(true, []string{"new-editor", "chat", "new-editor"}); err != nil {
		t.Fatal(err)
	}
	if c.Allowed("editor") || !c.Allowed("new-editor") || !c.Allowed("chat") || len(c.Snapshot().Allowed) != 2 {
		t.Fatal("resume did not apply defaults plus explicit additions", c.Snapshot())
	}
	// Temporarily making chat a default must not erase its original source.
	c.Pause()
	if err := c.ResumeWithApps(true, []string{"third-editor"}); err != nil {
		t.Fatal(err)
	}
	if !c.Allowed("chat") || c.Allowed("new-editor") {
		t.Fatal(c.Snapshot())
	}
	view := c.Snapshot()
	view.TemporaryAllowed[0] = "tampered"
	if c.Snapshot().TemporaryAllowed[0] != "chat" {
		t.Fatal("source slice escaped ownership")
	}
	c.Pause()
	before := c.Snapshot()
	if c.ResumeWithApps(false, []string{"not-consented"}) == nil || c.Snapshot().Phase != "paused" || c.Allowed("not-consented") || c.Snapshot().Elapsed != before.Elapsed {
		t.Fatal("denied resume changed scope or clock", c.Snapshot())
	}
}

func TestAppSourceSurvivesRestartAndLegacyScopeIsConservative(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		c, _ := setup(t)
		checkpoint := c.Snapshot()
		if legacy {
			checkpoint.TemporaryAllowed = nil
		}
		bytes, err := json.Marshal(checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		var loaded State
		if err = json.Unmarshal(bytes, &loaded); err != nil {
			t.Fatal(err)
		}
		recovered := New(time.Now)
		recovered.Recover(loaded)
		if err = recovered.ResumeWithApps(true, []string{"new-editor"}); err != nil {
			t.Fatal(err)
		}
		if !recovered.Allowed("new-editor") || recovered.Allowed("editor") != legacy {
			t.Fatalf("legacy=%v wrong resumed scope: %+v", legacy, recovered.Snapshot())
		}
		if legacy {
			if !recovered.Snapshot().LegacyAppScope {
				t.Fatal("legacy provenance marker lost on first resume")
			}
			checkpoint2, err := json.Marshal(recovered.Snapshot())
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(checkpoint2, &loaded); err != nil {
				t.Fatal(err)
			}
			again := New(time.Now)
			again.Recover(loaded)
			again.Allow("new-session-app")
			if err = again.ResumeWithApps(true, []string{"third-editor"}); err != nil {
				t.Fatal(err)
			}
			if !again.Allowed("editor") || !again.Allowed("new-session-app") || !again.Snapshot().LegacyAppScope {
				t.Fatal("second restart lost scope or legacy warning", again.Snapshot())
			}
			if again.Allowed("new-editor") {
				t.Fatal("a removed modern default accumulated into legacy scope", again.Snapshot())
			}
		}
	}
	c, _ := setup(t)
	c.Pause()
	if c.ResumeWithApps(true, nil) == nil || c.Snapshot().Phase != "paused" || !c.Allowed("editor") {
		t.Fatal("empty scope resumed or changed state")
	}
}

func TestGoalLimitCountsUnicodeCharacters(t *testing.T) {
	for _, goal := range []string{"事", strings.Repeat("推", 200), strings.Repeat("🌲", 200)} {
		c := New(time.Now)
		if err := c.Start(State{ID: "boundary", Goal: goal, Minutes: 1, Allowed: []string{"editor"}, Partial: true}); err != nil {
			t.Fatalf("valid Unicode goal rejected: %v", err)
		}
		if c.Snapshot().Goal != goal {
			t.Fatal("goal was truncated")
		}
	}
	for _, goal := range []string{strings.Repeat("推", 201), strings.Repeat("🌲", 201)} {
		c := New(time.Now)
		if c.Start(State{ID: "boundary", Goal: goal, Minutes: 1, Allowed: []string{"editor"}, Partial: true}) == nil {
			t.Fatal("over-limit goal accepted")
		}
	}
}

func setup(t *testing.T) (*Controller, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	c := New(func() time.Time { return now })
	e := c.Start(State{ID: "one", Goal: "完成方案", Minutes: 1, Allowed: []string{"editor"}, Scene: "forest", Partial: true})
	if e != nil {
		t.Fatal(e)
	}
	return c, &now
}

func TestWorkEntryProvenanceIsExplicitAndLegacyRemainsUnknown(t *testing.T) {
	read := func(s State) *bool {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			WorkStarted *bool `json:"workStarted"`
		}
		if err = json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		return v.WorkStarted
	}
	c, _ := setup(t)
	if v := read(c.Snapshot()); v == nil || *v {
		t.Fatal("new entry lacks explicit not-yet-working provenance")
	}
	if err := c.Enter(); err != nil {
		t.Fatal(err)
	}
	if v := read(c.Snapshot()); v == nil || !*v {
		t.Fatal("actual entry not recorded")
	}
	c.Pause()
	if v := read(c.Snapshot()); v == nil || !*v {
		t.Fatal("pause lost work provenance")
	}
	legacy := c.Snapshot()
	raw, _ := json.Marshal(legacy)
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	delete(fields, "workStarted")
	raw, _ = json.Marshal(fields)
	var old State
	json.Unmarshal(raw, &old)
	recovered := New(time.Now)
	recovered.Recover(old)
	if read(recovered.Snapshot()) != nil {
		t.Fatal("invented provenance for legacy checkpoint")
	}
	if err := recovered.Resume(true); err != nil {
		t.Fatal(err)
	}
	if v := read(recovered.Snapshot()); v == nil || !*v {
		t.Fatal("explicit resumed work not recorded")
	}
}
func TestClockPauseOvertimeAndOneReminder(t *testing.T) {
	c, n := setup(t)
	*n = n.Add(9 * time.Second)
	if c.Snapshot().Elapsed != 0 {
		t.Fatal("entry counted")
	}
	if e := c.Enter(); e != nil {
		t.Fatal(e)
	}
	*n = n.Add(40 * time.Second)
	c.Pause()
	*n = n.Add(8 * time.Hour)
	if c.Snapshot().Elapsed != 40*time.Second {
		t.Fatal("paused time counted")
	}
	if e := c.Resume(true); e != nil {
		t.Fatal(e)
	}
	*n = n.Add(21 * time.Second)
	if !c.Tick() {
		t.Fatal("reminder missing")
	}
	for i := 0; i < 10001; i++ {
		if c.Tick() {
			t.Fatal("duplicate reminder")
		}
	}
	if c.Snapshot().Phase != "overtime" || c.Snapshot().Elapsed != 61*time.Second {
		t.Fatal(c.Snapshot())
	}
	c.Pause()
	*n = n.Add(time.Hour)
	c.Resume(true)
	if c.Snapshot().Phase != "overtime" {
		t.Fatal("lost overtime")
	}
	if c.Tick() {
		t.Fatal("reminder repeated on resume")
	}
}
func TestTemporaryAllowanceSurvivesPauseResumeButNotNextSession(t *testing.T) {
	c, _ := setup(t)
	c.Enter()
	c.Allow("chat")
	c.Pause()
	c.Resume(true)
	if !c.Allowed("chat") {
		t.Fatal("temporary allowance lost on resume")
	}
	v := c.Snapshot()
	v.Allowed[0] = "tamper"
	if c.Allowed("tamper") {
		t.Fatal("snapshot mutated owner")
	}
	c.End()
	e := c.Start(State{ID: "two", Goal: "新目标", Minutes: 15, Allowed: []string{"editor"}, Partial: true})
	if e != nil {
		t.Fatal(e)
	}
	if c.Allowed("chat") {
		t.Fatal("temporary allowance leaked to default")
	}
}
func TestRecoveryNeverCreditsUnknownInterval(t *testing.T) {
	c, n := setup(t)
	c.Enter()
	*n = n.Add(20 * time.Second)
	checkpoint := c.Snapshot()
	*n = n.Add(48 * time.Hour)
	restored := New(func() time.Time { return *n })
	restored.Recover(checkpoint)
	if restored.Snapshot().Phase != "paused" || restored.Snapshot().Elapsed != 20*time.Second {
		t.Fatal(restored.Snapshot())
	}
}
func TestInvalidAndRepeatActions(t *testing.T) {
	c := New(time.Now)
	for _, s := range []State{{Goal: "", Minutes: 30, Allowed: []string{"x"}, Partial: true}, {Goal: "x", Minutes: 181, Allowed: []string{"x"}, Partial: true}, {Goal: "x", Minutes: 30, Partial: true}, {Goal: "x", Minutes: 30, Allowed: []string{"x"}}} {
		if c.Start(s) == nil {
			t.Fatal("invalid start accepted", s)
		}
	}
	c, _ = setup(t)
	if c.Start(State{}) == nil {
		t.Fatal("repeat start accepted")
	}
	c.Pause()
	if c.Resume(false) == nil {
		t.Fatal("implicit partial protection")
	}
	c.End()
	if c.Resume(true) == nil {
		t.Fatal("ended resumed")
	}
}

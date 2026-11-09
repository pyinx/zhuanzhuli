package session

import (
	"testing"
	"time"
)

func TestClockRefreshCannotExhaustCommands(t *testing.T) {
	c := New("run", time.Now)
	for i := 0; i < 10001; i++ {
		c.Tick()
	}
	if _, err := c.Handle(Command{RunID: "run", ID: "end", Revision: c.Snapshot().Revision, Kind: "end"}); err != nil {
		t.Fatal("clock prevented ending:", err)
	}
}

func TestEntryPauseOvertimeAndOneReminder(t *testing.T) {
	now := time.Unix(1000, 0)
	c := New("run", func() time.Time { return now })
	seq := 0
	send := func(kind string) {
		t.Helper()
		if kind == "tick" {
			c.Tick()
			return
		}
		seq++
		if _, err := c.Handle(Command{RunID: "run", ID: kind + time.Duration(seq).String(), Revision: c.Snapshot().Revision, Kind: kind, Goal: "测试", Minutes: 1}); err != nil {
			t.Fatal(err)
		}
	}
	send("start")
	now = now.Add(20 * time.Second)
	if c.Snapshot().Elapsed != 0 {
		t.Fatal("entry counted as work")
	}
	send("entry-complete")
	now = now.Add(40 * time.Second)
	send("pause")
	now = now.Add(time.Hour)
	if c.Snapshot().Elapsed != 40*time.Second {
		t.Fatal("pause counted")
	}
	send("resume")
	now = now.Add(25 * time.Second)
	send("tick")
	s := c.Snapshot()
	if s.Phase != "overtime" || s.Elapsed != 65*time.Second || s.Reminders != 1 {
		t.Fatalf("unexpected overtime: %+v", s)
	}
	send("tick")
	send("suspend")
	now = now.Add(time.Hour)
	send("wake")
	if c.Snapshot().Phase != "paused" || c.Snapshot().Reminders != 1 {
		t.Fatal("wake resumed or duplicated reminder")
	}
	send("resume")
	if c.Snapshot().Phase != "overtime" {
		t.Fatal("lost overtime")
	}
}

func TestDedupeConflictAndBoundary(t *testing.T) {
	for _, minutes := range []int{0, 181} {
		c := New("run", time.Now)
		if _, err := c.Handle(Command{RunID: "run", ID: "start", Kind: "start", Goal: "x", Minutes: minutes}); err == nil {
			t.Fatalf("accepted %d", minutes)
		}
		if c.Snapshot().Phase != "idle" {
			t.Fatal("invalid mutated state")
		}
	}
	for _, minutes := range []int{1, 180} {
		c := New("run", time.Now)
		cmd := Command{RunID: "run", ID: "start", Kind: "start", Goal: "x", Minutes: minutes}
		r, err := c.Handle(cmd)
		if err != nil {
			t.Fatal(err)
		}
		duplicate, err := c.Handle(cmd)
		if err != nil || r != duplicate || c.Snapshot().Revision != 1 {
			t.Fatal("duplicate changed state")
		}
		cmd.Goal = "different"
		if _, err := c.Handle(cmd); err == nil {
			t.Fatal("accepted reused ID with different payload")
		}
		if _, err := c.Handle(Command{RunID: "run", ID: "old", Kind: "pause"}); err == nil {
			t.Fatal("accepted old revision")
		}
	}
}

func TestOutsideChoiceDoesNotChangeDefaults(t *testing.T) {
	c := New("run", time.Now)
	for i, kind := range []string{"start", "entry-complete", "outside", "outside", "allow"} {
		_, err := c.Handle(Command{RunID: "run", ID: time.Duration(i + 1).String(), Revision: c.Snapshot().Revision, Kind: kind, Goal: "x", Minutes: 30, Target: "B"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !c.Snapshot().AllowB || c.Snapshot().PendingChoice {
		t.Fatal("allow failed")
	}
	_, err := c.Handle(Command{RunID: "run", ID: "end", Revision: c.Snapshot().Revision, Kind: "end"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Handle(Command{RunID: "run", ID: "restart", Revision: c.Snapshot().Revision, Kind: "start", Goal: "x", Minutes: 30})
	if err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().AllowB {
		t.Fatal("temporary allowance became default")
	}
}

func TestInvalidCommandLeavesStateUnchanged(t *testing.T) {
	c := New("run", time.Now)
	for _, cmd := range []Command{{RunID: "other", ID: "x", Kind: "start", Goal: "x", Minutes: 1}, {RunID: "run", ID: "", Kind: "start"}, {RunID: "run", ID: "x", Kind: "arbitrary"}, {RunID: "run", ID: "x", Kind: "start", Minutes: 30}} {
		if _, err := c.Handle(cmd); err == nil {
			t.Fatal("accepted invalid command")
		}
		if c.Snapshot().Revision != 0 {
			t.Fatal("invalid command mutated state")
		}
	}
}

//go:build darwin

package main

import (
	"focus.local/zhuanzhuli/internal/session"
	"testing"
	"time"
)

func TestCounterRoundsRemainingTimeAndChangesDirectionAtLimit(t *testing.T) {
	for _, c := range []struct {
		elapsed time.Duration
		want    string
	}{
		{0, "01:00"}, {time.Millisecond, "01:00"}, {time.Second, "00:59"},
		{time.Minute - time.Nanosecond, "00:01"}, {time.Minute, "+00:00"},
		{time.Minute + time.Second, "+00:01"},
	} {
		if got := counter(session.State{Minutes: 1, Elapsed: c.elapsed}); got != c.want {
			t.Fatalf("%v: %s != %s", c.elapsed, got, c.want)
		}
	}
}
func TestTrayExposesTimingAndPauseWithoutAnotherClock(t *testing.T) {
	s := session.State{Minutes: 1, Elapsed: 20 * time.Second, Phase: "active"}
	if trayTitle(s) != "◉ 00:40" {
		t.Fatal(trayTitle(s))
	}
	s.Phase = "paused"
	if trayTitle(s) != "Ⅱ 00:40" {
		t.Fatal(trayTitle(s))
	}
	s.Phase = "ended"
	if trayTitle(s) != "◉" {
		t.Fatal(trayTitle(s))
	}
}

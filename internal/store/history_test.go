package store

import (
	"fmt"
	"focus.local/zhuanzhuli/internal/session"
	"testing"
	"time"
)

func TestFocusByDayUsesSavedElapsedAndInclusiveLocalDates(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 11, 2, 12, 0, 0, 0, loc)
	previous := time.Date(2026, 11, 1, 23, 58, 0, 0, loc)
	notStarted := false
	records := []session.State{
		{Started: previous.UTC(), Elapsed: 35 * time.Minute, Minutes: 25, Interrupted: true},
		{Started: today, Elapsed: 61 * time.Second},
		{Started: today, Elapsed: 2 * time.Minute},
		{Started: today, Elapsed: 20 * time.Minute, WorkStarted: &notStarted},
		{Started: today.AddDate(0, 0, -7), Elapsed: time.Hour},
	}
	got := FocusByDay(records, today.AddDate(0, 0, -6), today)
	if len(got) != 2 || got["2026-11-01"] != 35*time.Minute || got["2026-11-02"] != 181*time.Second {
		t.Fatalf("unexpected daily totals: %+v", got)
	}
}

func TestHistorySearchBeforePaginationAndClampsDeletedPage(t *testing.T) {
	rows := []session.State{}
	now := time.Now()
	for i := 0; i < 41; i++ {
		rows = append(rows, session.State{ID: fmt.Sprint(i), Goal: "写作", TemplateName: "WRITE", Started: now.Add(-time.Duration(i) * time.Minute)})
	}
	p, e := QueryHistory(rows, "写", "", "", 2, 20, time.UTC)
	if e != nil || p.Total != 41 || len(p.Records) != 1 || p.Records[0].ID != "40" {
		t.Fatal(p, e)
	}
	p, e = QueryHistory(rows[:40], "写", "", "", 2, 20, time.UTC)
	if e != nil || p.Page != 1 || len(p.Records) != 20 {
		t.Fatal(p, e)
	}
	p, e = QueryHistory(rows, "missing", "", "", 9, 20, time.UTC)
	if e != nil || p.Total != 0 || p.Page != 0 || p.Pages != 0 {
		t.Fatal(p, e)
	}
	if _, e = QueryHistory(rows, "", "2026-11-04", "2026-11-03", 0, 20, time.UTC); e == nil {
		t.Fatal("invalid range accepted")
	}
}
func TestHistoryInclusiveLocalDateAcrossDST(t *testing.T) {
	loc, e := time.LoadLocation("America/New_York")
	if e != nil {
		t.Fatal(e)
	}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	rows := []session.State{{ID: "start", Started: start}, {ID: "end", Started: start.AddDate(0, 0, 1).Add(-time.Nanosecond)}, {ID: "outside", Started: start.AddDate(0, 0, 1)}}
	p, e := QueryHistory(rows, "", "2026-11-01", "2026-11-01", 0, 20, loc)
	if e != nil || p.Total != 2 {
		t.Fatal(p, e)
	}
}

func TestHistorySearchOnlyGoalAndOpenDateBounds(t *testing.T) {
	loc := time.FixedZone("test", 8*3600)
	day := time.Date(2026, 11, 6, 0, 0, 0, 0, loc)
	cfg := session.Config{Apps: []session.App{{ID: "hidden-app", Name: "隐含应用"}}}
	rows := []session.State{
		{ID: "a", Goal: "写作 A", TemplateName: "hidden-template", Started: day, Config: &cfg},
		{ID: "b", Goal: "READ b", Started: day.Add(24*time.Hour - time.Nanosecond)},
		{ID: "outside", Goal: "别的", Started: day.AddDate(0, 0, 1)},
	}
	for _, q := range []string{"hidden-template", "隐含应用", "hidden-app"} {
		p, err := QueryHistory(rows, q, "", "", 0, 20, loc)
		if err != nil || p.Total != 0 {
			t.Fatalf("non-goal query %q matched: %+v %v", q, p, err)
		}
	}
	for _, q := range []string{"写", "read", "READ"} {
		p, err := QueryHistory(rows, q, "2026-11-06", "2026-11-06", 0, 20, loc)
		if err != nil || p.Total != 1 {
			t.Fatal(q, p, err)
		}
	}
	p, err := QueryHistory(rows, "", "", "2026-11-06", 0, 20, loc)
	if err != nil || p.Total != 2 {
		t.Fatal(p, err)
	}
	p, err = QueryHistory(rows, "", "2026-11-07", "", 0, 20, loc)
	if err != nil || p.Total != 1 {
		t.Fatal(p, err)
	}
}

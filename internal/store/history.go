package store

import (
	"fmt"
	"focus.local/zhuanzhuli/internal/session"
	"sort"
	"strings"
	"time"
)

type HistoryPage struct {
	Records            []session.State
	Total, Page, Pages int
}

// FocusByDay groups saved work by its local start date, including both dates.
// Days without work are absent; report views can render them as zero.
func FocusByDay(records []session.State, from, to time.Time) map[string]time.Duration {
	days := make(map[string]time.Duration)
	first, last := from.Format(time.DateOnly), to.In(from.Location()).Format(time.DateOnly)
	for _, r := range records {
		if r.Elapsed <= 0 || (r.WorkStarted != nil && !*r.WorkStarted) {
			continue
		}
		if day := r.Started.In(from.Location()).Format(time.DateOnly); day >= first && day <= last {
			days[day] += r.Elapsed
		}
	}
	return days
}

func QueryHistory(records []session.State, query, from, to string, page, size int, loc *time.Location) (HistoryPage, error) {
	if loc == nil {
		loc = time.Local
	}
	if size < 1 {
		size = 20
	}
	var start, end time.Time
	var err error
	if strings.TrimSpace(from) != "" {
		start, err = time.ParseInLocation("2006-01-02", strings.TrimSpace(from), loc)
		if err != nil {
			return HistoryPage{}, fmt.Errorf("开始日期请使用 YYYY-MM-DD")
		}
	}
	if strings.TrimSpace(to) != "" {
		end, err = time.ParseInLocation("2006-01-02", strings.TrimSpace(to), loc)
		if err != nil {
			return HistoryPage{}, fmt.Errorf("结束日期请使用 YYYY-MM-DD")
		}
		end = end.AddDate(0, 0, 1)
	}
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return HistoryPage{}, fmt.Errorf("开始日期不能晚于结束日期")
	}
	query = strings.ToLower(strings.TrimSpace(query))
	matches := []session.State{}
	for _, r := range records {
		if !start.IsZero() && r.Started.Before(start) || !end.IsZero() && !r.Started.Before(end) {
			continue
		}
		if strings.Contains(strings.ToLower(r.Goal), query) {
			matches = append(matches, r)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Started.After(matches[j].Started) })
	pages := (len(matches) + size - 1) / size
	if page < 0 {
		page = 0
	}
	if pages > 0 && page >= pages {
		page = pages - 1
	}
	if pages == 0 {
		page = 0
	}
	first := page * size
	last := min(first+size, len(matches))
	return HistoryPage{Records: matches[first:last], Total: len(matches), Page: page, Pages: pages}, nil
}

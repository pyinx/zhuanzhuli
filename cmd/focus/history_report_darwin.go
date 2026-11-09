//go:build darwin

package main

import (
	"fmt"
	"time"

	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/store"
)

const (
	reportToday = iota
	reportWeek
	reportMonth
	reportCustom
)

const reportPageSize = 30

type focusReport struct {
	initialized                                             bool
	width                                                   float64
	draw                                                    func(float64)
	period, page                                            int
	from, to, day                                           string
	fromPicker, toPicker                                    *m.DateFilter
	custom, summary, plot, pages, previous, next, pageLabel m.ID
}

func (a *application) buildFocusReport(parent m.ID) {
	r := &a.report
	if !r.initialized {
		r.period = reportWeek
		r.initialized = true
	}
	header := row(parent, 12)
	titleText(header, "每日专注时长", 15)
	m.Flex(header)
	periods := m.Segmented(header, []string{"今天", "近 7 天", "近 30 天", "自定义"}, r.period, m.R(0, 0, 300, 28), func(index int) {
		r.period = index
		r.page = 0
		r.fromPicker.Dismiss()
		r.toPicker.Dismiss()
		m.Hidden(r.custom, r.period != reportCustom)
		a.updateFocusReport()
		m.KeyLoop(a.main.NativeHandle())
	})
	m.Arrange(header, periods, false)
	m.Fixed(periods, "height", 28)
	m.Send(periods, "setAccessibilityLabel:", m.NS("专注时长报表范围"))
	if r.from == "" && r.to == "" && r.period != reportCustom {
		now := time.Now()
		r.from, r.to = now.AddDate(0, 0, -6).Format(time.DateOnly), now.Format(time.DateOnly)
	}
	r.custom = row(parent, 8)
	change := func() { r.page = 0; a.updateFocusReport() }
	r.fromPicker = m.NewDateFilter(r.custom, "报表起始日", r.from, func(v string) { r.from = v; change() })
	r.toPicker = m.NewDateFilter(r.custom, "报表结束日", r.to, func(v string) { r.to = v; change() })
	m.Flex(r.custom)
	m.Hidden(r.custom, r.period != reportCustom)
	r.summary = bodyText(parent, "", 12, true)
	r.plot = m.View("NSView", m.R(0, 0, 1, 166))
	m.Arrange(parent, r.plot, true)
	m.Fixed(r.plot, "height", 166)
	m.Release(r.plot)
	r.pages = row(parent, 8)
	r.previous = action(r.pages, "上一段", func() { r.page--; a.updateFocusReport() })
	r.pageLabel = bodyText(r.pages, "", 12, true)
	r.next = action(r.pages, "下一段", func() { r.page++; a.updateFocusReport() })
	m.Flex(r.pages)
	a.updateFocusReport()
}

func (a *application) updateFocusReport() {
	a.report.day = ""
	a.refreshHistoryStats(time.Now())
}

func (a *application) refreshHistoryStats(now time.Time) {
	r := &a.report
	if day := now.Format(time.DateOnly); r.day == day {
		return
	} else {
		r.day = day
	}
	loc := now.Location()
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	from := to.AddDate(0, 0, -6)
	switch r.period {
	case reportToday:
		from = to
	case reportMonth:
		from = to.AddDate(0, 0, -29)
	case reportCustom:
		var firstErr, lastErr error
		from, firstErr = time.ParseInLocation(time.DateOnly, r.from, loc)
		to, lastErr = time.ParseInLocation(time.DateOnly, r.to, loc)
		if firstErr != nil || lastErr != nil || from.After(to) {
			msg := "请选择报表的起始日和结束日。"
			if firstErr == nil && lastErr == nil {
				msg = "报表起始日不能晚于结束日。"
			}
			m.Text(r.summary, msg)
			r.draw = nil
			m.Hidden(r.plot, true)
			m.Hidden(r.pages, true)
			return
		}
	}
	// Civil dates are counted in UTC so daylight saving doesn't drop a bar.
	civil := func(t time.Time) int64 {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
	}
	days := int(civil(to)-civil(from)) + 1
	pages := (days + reportPageSize - 1) / reportPageSize
	r.page = min(max(0, r.page), pages-1)
	offset := r.page * reportPageSize
	totals := store.FocusByDay(a.store.Data.History, from, to)
	var total, peak time.Duration
	for _, d := range totals {
		total += d
		peak = max(peak, d)
	}
	m.Text(r.summary, fmt.Sprintf("%s — %s  ·  总计 %s", from.Format(time.DateOnly), to.Format(time.DateOnly), spent(total)))
	m.Hidden(r.plot, false)
	m.Hidden(r.pages, pages <= 1)
	m.Enabled(r.previous, r.page > 0)
	m.Enabled(r.next, r.page+1 < pages)
	m.Text(r.pageLabel, fmt.Sprintf("%d / %d", r.page+1, pages))
	r.width = 0
	r.draw = func(width float64) {
		drawFocusBars(r.plot, width, from.AddDate(0, 0, offset), min(reportPageSize, days-offset), totals, peak)
	}
	a.layoutFocusReport()
}

// Chart geometry follows the viewport. Its labels and bars never constrain the window.
func (a *application) layoutFocusReport() {
	r := &a.report
	if r.plot == 0 || r.draw == nil || a.mainPage != pageHistory || a.historySection != 0 {
		return
	}
	m.Send(a.pages[pageHistory], "layoutSubtreeIfNeeded")
	width := m.Bounds(r.plot).W
	if width > 60 && width != r.width {
		r.width = width
		r.draw(width)
	}
}

func drawFocusBars(plot m.ID, width float64, first time.Time, count int, totals map[string]time.Duration, peak time.Duration) {
	m.ClearContent(plot)
	const left, baseline, height = 50.0, 22.0, 124.0
	plotWidth := width - left - 4
	step := reportAxisStep(peak)
	accent := m.Send(m.Class("NSColor"), "controlAccentColor")
	grid := m.Send(m.Class("NSColor"), "separatorColor")
	box := func(parent, color m.ID, rect m.Rect, radius float64) m.ID {
		v := m.View("NSBox", rect)
		m.Send(v, "setBoxType:", uint64(4))
		m.Send(v, "setTitlePosition:", uint64(0))
		m.Send(v, "setBorderWidth:", float64(0))
		m.Send(v, "setCornerRadius:", radius)
		m.Send(v, "setFillColor:", color)
		m.Add(parent, v)
		m.Release(v)
		return v
	}
	for i := 0; i <= 4; i++ {
		y := baseline + height*float64(i)/4
		box(plot, grid, m.R(left, y, plotWidth, 1), 0)
		m.Secondary(m.Label(plot, reportAxisLabel(time.Duration(i)*step, step), m.R(0, y-7, left-4, 15), 10))
	}
	colWidth := plotWidth / float64(count)
	labelStep := max(1, int(34/colWidth)+1)
	for i := 0; i < count; i++ {
		day := first.AddDate(0, 0, i)
		elapsed := totals[day.Format(time.DateOnly)]
		col := m.View("NSView", m.R(left+float64(i)*colWidth, 0, colWidth, 166))
		m.Add(plot, col)
		m.Release(col)
		detail := day.Format(time.DateOnly) + " · " + spent(elapsed)
		m.Send(col, "setToolTip:", m.NS(detail))
		m.Send(col, "setAccessibilityElement:", true)
		m.Send(col, "setAccessibilityRole:", m.NS("AXGroup"))
		m.Send(col, "setAccessibilityLabel:", m.NS(detail))
		barHeight := 0.0
		if elapsed > 0 {
			barHeight = max(2, height*float64(elapsed)/float64(step)/4)
			w := min(42, colWidth*.64)
			bar := box(col, accent, m.R((colWidth-w)/2, baseline, w, barHeight), 3)
			m.Send(bar, "setToolTip:", m.NS(detail))
		}
		center := func(text string, y float64) m.ID {
			label := m.Label(col, text, m.R((colWidth-48)/2, y, 48, 15), 10)
			m.Send(label, "setAlignment:", int64(2))
			return label
		}
		if count <= 7 {
			value := fmt.Sprintf("%d 分", elapsed/time.Minute)
			if elapsed > 0 && elapsed < time.Minute {
				value = fmt.Sprintf("%d 秒", elapsed/time.Second)
			}
			label := center(value, baseline+barHeight+2)
			if elapsed == 0 {
				m.Secondary(label)
			}
		}
		if i%labelStep == 0 {
			m.Secondary(center(day.Format("1/2"), 0))
		}
	}
}

func reportAxisStep(peak time.Duration) time.Duration {
	for _, step := range []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour} {
		if peak <= 4*step {
			return step
		}
	}
	return ((peak/time.Hour + 4) / 4) * time.Hour
}

func reportAxisLabel(d, step time.Duration) string {
	switch {
	case d == 0:
		return "0"
	case step >= time.Minute:
		return fmt.Sprintf("%d 分", d/time.Minute)
	default:
		return fmt.Sprintf("%d 秒", d/time.Second)
	}
}

// Package session owns only the probe's state. It does not control the OS.
package session

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type Command struct {
	RunID    string `json:"runID"`
	ID       string `json:"commandID"`
	Revision uint64 `json:"expectedRevision"`
	Kind     string `json:"kind"`
	Goal     string `json:"goal"`
	Minutes  int    `json:"minutes"`
	Target   string `json:"target"`
}
type View struct {
	Revision      uint64        `json:"revision"`
	Phase         string        `json:"phase"`
	Elapsed       time.Duration `json:"elapsed"`
	Overtime      time.Duration `json:"overtime"`
	Reminders     int           `json:"reminders"`
	AllowB        bool          `json:"allowB"`
	PendingChoice bool          `json:"pendingChoice"`
	Goal          string        `json:"goal"`
	Minutes       int           `json:"minutes"`
}
type Receipt struct {
	Revision uint64 `json:"revision"`
	Phase    string `json:"phase"`
}
type prior struct {
	body    string
	receipt Receipt
}
type Controller struct {
	run    string
	now    func() time.Time
	v      View
	anchor time.Time
	saved  map[string]prior
}

func New(run string, now func() time.Time) *Controller {
	return &Controller{run: run, now: now, v: View{Phase: "idle"}, saved: make(map[string]prior)}
}
func working(phase string) bool { return phase == "active" || phase == "overtime" }
func (c *Controller) Snapshot() View {
	v := c.v
	if working(v.Phase) {
		if d := c.now().Sub(c.anchor); d > 0 {
			v.Elapsed += d
		}
	}
	if extra := v.Elapsed - time.Duration(v.Minutes)*time.Minute; extra > 0 {
		v.Overtime = extra
	}
	return v
}

// Tick observes elapsed time; clock refreshes are not commands to deduplicate.
func (c *Controller) Tick() {
	v := c.Snapshot()
	if working(v.Phase) && v.Elapsed >= time.Duration(v.Minutes)*time.Minute && v.Reminders == 0 {
		v.Phase = "overtime"
		v.Reminders = 1
		v.Revision++
		c.v = v
		c.anchor = c.now()
	}
}
func (c *Controller) Handle(cmd Command) (Receipt, error) {
	if cmd.RunID != c.run || cmd.ID == "" || len(cmd.ID) > 128 {
		return Receipt{}, errors.New("invalid: run or command ID")
	}
	body, _ := json.Marshal(cmd)
	if p, ok := c.saved[cmd.ID]; ok {
		if p.body != string(body) {
			return Receipt{}, errors.New("conflict: command ID reused")
		}
		return p.receipt, nil
	}
	if cmd.Revision != c.v.Revision {
		return Receipt{}, errors.New("conflict: stale revision")
	}
	if len(c.saved) >= 10000 {
		return Receipt{}, errors.New("conflict: run command limit reached; end and start a new run")
	}
	v := c.Snapshot()
	switch cmd.Kind {
	case "start":
		if v.Phase != "idle" && v.Phase != "ended" {
			return Receipt{}, errors.New("conflict: session exists")
		}
		if strings.TrimSpace(cmd.Goal) == "" || utf8.RuneCountInString(cmd.Goal) > 200 || cmd.Minutes < 1 || cmd.Minutes > 180 {
			return Receipt{}, errors.New("invalid: goal or minutes")
		}
		v = View{Revision: v.Revision, Phase: "entry", Goal: strings.TrimSpace(cmd.Goal), Minutes: cmd.Minutes}
	case "entry-complete":
		if v.Phase != "entry" {
			return Receipt{}, errors.New("conflict: not entering")
		}
		v.Phase = "active"
	case "pause", "suspend", "lock":
		if working(v.Phase) || v.Phase == "entry" {
			v.Phase = "paused"
		}
	case "wake", "unlock": // Deliberately never resume on an OS event.
	case "resume":
		if v.Phase != "paused" {
			return Receipt{}, errors.New("conflict: not paused")
		}
		v.Phase = "active"
	case "outside":
		if cmd.Target != "B" {
			return Receipt{}, errors.New("invalid: fixture")
		}
		if working(v.Phase) && !v.AllowB {
			v.PendingChoice = true
		}
	case "allow", "return":
		if !v.PendingChoice {
			return Receipt{}, errors.New("conflict: no pending choice")
		}
		v.PendingChoice = false
		if cmd.Kind == "allow" {
			v.AllowB = true
		}
	case "end":
		v.Phase = "ended"
		v.PendingChoice = false
	default:
		return Receipt{}, errors.New("invalid: command")
	}
	if working(v.Phase) && v.Elapsed >= time.Duration(v.Minutes)*time.Minute {
		v.Phase = "overtime"
		if v.Reminders == 0 {
			v.Reminders = 1
		}
	}
	v.Revision++
	c.v = v
	c.anchor = c.now()
	r := Receipt{v.Revision, v.Phase}
	c.saved[cmd.ID] = prior{string(body), r}
	return r, nil
}

// Package session owns the production session, independent of windows and OS effects.
package session

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type State struct {
	Config          *Config       `json:"config,omitempty"`
	Immersion       bool          `json:"immersion,omitempty"`
	TemplateID      string        `json:"templateID,omitempty"`
	TemplateName    string        `json:"templateName,omitempty"`
	TemporaryApps   []App         `json:"temporaryApps,omitempty"`
	RecoveredConfig bool          `json:"recoveredConfig,omitempty"`
	ID              string        `json:"id"`
	Goal            string        `json:"goal"`
	Minutes         int           `json:"minutes"`
	Scene           string        `json:"scene"`
	Started         time.Time     `json:"started"`
	Phase           string        `json:"phase"`
	Elapsed         time.Duration `json:"elapsed"`
	Allowed         []string      `json:"allowed"`
	// A non-nil empty slice identifies a checkpoint written with app provenance.
	// Allowed remains the merged list so older versions can read this state.
	TemporaryAllowed []string `json:"temporaryAllowed"`
	LegacyAppScope   bool     `json:"legacyAppScope,omitempty"`
	Reminder         bool     `json:"reminder"`
	Partial          bool     `json:"partial"`
	Interrupted      bool     `json:"interrupted,omitempty"`
	// nil means a legacy checkpoint whose work-entry provenance is unknown.
	WorkStarted *bool `json:"workStarted,omitempty"`
}

func (s State) MinutesValue() int {
	if s.Config != nil {
		return s.Config.Minutes
	}
	return s.Minutes
}
func (s State) SceneValue() string {
	if s.Config != nil {
		return s.Config.Scene
	}
	return s.Scene
}
func (s State) AllowedIDs() []string {
	if s.Config == nil {
		return append([]string{}, s.Allowed...)
	}
	ids := s.Config.IDs()
	for _, id := range s.TemporaryAllowed {
		found := false
		for _, v := range ids {
			if id == v {
				found = true
				break
			}
		}
		if !found && id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
func cloneState(s State) State {
	if s.Config != nil {
		c := s.Config.Clone()
		s.Config = &c
	}
	s.TemporaryApps = append([]App{}, s.TemporaryApps...)
	return s
}

type Controller struct {
	state  State
	anchor time.Time
	now    func() time.Time
}

func New(now func() time.Time) *Controller { return &Controller{state: State{Phase: "idle"}, now: now} }
func Working(p string) bool                { return p == "active" || p == "overtime" }
func Exists(p string) bool                 { return p == "entry" || Working(p) || p == "paused" }
func (c *Controller) Snapshot() State {
	s := cloneState(c.state)
	if s.WorkStarted != nil {
		v := *s.WorkStarted
		s.WorkStarted = &v
	}
	s.Allowed = append([]string{}, s.Allowed...)
	if s.TemporaryAllowed != nil {
		s.TemporaryAllowed = append([]string{}, s.TemporaryAllowed...)
	}
	if Working(s.Phase) {
		if d := c.now().Sub(c.anchor); d > 0 {
			s.Elapsed += d
		}
	}
	if Working(s.Phase) && s.Elapsed >= time.Duration(s.MinutesValue())*time.Minute {
		s.Phase = "overtime"
	}
	return s
}
func (c *Controller) Start(s State) error {
	if Exists(c.state.Phase) {
		return errors.New("当前专注尚未结束")
	}
	if s.Config != nil {
		if err := s.Config.Validate(true); err != nil {
			return err
		}
		s = cloneState(s)
		s.Minutes = 0
		s.Scene = ""
		s.Allowed = nil
	}
	s.Goal = strings.TrimSpace(s.Goal)
	if s.Goal == "" || utf8.RuneCountInString(s.Goal) > 200 {
		return errors.New("请输入 1–200 字的目标")
	}
	if s.MinutesValue() < 1 || s.MinutesValue() > 180 {
		return errors.New("请输入 1–180 的整数分钟")
	}
	if len(s.AllowedIDs()) == 0 {
		return errors.New("请至少选择一个专注应用")
	}
	if !s.Partial {
		return errors.New("通知保护尚不完整，需要明确选择部分保护")
	}
	s.Started = c.now()
	s.Phase = "entry"
	s.Elapsed = 0
	s.Reminder = false
	s.Allowed = append([]string{}, s.Allowed...)
	s.TemporaryAllowed = []string{}
	s.LegacyAppScope = false
	workStarted := false
	s.WorkStarted = &workStarted
	c.state = s
	c.anchor = c.now()
	return nil
}
func (c *Controller) Enter() error {
	if c.state.Phase != "entry" {
		return errors.New("当前不在入场阶段")
	}
	c.state.Phase = "active"
	workStarted := true
	c.state.WorkStarted = &workStarted
	c.anchor = c.now()
	return nil
}
func (c *Controller) Pause() {
	if Exists(c.state.Phase) && c.state.Phase != "paused" {
		c.state = c.Snapshot()
		c.state.Phase = "paused"
	}
}

// UseOrdinaryFocus changes this paused session only, after a durable checkpoint.
func (c *Controller) UseOrdinaryFocus(save func(State) error) error {
	s := c.Snapshot()
	if s.Phase != "paused" || !s.Immersion {
		return errors.New("当前没有已暂停的沉浸专注")
	}
	s.Immersion = false
	if err := save(s); err != nil {
		return err
	}
	c.state = s
	return nil
}
func (c *Controller) Resume(partial bool) error {
	if c.state.Phase != "paused" {
		return errors.New("当前未暂停")
	}
	if !partial {
		return errors.New("请确认当前保护状态")
	}
	c.state.Phase = "active"
	workStarted := true
	c.state.WorkStarted = &workStarted
	c.anchor = c.now()
	return nil
}

// ResumeWithApps applies the latest defaults at the same boundary as resume.
// Explicit additions survive default edits for the lifetime of this session.
func (c *Controller) ResumeWithApps(partial bool, defaults []string) error {
	if c.state.Config != nil {
		return c.Resume(partial)
	}
	allowed := []string{}
	for _, id := range append(append([]string{}, defaults...), c.state.TemporaryAllowed...) {
		if id == "" {
			continue
		}
		found := false
		for _, existing := range allowed {
			if existing == id {
				found = true
				break
			}
		}
		if !found {
			allowed = append(allowed, id)
		}
	}
	if len(allowed) == 0 {
		return errors.New("请至少选择一个专注应用")
	}
	if err := c.Resume(partial); err != nil {
		return err
	}
	c.state.Allowed = allowed
	return nil
}

// CommitWork publishes work entry only if its checkpoint is durable. The
// previous provenance and elapsed time survive a failed first resume as well.
func (c *Controller) CommitWork(resume bool, defaults []string, save func(State) error) error {
	before := c.Snapshot()
	var err error
	if resume {
		err = c.ResumeWithApps(true, defaults)
	} else {
		err = c.Enter()
	}
	if err != nil {
		return err
	}
	if err = save(c.Snapshot()); err != nil {
		c.state = before
		c.state.Phase = "paused"
		c.anchor = c.now()
		return err
	}
	return nil
}
func (c *Controller) End() State {
	c.state = c.Snapshot()
	c.state.Phase = "ended"
	return c.Snapshot()
}
func (c *Controller) Allowed(id string) bool {
	for _, a := range c.state.AllowedIDs() {
		if a == id {
			return true
		}
	}
	return false
}
func (c *Controller) Allow(id string) {
	if id != "" && !c.Allowed(id) {
		if c.state.Config == nil {
			c.state.Allowed = append(c.state.Allowed, id)
		}
		c.state.TemporaryAllowed = append(c.state.TemporaryAllowed, id)
	}
}
func (c *Controller) AllowApp(a App) {
	if !c.Allowed(a.ID) {
		c.Allow(a.ID)
		c.state.TemporaryApps = append(c.state.TemporaryApps, a)
	}
}

// CompleteLegacy fixes only a paused legacy checkpoint, after explicit review.
func (c *Controller) CompleteLegacy(cfg Config) error {
	if c.state.Phase != "paused" || c.state.Config != nil {
		return errors.New("当前无需补齐旧会话配置")
	}
	if err := cfg.Validate(true); err != nil {
		return err
	}
	for _, id := range c.state.AllowedIDs() {
		found := false
		for _, a := range cfg.Apps {
			if a.ID == id {
				found = true
			}
		}
		if !found {
			return errors.New("旧会话应用范围不能隐式缩小")
		}
	}
	v := cfg.Clone()
	known := c.state.TemporaryAllowed != nil && !c.state.LegacyAppScope
	if known {
		temps := append([]string{}, c.state.TemporaryAllowed...)
		v.Apps = nil
		c.state.TemporaryApps = nil
		for _, app := range cfg.Apps {
			temporary := false
			for _, id := range temps {
				if app.ID == id {
					temporary = true
					break
				}
			}
			if temporary {
				c.state.TemporaryApps = append(c.state.TemporaryApps, app)
			} else {
				v.Apps = append(v.Apps, app)
			}
		}
		c.state.TemporaryAllowed = temps
	} else {
		c.state.TemporaryAllowed = []string{}
		c.state.LegacyAppScope = true
	}
	c.state.Config = &v
	c.state.Minutes = 0
	c.state.Scene = ""
	c.state.Allowed = nil

	c.state.RecoveredConfig = true
	return nil
}

// Tick emits one reminder per session. View refreshes never allocate command IDs.
func (c *Controller) Tick() bool {
	if c.Snapshot().Phase == "overtime" && !c.state.Reminder {
		c.state = c.Snapshot()
		c.anchor = c.now()
		c.state.Reminder = true
		return true
	}
	return false
}

// Recover never credits unknown time and requires an explicit user resume.
func (c *Controller) Recover(s State) {
	s = cloneState(s)
	if s.WorkStarted != nil {
		v := *s.WorkStarted
		s.WorkStarted = &v
	}
	s.Interrupted = true
	s.Phase = "paused"
	s.Allowed = append([]string{}, s.Allowed...)
	if s.TemporaryAllowed == nil {
		// Freeze the original legacy scope once. Later defaults must not
		// accumulate into it; the separate marker keeps the warning on restart.
		s.TemporaryAllowed = append([]string{}, s.Allowed...)
		s.LegacyAppScope = true
	} else {
		s.TemporaryAllowed = append([]string{}, s.TemporaryAllowed...)
	}
	c.state = s
	c.anchor = c.now()
}

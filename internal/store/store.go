package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"focus.local/zhuanzhuli/internal/media"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/pkg/journal"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type App = session.App
type Preferences struct {
	Appearance    string                  `json:"appearance"`
	Immersion     bool                    `json:"immersion,omitempty"`
	Minutes       int                     `json:"minutes"`
	Scene         string                  `json:"scene"`
	Apps          []App                   `json:"apps"`
	Sound         bool                    `json:"sound"`
	Volume        float64                 `json:"volume"`
	Voice         bool                    `json:"voice"`
	Entry         bool                    `json:"entry"`
	ReminderSound bool                    `json:"reminderSound"`
	Background    bool                    `json:"background"`
	Ambience      *session.AmbienceConfig `json:"ambience,omitempty"`
	FocusLinked   bool                    `json:"focusLinked"`
	FocusProfile  string                  `json:"focusProfile"`
	QuickTemplate string                  `json:"quickTemplate,omitempty"`
}
type Data struct {
	Templates   []Template      `json:"templates"`
	Version     int             `json:"version"`
	Preferences Preferences     `json:"preferences"`
	History     []session.State `json:"history"`
	Current     *session.State  `json:"current,omitempty"`
}
type Store struct {
	Path string
	Data Data
}

func Defaults() Data {
	return Data{Version: 3, Templates: []Template{}, Preferences: Preferences{Appearance: "light", Minutes: 30, Scene: "forest", Volume: 0.3, Ambience: &session.AmbienceConfig{Mode: "off", Volume: .15}, Entry: true, Background: true, Apps: []App{}}, History: []session.State{}}
}
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{Path: filepath.Join(dir, "data.json"), Data: Defaults()}
	b, e := os.ReadFile(s.Path)
	if errors.Is(e, os.ErrNotExist) {
		return s, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, &s.Data); e != nil {
		return nil, errors.New("本地数据损坏；已保留原文件，未覆盖")
	}
	oldVersion := s.Data.Version
	legacy := oldVersion == 1 || oldVersion == 2
	if !legacy && s.Data.Version != 3 {
		return nil, errors.New("本地数据版本不兼容，未覆盖")
	}
	if legacy {
		// Defaults seed missing appearance, but must not mask an absent legacy
		// audio field when reading into the default preferences value.
		var fields struct {
			Preferences map[string]json.RawMessage `json:"preferences"`
		}
		if err := json.Unmarshal(b, &fields); err != nil {
			return nil, err
		}
		if _, found := fields.Preferences["ambience"]; !found {
			s.Data.Preferences.Ambience = nil
		}
	}
	p := s.Data.Preferences
	if p.Appearance != "light" && p.Appearance != "dark" {
		return nil, errors.New("应用外观配置无效，未覆盖")
	}
	if err := p.Config().Validate(false); err != nil {
		return nil, err
	}
	if err := validateTemplates(s.Data.Templates); err != nil {
		return nil, err
	}
	if strings.ContainsRune(p.FocusProfile, 0) || utf8.RuneCountInString(p.FocusProfile) > 80 || (p.FocusLinked && strings.TrimSpace(p.FocusProfile) == "") {
		return nil, errors.New("通知联动配置无效，未覆盖")
	}
	if p.Minutes < 1 || p.Minutes > 180 || p.Volume < 0 || p.Volume > 1 {
		return nil, errors.New("本地偏好数据无效，未覆盖")
	}
	for _, a := range p.Apps {
		if a.ID == "" || !filepath.IsAbs(a.Path) {
			return nil, errors.New("本地应用选择数据无效")
		}
	}
	if c := s.Data.Current; c != nil {
		for _, temporary := range c.TemporaryAllowed {
			if c.Config != nil {
				continue
			}
			found := false
			for _, allowed := range c.Allowed {
				if temporary != "" && temporary == allowed {
					found = true
					break
				}
			}
			if !found {
				return nil, errors.New("本次应用来源无效；已保留原文件，未自动恢复")
			}
		}
		if c.Config != nil {
			if err := c.Config.Validate(false); err != nil {
				return nil, err
			}
		}
		validScene := media.KnownScene(c.SceneValue())
		if c.ID == "" || strings.TrimSpace(c.Goal) == "" || utf8.RuneCountInString(c.Goal) > 200 || c.MinutesValue() < 1 || c.MinutesValue() > 180 || c.Elapsed < 0 || c.Started.IsZero() || len(c.AllowedIDs()) == 0 || !validScene || !session.Exists(c.Phase) {
			return nil, errors.New("当前会话检查点无效；已保留原文件，未自动恢复")
		}
	}
	for _, v := range s.Data.History {
		if v.Config != nil {
			if err := v.Config.Validate(false); err != nil {
				return nil, err
			}
		}
	}
	if legacy {
		// A content-addressed backup is immutable and must be durable before upgrade.
		hash := sha256.Sum256(b)
		backup := filepath.Join(dir, fmt.Sprintf("data.v%d.%x.json", oldVersion, hash))
		if err := immutableBackup(backup, b); err != nil {
			return nil, err
		}

		s.Data.Preferences.SetConfig(s.Data.Preferences.Config().IndependentAudio())
		for i := range s.Data.Templates {
			s.Data.Templates[i].Config = s.Data.Templates[i].Config.IndependentAudio()
		}
		if c := s.Data.Current; c != nil && c.Config != nil {
			v := c.Config.IndependentAudio()
			c.Config = &v
		}
		// History is evidence of past configuration, never a migration guess.
		s.Data.Version = 3
		if err := s.Save(); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *Store) Save() error { return journal.WriteJSON(s.Path, s.Data) }
func (s *Store) Checkpoint(v session.State) error {
	next := s.Data
	next.Current = &v
	if err := journal.WriteJSON(s.Path, next); err != nil {
		return err
	}
	s.Data = next
	return nil
}
func (s *Store) SetPreferences(p Preferences) error {
	p.SetConfig(p.Config().IndependentAudio())
	if err := p.Config().Validate(false); err != nil {
		return err
	}
	if p.Appearance != "light" && p.Appearance != "dark" {
		return errors.New("应用外观配置无效")
	}
	next := s.Data
	p.Apps = append([]App{}, p.Apps...)
	next.Preferences = p
	if err := journal.WriteJSON(s.Path, next); err != nil {
		return err
	}
	s.Data = next
	return nil
}
func orderHistory(records []session.State) {
	sort.SliceStable(records, func(i, j int) bool { return records[i].Started.After(records[j].Started) })
}
func (s *Store) Finish(v session.State) error {
	next := s.Data
	if v.WorkStarted == nil || *v.WorkStarted {
		next.History = append([]session.State{v}, s.Data.History...)
		orderHistory(next.History)
	}
	next.Current = nil
	if err := journal.WriteJSON(s.Path, next); err != nil {
		return err
	}
	s.Data = next
	return nil
}
func (s *Store) Delete(id string) (session.State, int, error) {
	for i, v := range s.Data.History {
		if v.ID == id {
			next := s.Data
			next.History = append(append([]session.State{}, s.Data.History[:i]...), s.Data.History[i+1:]...)
			if e := journal.WriteJSON(s.Path, next); e != nil {
				return session.State{}, 0, e
			}
			s.Data = next
			return v, i, nil
		}
	}
	return session.State{}, 0, errors.New("记录已不存在")
}
func (s *Store) Undo(v session.State, index int) error {
	for _, x := range s.Data.History {
		if x.ID == v.ID {
			return nil
		}
	}
	if index < 0 || index > len(s.Data.History) {
		index = len(s.Data.History)
	}
	next := s.Data
	h := append([]session.State{}, s.Data.History...)
	h = append(h, session.State{})
	copy(h[index+1:], h[index:])
	h[index] = v
	next.History = h
	orderHistory(next.History)
	if e := journal.WriteJSON(s.Path, next); e != nil {
		return e
	}
	s.Data = next
	return nil
}

func immutableBackup(path string, body []byte) error {
	if old, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(old, body) {
			return errors.New("旧数据备份冲突，未升级")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".data-v1-backup-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Link(name, path); errors.Is(err, os.ErrExist) {
		old, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(old, body) {
			return errors.New("旧数据备份冲突，未升级")
		}
	} else if err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

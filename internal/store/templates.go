package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/pkg/journal"
	"strings"
	"unicode/utf8"
)

type Template struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Config session.Config `json:"config"`
}

func (p Preferences) Config() session.Config {
	return (session.Config{Minutes: p.Minutes, Scene: p.Scene, Apps: p.Apps, Sound: p.Sound, Volume: p.Volume, Voice: p.Voice, Entry: p.Entry, ReminderSound: p.ReminderSound, Background: p.Background, Ambience: p.Ambience}).Clone()
}
func (p *Preferences) SetConfig(c session.Config) {
	p.Minutes = c.Minutes
	p.Scene = c.Scene
	p.Apps = append([]App{}, c.Apps...)
	p.Sound = c.Sound
	p.Volume = c.Volume
	p.Voice = c.Voice
	p.Entry = c.Entry
	p.ReminderSound = c.ReminderSound
	p.Background = c.Background
	p.Ambience = c.Clone().Ambience
}
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func validateTemplates(items []Template) error {
	ids, names := map[string]bool{}, map[string]bool{}
	for _, v := range items {
		name := strings.TrimSpace(v.Name)
		key := strings.ToLower(name)
		if v.ID == "" || ids[v.ID] || name == "" || name != v.Name || utf8.RuneCountInString(name) > 80 || strings.ContainsRune(name, 0) || names[key] {
			return errors.New("模版名称须为 1–80 字且不能重复")
		}
		if err := v.Config.Validate(false); err != nil {
			return err
		}
		ids[v.ID] = true
		names[key] = true
	}
	return nil
}
func (s *Store) SaveTemplate(v Template) (Template, error) {
	v.Name = strings.TrimSpace(v.Name)
	v.Config = v.Config.IndependentAudio()
	if v.ID == "" {
		id, err := NewID()
		if err != nil {
			return v, err
		}
		v.ID = id
	}
	items := append([]Template{}, s.Data.Templates...)
	found := false
	for i, old := range items {
		if old.ID == v.ID {
			items[i] = v
			found = true
			break
		}
	}
	if !found {
		items = append(items, v)
	}
	if err := validateTemplates(items); err != nil {
		return v, err
	}
	next := s.Data
	next.Templates = items
	if err := journal.WriteJSON(s.Path, next); err != nil {
		return v, err
	}
	s.Data = next
	result := v
	result.Config = v.Config.Clone()
	return result, nil
}
func (s *Store) DeleteTemplate(id string) error {
	items := []Template{}
	found := false
	for _, v := range s.Data.Templates {
		if v.ID == id {
			found = true
		} else {
			items = append(items, v)
		}
	}
	if !found {
		return errors.New("模版已不存在")
	}
	next := s.Data
	next.Templates = items
	if next.Preferences.QuickTemplate == id {
		next.Preferences.QuickTemplate = ""
	}
	if err := journal.WriteJSON(s.Path, next); err != nil {
		return err
	}
	s.Data = next
	return nil
}

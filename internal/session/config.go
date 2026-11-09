package session

import (
	"errors"
	"focus.local/zhuanzhuli/internal/media"
	"math"
	"path/filepath"
	"strings"
)

type App struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type AmbienceConfig struct {
	Mode   string  `json:"mode"`
	Volume float64 `json:"volume"`
}

// Config is a value, copied at the edit/start boundary. It never owns a clock.
type Config struct {
	Minutes       int             `json:"minutes"`
	Scene         string          `json:"scene"`
	Apps          []App           `json:"apps"`
	Sound         bool            `json:"sound"`
	Volume        float64         `json:"volume"`
	Voice         bool            `json:"voice"`
	Entry         bool            `json:"entry"`
	ReminderSound bool            `json:"reminderSound"`
	Background    bool            `json:"background"`
	Ambience      *AmbienceConfig `json:"ambience,omitempty"`
}

func (c Config) Clone() Config {
	c.Apps = append([]App{}, c.Apps...)
	if c.Ambience != nil {
		v := *c.Ambience
		c.Ambience = &v
	}
	return c
}

// IndependentAudio is the one upgrade boundary for legacy mixed audio.
// Historical configs remain untouched; editable/live copies gain explicit values.
func (c Config) IndependentAudio() Config {
	c = c.Clone()
	if c.Ambience == nil {
		mode, volume := "off", c.Volume
		if c.Sound {
			mode = "auto"
		}
		if item, ok := media.DescribeScene(c.Scene); ok {
			volume *= item.LegacyNatureRatio
		}
		c.Ambience = &AmbienceConfig{Mode: mode, Volume: volume}
	}
	return c
}
func (c Config) Validate(requireApps bool) error {
	if c.Minutes < 1 || c.Minutes > 180 {
		return errors.New("请输入 1–180 的整数分钟")
	}
	if math.IsNaN(c.Volume) || math.IsInf(c.Volume, 0) || c.Volume < 0 || c.Volume > 1 {
		return errors.New("音量须在 0–100% 之间")
	}
	if v := c.Ambience; v != nil {
		if v.Mode != "auto" && v.Mode != "off" && media.AmbienceName(v.Mode) == "" {
			return errors.New("白噪音选择无效")
		}
		if math.IsNaN(v.Volume) || math.IsInf(v.Volume, 0) || v.Volume < 0 || v.Volume > 1 {
			return errors.New("白噪音音量须在 0–100% 之间")
		}
	}
	if !media.KnownScene(c.Scene) {
		return errors.New("环境配置无效")
	}
	if requireApps && len(c.Apps) == 0 {
		return errors.New("请至少选择一个专注应用")
	}
	seen := map[string]bool{}
	for _, a := range c.Apps {
		if strings.TrimSpace(a.ID) == "" || !filepath.IsAbs(a.Path) || seen[a.ID] {
			return errors.New("专注应用身份、路径无效或重复")
		}
		seen[a.ID] = true
	}
	return nil
}
func (c Config) IDs() []string {
	ids := []string{}
	for _, a := range c.Apps {
		ids = append(ids, a.ID)
	}
	return ids
}

func MatchApp(a App, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	return strings.Contains(strings.ToLower(a.Name), q) || strings.Contains(strings.ToLower(a.ID), q)
}

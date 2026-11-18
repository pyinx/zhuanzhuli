//go:build darwin

package main

import (
	"errors"
	"fmt"
	"focus.local/zhuanzhuli/internal/media"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"github.com/egoist/mygo"
	"time"
)

func (a *application) ambiencePath(c session.Config) (string, string, error) {
	c = c.IndependentAudio()
	v := c.Ambience
	if v.Mode == "off" || v.Volume == 0 {
		a.recordMediaFailure(c.Scene, ".nature", nil)
		return "", "", nil
	}
	id := v.Mode
	if id == "auto" {
		var ok bool
		id, ok = media.AutomaticAmbience(c.Scene)
		if !ok {
			return "", "", errors.New("此环境没有可用的自动白噪音，请手动选择")
		}
	}
	item, ok := a.catalog.Ambience(id)
	if !ok {
		err := fmt.Errorf("%s录音资源缺失", media.AmbienceName(id))
		a.recordMediaFailure(c.Scene, ".nature", err)
		return "", id, err
	}
	path, err := media.Resolve(a.resources, item.Audio)
	a.recordMediaFailure(c.Scene, ".nature", err)
	return path, id, err
}

func (a *application) buildEnvironmentControls(root m.ID, f *configForm) {
	get, changed := f.get, f.changed
	*get() = get().IndependentAudio()
	c := get()
	titleText(root, "自然环境", 13)
	sceneRoot := root
	background := m.Check(root, "显示自然背景", c.Background, m.R(0, 0, 400, 26), func(v bool) {
		get().Background = v
		if f.isFocus {
			if !v && a.auditionNotice == f.notice && a.previewVideo.Player != 0 {
				a.stopAudition()
			}
			m.Hidden(sceneRoot, !v)
			m.KeyLoop(a.main.NativeHandle())
		}
		changed()
	})
	if f.isFocus {
		m.Arrange(root, background, true)
		sceneRoot = m.Stack(root, true, 8)
		m.Arrange(root, sceneRoot, true)
		m.Hidden(sceneRoot, !c.Background)
	}
	f.mediaGroup, f.mediaCaption = a.mediaFailureControlsFor(sceneRoot, func() string { return get().Scene })
	choices := []m.SceneChoice{}
	if _, ok := a.catalog.Scene(c.Scene); !ok && c.Scene != "" {
		choices = append(choices, m.SceneChoice{ID: c.Scene, Name: media.SceneName(c.Scene) + "（暂不可用）"})
	}
	for _, scene := range a.catalog.Scenes {
		path := a.mediaFor(scene.ID, ".png")
		choices = append(choices, m.SceneChoice{ID: scene.ID, Name: scene.Name, Path: path, Available: path != ""})
	}
	for _, choice := range choices {
		f.sceneIDs = append(f.sceneIDs, choice.ID)
	}
	f.grid = m.NewSceneGrid(sceneRoot, choices, c.Scene, func(id string) { a.stopAudition(); get().Scene = id; f.refreshScene(); changed() })
	f.scenePicker = f.grid.View
	f.sceneCaption = bodyText(sceneRoot, "", 12, true)
	preview := action(sceneRoot, "预览入场", func() { a.previewScene(get().Clone(), f.notice) })
	if m.ReducedMotion() {
		m.Enabled(preview, false)
		bodyText(sceneRoot, "减少动态效果已开启，将直接进入静态环境。", 12, true)
	}
	var refreshEffects func()
	if f.isFocus {
		action(root, "声音与入场设置…", a.openFocusSettings)
	} else {
		m.Arrange(root, background, true)
		refreshEffects = a.buildFocusEffects(root, f, nil)
	}
	f.notice = message(root)
	f.refreshScene = func() {
		id := get().Scene
		f.scenePreview = f.grid.Select(id)
		for i, choice := range f.grid.Choices {
			path := a.mediaFor(choice.ID, ".png")
			ok := f.grid.SetImage(i, path)
			m.Enabled(f.grid.Buttons[i], ok)
			f.grid.Choices[i].Available = ok
			if !ok && path != "" {
				a.recordMediaFailure(choice.ID, ".png", errors.New("背景图片无法解码"))
			}
		}
		description := "此场景暂不可用，请选择自然环境。"
		if item, ok := media.DescribeScene(id); ok && !item.Legacy {
			description = item.Sound
		}
		m.Text(f.sceneCaption, media.SceneName(id)+" · "+description)
		caption := a.formMediaFailureCaption(f)
		m.Text(f.mediaCaption, caption)
		m.Hidden(f.mediaGroup, caption == "")
		if refreshEffects != nil {
			refreshEffects()
		}
	}
	f.onClose = func() {
		if a.auditionNotice == f.notice {
			a.stopAudition()
			a.auditionNotice = 0
		}
	}
	f.refreshScene()
}

func (a *application) buildFocusEffects(root m.ID, f *configForm, commit func(func(*session.Config))) func() {
	get := f.get
	inSettings := commit != nil
	if commit == nil {
		commit = func(edit func(*session.Config)) {
			edit(get())
			f.changed()
		}
	}
	*get() = get().IndependentAudio()
	c := get()
	titleText(root, "声音", 13)
	group, g := m.GroupStack(root, 8)
	m.Arrange(root, group, true)
	var musicSlider, natureSlider, musicSwitch, naturePicker, natureCaption m.ID
	modes := []string{"auto", "off", "rain", "birds", "waves", "stream"}
	refreshSound := func() {
		cfg := get()
		m.Enabled(musicSlider, cfg.Sound)
		m.Enabled(natureSlider, cfg.Ambience.Mode != "off")
		musicText := fmt.Sprintf("音乐音量 %d%%", int(cfg.Volume*100+.5))
		if cfg.Volume == 0 {
			musicText += " · 静音"
		}
		m.Text(f.volumeCaption, musicText)
		text := fmt.Sprintf("白噪音音量 %d%%", int(cfg.Ambience.Volume*100+.5))
		if cfg.Ambience.Volume == 0 {
			text += " · 静音"
		}
		m.Text(natureCaption, text)
		label := "自动"
		if id, ok := media.AutomaticAmbience(cfg.Scene); ok {
			label += " · " + media.AmbienceName(id)
		} else {
			label += " · 暂不可用"
		}
		item := m.Send(naturePicker, "itemAtIndex:", int64(0))
		m.Send(item, "setTitle:", m.NS(label))
	}
	musicSwitch = m.Check(g, "背景音乐", c.Sound, m.R(0, 0, 400, 26), func(v bool) {
		a.stopAudition()
		commit(func(c *session.Config) { c.Sound = v })
		refreshSound()
	})
	m.Arrange(g, musicSwitch, true)
	musicSlider = m.Slider(g, c.Volume, m.R(0, 0, 400, 24), func(v float64) {
		commit(func(c *session.Config) { c.Volume = v })
		if a.auditionNotice == f.notice {
			a.audition.Volume(get().Volume)
		}
		refreshSound()
	})
	m.Arrange(g, musicSlider, true)
	m.Fixed(musicSlider, "height", 24)
	m.Send(musicSlider, "setAccessibilityLabel:", m.NS("音乐音量"))
	f.volumeCaption = bodyText(g, "", 12, true)
	bodyText(g, "白噪音", 13, false)
	selected := 0
	for i, mode := range modes {
		if mode == c.Ambience.Mode {
			selected = i
		}
	}
	naturePicker = m.Popup(g, []string{"自动", "关闭", "下雨", "鸟叫", "海浪", "流水"}, selected, m.R(0, 0, 300, 28), func(i int) {
		if i >= 0 && i < len(modes) {
			a.stopAudition()
			commit(func(c *session.Config) { c.Ambience.Mode = modes[i] })
			refreshSound()
		}
	})
	m.Arrange(g, naturePicker, true)
	m.Send(naturePicker, "setAccessibilityLabel:", m.NS("白噪音选择"))
	natureSlider = m.Slider(g, c.Ambience.Volume, m.R(0, 0, 400, 24), func(v float64) {
		commit(func(c *session.Config) { c.Ambience.Volume = v })
		if a.auditionNotice == f.notice {
			a.auditionNature.Volume(get().Ambience.Volume)
		}
		refreshSound()
	})
	if inSettings {
		m.Send(musicSlider, "setContinuous:", false)
		m.Send(natureSlider, "setContinuous:", false)
	}
	m.Arrange(g, natureSlider, true)
	m.Fixed(natureSlider, "height", 24)
	m.Send(natureSlider, "setAccessibilityLabel:", m.NS("白噪音音量"))
	natureCaption = bodyText(g, "", 12, true)
	f.auditionButton = action(g, "试听当前组合 5 秒", func() { a.auditionConfig(get().Clone(), f.notice) })
	bodyText(g, "自然录音随应用离线可用。关闭只影响对应声轨。", 12, true)
	advancedHeader := row(root, 8)
	toggle := action(advancedHeader, "▸ 入场与提醒", nil)
	m.Flex(advancedHeader)
	summary := bodyText(root, "", 12, true)
	advanced := m.Stack(root, true, 8)
	m.Arrange(root, advanced, true)
	expanded := inSettings
	m.Hidden(advanced, !expanded)
	if expanded {
		m.Send(toggle, "setTitle:", m.NS("▾ 入场与提醒"))
	}
	m.Bind(toggle, func() {
		expanded = !expanded
		m.Hidden(advanced, !expanded)
		title := "▸ 入场与提醒"
		if expanded {
			title = "▾ 入场与提醒"
		}
		m.Send(toggle, "setTitle:", m.NS(title))
		m.KeyLoop(a.main.NativeHandle())
	})
	refreshSummary := func() {
		on := func(v bool) string {
			if v {
				return "开启"
			}
			return "关闭"
		}
		cfg := get()
		m.Text(summary, fmt.Sprintf("入场%s · 人声%s · 到点提示%s", on(cfg.Entry), on(cfg.Voice), on(cfg.ReminderSound)))
	}
	check := func(title string, on bool, set func(*session.Config, bool)) m.ID {
		v := m.Check(advanced, title, on, m.R(0, 0, 400, 26), func(v bool) {
			commit(func(c *session.Config) { set(c, v) })
			refreshSummary()
		})
		m.Arrange(advanced, v, true)
		return v
	}
	entry := check("播放入场短片", c.Entry, func(c *session.Config, v bool) { c.Entry = v })
	bodyText(advanced, "关闭「显示自然背景」时，本次也会跳过入场短片。", 12, true)
	voice := check("入场人声引导", c.Voice, func(c *session.Config, v bool) { c.Voice = v })
	action(advanced, "预听人声", func() { a.previewGuide(get().Scene, f.notice) })
	reminder := check("到点提示音", c.ReminderSound, func(c *session.Config, v bool) { c.ReminderSound = v })
	action(advanced, "试听到点提示音", func() {
		if err := playReminderSound(); err != nil {
			showMessage(f.notice, "提示音未播放，请检查系统声音输出。")
		} else {
			showMessage(f.notice, "已尝试播放提示音；系统静音时不会发声。")
		}
	})
	refreshSound()
	refreshSummary()
	return func() {
		if inSettings {
			cfg := get()
			m.SetChecked(musicSwitch, cfg.Sound)
			m.Send(musicSlider, "setDoubleValue:", cfg.Volume)
			m.Send(natureSlider, "setDoubleValue:", cfg.Ambience.Volume)
			for i, mode := range modes {
				if mode == cfg.Ambience.Mode {
					m.Send(naturePicker, "selectItemAtIndex:", int64(i))
					break
				}
			}
			m.SetChecked(entry, cfg.Entry)
			m.SetChecked(voice, cfg.Voice)
			m.SetChecked(reminder, cfg.ReminderSound)
		}
		refreshSound()
		refreshSummary()
	}
}

func (a *application) formMediaFailureCaption(f *configForm) string {
	caption := a.mediaFailureCaptionFor(f.get().Scene)
	if f.grid != nil {
		for _, choice := range f.grid.Choices {
			if choice.ID != f.get().Scene && !choice.Available {
				if caption != "" {
					caption += "、"
				}
				caption += choice.Name + "的画面不可用"
			}
		}
	}
	return caption
}

func (a *application) previewGuide(sceneID string, notice m.ID) {
	if session.Working(a.controller.Snapshot().Phase) || a.controller.Snapshot().Phase == "entry" {
		showMessage(notice, "暂停专注后可预听。")
		return
	}
	a.stopAudition()
	a.auditionNotice = notice
	path, err := media.Resolve(a.resources, a.catalog.Guide)
	if err == nil {
		err = a.audition.PlayOnce(path, .55)
	}
	a.recordMediaFailure(sceneID, ".guide", err)
	if err != nil {
		a.auditionError = "入场人声无法预听，请重新读取资源。"
		showMessage(notice, a.auditionError)
		a.refresh()
		return
	}
	// The guide is bounded by the entry duration; stop the short-lived player
	// after ten seconds even when a corrupt completion event is missing.
	generation := a.auditionGeneration
	a.auditionTimer = time.AfterFunc(10*time.Second, func() {
		mygo.RunOnMain(func() {
			if generation == a.auditionGeneration {
				a.stopAudition()
			}
		})
	})
	showMessage(notice, "正在预听入场人声。")
}
func (a *application) previewScene(c session.Config, notice m.ID) {
	if m.ReducedMotion() {
		showMessage(notice, "减少动态效果已开启。")
		return
	}
	if session.Working(a.controller.Snapshot().Phase) || a.controller.Snapshot().Phase == "entry" {
		showMessage(notice, "暂停专注后可预览。")
		return
	}
	a.stopAudition()
	a.auditionNotice = notice
	path := a.mediaFor(c.Scene, ".mp4")
	if path == "" {
		a.auditionError = "入场短片不可用，请重新读取资源。"
		a.refresh()
		return
	}
	w := window("入场预览 · "+media.SceneName(c.Scene), 800, 560, false)
	a.previewWindow = w
	a.previewSceneID, a.previewAt = c.Scene, time.Now()
	root := m.FixedSurface(w.NativeHandle(), 800, 560, false)
	if err := a.previewVideo.Play(root, path, m.R(0, 60, 800, 500), 0); err != nil {
		a.stopAudition()
		a.recordMediaFailure(c.Scene, ".mp4", err)
		a.auditionError = "入场短片无法播放，请重新读取资源。"
		a.refresh()
		return
	}
	m.Label(root, "仅预览画面，不开始专注", m.R(20, 20, 500, 24), 13)
	b := m.Button(root, "关闭预览", m.R(660, 16, 120, 30), a.stopAudition)
	m.Send(b, "setKeyEquivalent:", m.NS("\x1b"))
	w.OnClose(func(*mygo.CloseEvent) {
		if a.previewWindow == w {
			a.previewWindow = nil
			a.previewVideo.Stop()
			a.previewSceneID, a.previewAt = "", time.Time{}
		}
	})
	w.OnClosed(func() {
		m.UnbindTree(root)
		if a.previewWindow == w {
			a.previewWindow = nil
		}
	})
	w.Show()
	w.Focus()
}

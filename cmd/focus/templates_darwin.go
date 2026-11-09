//go:build darwin

package main

import (
	"errors"
	"fmt"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"reflect"
	"strings"
)

func (a *application) templateIsDirty() bool {
	if a.templateDraft == nil {
		return false
	}
	v := *a.templateDraft
	if a.templateName != 0 {
		v.Name = m.Value(a.templateName)
	}
	return a.templateBaseline == nil || !reflect.DeepEqual(v, *a.templateBaseline)
}
func (a *application) savedTemplate(id string) (store.Template, bool) {
	for _, v := range a.store.Data.Templates {
		if v.ID == id {
			return v, true
		}
	}
	return store.Template{}, false
}
func (a *application) startSavedTemplate(id string) {
	if a.busy {
		return
	}
	if session.Exists(a.controller.Snapshot().Phase) {
		a.openControl(true)
		return
	}
	v, ok := a.savedTemplate(id)
	if !ok {
		a.openMain()
		a.fail(noticeInfo, "固定模版已不可用，请重新选择。", nil)
		return
	}
	if !a.startConfigured(v.Name, v.Config.Clone(), v.ID, v.Name) && !a.mainModal() {
		a.showPage(pageTemplates)
		if !a.templateIsDirty() && (a.templateDraft == nil || a.templateDraft.ID != id) {
			for i, item := range a.store.Data.Templates {
				if item.ID == id {
					a.loadTemplate(i + 1)
					break
				}
			}
		}
		showMessage(a.templateNotice, a.notice.Text)
	}
}
func (a *application) setQuickTemplate(id string) bool {
	if !a.updatePreferences(func(p *store.Preferences) { p.QuickTemplate = id }) {
		a.fail(noticeStorage, "菜单栏快捷开始未保存。", nil)
		return false
	}
	if a.root != 0 {
		a.renderReady()
	}
	a.refresh()
	return true
}
func (a *application) loadTemplate(index int) {
	load := func() {
		a.templateFromFocus = false
		a.templateDraft = nil
		a.templateBaseline = nil
		if index > 0 && index-1 < len(a.store.Data.Templates) {
			v := a.store.Data.Templates[index-1]
			v.Config = v.Config.Clone()
			a.templateDraft = &v
			base := v
			base.Config = v.Config.Clone()
			a.templateBaseline = &base
		}
		a.renderTemplates()
	}
	if a.templateIsDirty() {
		index := 0
		for i, v := range a.store.Data.Templates {
			if a.templateDraft != nil && v.ID == a.templateDraft.ID {
				index = i + 1
			}
		}
		m.Send(a.templatePicker, "selectItemAtIndex:", int64(index))
		a.confirmAction("放弃模版的未保存修改？", "已保存的模版与本次专注不会改变。", "放弃修改", load)
	} else {
		load()
	}
}
func (a *application) renderTemplates() {
	a.templateForm.close()
	a.templateForm = nil
	a.templateName = 0
	a.templateNotice = 0
	a.pageResponders[pageTemplates] = 0
	root := a.pageRoot(pageTemplates)
	a.templateRoot = root
	m.ClearContent(root)
	r, footer, _ := contentFooter(root)
	titleText(r, "模版", 22)
	bodyText(r, "保存常用配置。快捷开始用模版名称作为目标，系统通知仍可能出现。", 12, true)
	names := []string{"选择模版…"}
	index := 0
	for i, v := range a.store.Data.Templates {
		names = append(names, v.Name)
		if a.templateDraft != nil && v.ID == a.templateDraft.ID {
			index = i + 1
		}
	}
	picker := m.Popup(r, names, index, m.R(0, 0, 300, 28), a.loadTemplate)
	m.Arrange(r, picker, true)
	a.templatePicker = picker
	actions := row(r, 8)
	action(actions, "新建模版", func() {
		create := func() {
			a.templateDraft = &store.Template{Config: a.store.Data.Preferences.Config()}
			a.templateBaseline = nil
			a.templateFromFocus = false
			a.renderTemplates()
		}
		if a.templateIsDirty() {
			a.confirmAction("放弃未保存修改？", "新建前先处理当前模版草稿。", "放弃并新建", create)
		} else {
			create()
		}
	})
	m.Flex(actions)
	if a.templateDraft == nil {
		if len(a.store.Data.Templates) == 0 {
			bodyText(r, "新建你的第一份模版，下一次就能一键开始。", 13, true)
		}
		for i, v := range a.store.Data.Templates {
			line := row(r, 8)
			label := bodyText(line, fmt.Sprintf("%s · %d 分钟", v.Name, v.Config.Minutes), 13, false)
			m.LimitGoalLabel(label)
			m.Send(label, "setToolTip:", m.NS(v.Name))
			m.Flex(line)
			start := action(line, "开始", func() { a.startSavedTemplate(v.ID) })
			m.Send(start, "setAccessibilityLabel:", m.NS("以部分保护开始 "+v.Name))
			edit := action(line, "编辑", func() { a.loadTemplate(i + 1) })
			m.Send(edit, "setAccessibilityLabel:", m.NS("编辑 "+v.Name))
		}
		return
	}
	action(actions, "复制", func() {
		v := *a.templateDraft
		v.ID = ""
		v.Config = v.Config.Clone()
		base := strings.TrimSpace(v.Name) + " 副本"
		v.Name = base
		for n := 2; a.templateNameExists(v.Name); n++ {
			v.Name = fmt.Sprintf("%s %d", base, n)
		}
		a.templateDraft = &v
		a.templateBaseline = nil
		a.templateFromFocus = false
		a.renderTemplates()
	})
	if a.templateDraft.ID != "" {
		id := a.templateDraft.ID
		shortcut := row(r, 8)
		action(shortcut, "开始已保存模版", func() { a.startSavedTemplate(id) })
		pinLabel := "固定到菜单栏…"
		if a.store.Data.Preferences.QuickTemplate == id {
			pinLabel = "取消菜单栏固定"
		}
		action(shortcut, pinLabel, func() {
			set := func(value string) {
				if a.setQuickTemplate(value) {
					a.renderTemplates()
				}
			}
			if a.store.Data.Preferences.QuickTemplate == id {
				set("")
				return
			}
			v, ok := a.savedTemplate(id)
			if !ok {
				return
			}
			a.confirmAction("固定这个模版？", fmt.Sprintf("空闲时单击菜单栏将以部分保护开始「%s」的 %d 分钟专注。右键可打开%s；在模版页可取消固定。", v.Name, v.Config.Minutes, appDisplayName), "固定", func() { set(id) })
		})
		m.Flex(shortcut)
		bodyText(r, "使用已保存的配置和模版名称作为目标；系统通知仍可能出现。", 12, true)
		action(actions, "删除模版…", func() {
			a.confirmAction("删除这个模版？", "历史与当前专注会保留。本次已载入的配置将转为自定义。", "删除模版", func() {
				if err := a.store.DeleteTemplate(id); err != nil {
					showMessage(a.templateNotice, "删除未保存："+err.Error())
					return
				}
				if a.focusDraft != nil && a.focusDraft.sourceID == id {
					a.focusDraft.sourceID = ""
					a.focusDraft.sourceName = ""
				}
				a.templateDraft = nil
				a.templateBaseline = nil
				a.renderTemplates()
				if a.root != 0 {
					a.renderReady()
				}
			})
		})
	}
	titleText(r, "模版名称", 13)
	a.templateName = field(r, a.templateDraft.Name, "例如，写作", "模版名称，1 到 80 字")
	m.ObserveText(a.templateName, func() {
		a.quitDiscarded = false
		a.templateDraft.Name = m.Value(a.templateName)
	})
	if a.templateDurationOwner != a.templateDraft {
		a.templateDurationOwner = a.templateDraft
		a.templateDuration = durationEdit{}
	}
	a.templateForm = a.buildConfigForm(r, func() *session.Config { return &a.templateDraft.Config }, &a.templateDuration, func() { a.quitDiscarded = false }, false)
	a.templateNotice = message(footer)
	buttons := row(footer, 8)
	action(buttons, "取消修改", func() {
		a.templateDraft = nil
		if a.templateBaseline != nil {
			v := *a.templateBaseline
			v.Config = v.Config.Clone()
			a.templateDraft = &v
		}
		a.renderTemplates()
	})
	m.Flex(buttons)
	save := action(buttons, "保存模版", a.saveTemplateDraft)
	m.Primary(save)
	m.KeyLoop(a.main.NativeHandle())
}
func (a *application) templateNameExists(name string) bool {
	for _, v := range a.store.Data.Templates {
		if strings.EqualFold(strings.TrimSpace(name), v.Name) {
			return true
		}
	}
	return false
}
func (a *application) saveTemplateDraft() {
	if a.templateDraft == nil {
		return
	}
	a.quitDiscarded = false
	a.templateDraft.Name = m.Value(a.templateName)
	v, err := a.store.SaveTemplate(*a.templateDraft)
	if err != nil {
		showMessage(a.templateNotice, "模版未保存："+err.Error())
		return
	}
	a.templateDraft = &v
	base := v
	base.Config = v.Config.Clone()
	a.templateBaseline = &base
	if a.templateFromFocus {
		a.focusDraft.sourceID = v.ID
		a.focusDraft.sourceName = v.Name
		a.focusDraft.baseline = v.Config.Clone()
		a.templateFromFocus = false
	}
	a.renderTemplates()
	showMessage(a.templateNotice, "模版已保存；已载入的本次配置保持原样。")
	if a.root != 0 {
		a.renderReady()
	}
}
func (a *application) reviewLegacyConfiguration() {
	s := a.controller.Snapshot()
	if s.Config != nil {
		return
	}
	a.ensureFocusDraft()
	cfg := a.focusDraft.config.Clone()
	cfg.Minutes = s.Minutes
	cfg.Scene = s.Scene
	cfg.Apps = nil
	for _, id := range s.AllowedIDs() {
		app, err := m.AppForID(id)
		if err != nil {
			for _, candidate := range a.store.Data.Preferences.Apps {
				if candidate.ID == id {
					if v, e := m.AppAt(candidate.Path); e == nil && v.ID == id {
						app = v
						err = nil
						break
					}
				}
			}
			if err != nil {
				a.openMain()
				v, e := m.ChooseApp()
				if errors.Is(e, m.ErrSelectionCancelled) {
					return
				}
				if e != nil || v.ID != id {
					a.fail(noticeInfo, "请选择旧会话中同一应用的新位置："+id, e)
					return
				}
				app = v
			}
		}
		cfg.Apps = append(cfg.Apps, app)
	}
	text := fmt.Sprintf("原应用范围、%d 分钟和场景 %s 保持。旧版未记录的效果将使用：背景 %t、声音 %t、音量 %d%%、入场 %t、人声 %t。历史注明这是恢复时补齐的配置。", cfg.Minutes, cfg.Scene, cfg.Background, cfg.Sound, int(cfg.Volume*100+.5), cfg.Entry, cfg.Voice)
	verb := "使用这些配置恢复"
	if _, ok := a.catalog.Scene(cfg.Scene); !ok {
		cfg.Background = false
		cfg.Sound = false
		cfg.Entry = false
		cfg.Voice = false
		text = fmt.Sprintf("原应用范围、%d 分钟和原场景 %s 保留。该场景尚无正式素材，本次恢复将关闭背景、环境声音、入场和人声，不播放其他场景。历史注明恢复时配置。", cfg.Minutes, cfg.Scene)
		verb = "关闭这些效果并恢复"
	}
	a.confirmAction("补齐旧会话的恢复配置？", text, verb, func() {
		if err := a.controller.CompleteLegacy(cfg); err != nil {
			a.fail(noticeInfo, err.Error(), nil)
			return
		}
		if a.persist() {
			a.resumeWithConsent()
		}
	})
}

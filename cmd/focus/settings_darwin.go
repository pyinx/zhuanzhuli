//go:build darwin

package main

import (
	"errors"
	"fmt"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"focus.local/zhuanzhuli/pkg/journal"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const environmentPreviewWidth, environmentPreviewHeight = 200.0, 130.0

func (a *application) openSettings()            { a.showPage(pageSettings) }
func (a *application) openFocusSettings()       { a.openSettingsSection(0) }
func (a *application) openApplicationSettings() { a.openSettingsSection(1) }
func (a *application) openSettingsSection(i int) {
	if a.mainModal() {
		return
	}
	a.selectSettingsSection(i)
	a.showPage(pageSettings)
}
func (a *application) selectSettingsSection(i int) {
	if a.settingsSection != i {
		a.stopAudition()
	}
	a.settingsSection = i
	if a.settingsTabs != 0 && a.settingsSections[0] != 0 {
		m.Send(a.settingsTabs, "setSelectedSegment:", int64(i))
		for j, pane := range a.settingsSections {
			m.Hidden(pane, j != i)
		}
	}
}
func (a *application) rememberNotificationDraft() {
	if a.profileField != 0 && a.notificationDraft != nil {
		a.notificationDraft.profile = m.Value(a.profileField)
		a.notificationDraft.linked = m.Checked(a.linkedField)
	}
}

func (a *application) openSettingsPane(pane int) {
	switch {
	case pane == 0:
		a.openMain()
	case pane == 1:
		a.openFocusSettings()
	default:
		a.openApplicationSettings()
	}
}
func (a *application) renderSettings() {
	a.rememberNotificationDraft()
	root := a.pageRoot(pageSettings)
	a.settingsRoot = root
	a.pageResponders[pageSettings] = 0
	if a.settingsSections[0] == 0 {
		a.settingsSections, a.settingsTabs = a.buildSectionTabs(root, []string{"专注设置", "应用设置"}, a.settingsSection, a.selectSettingsSection)
		a.buildFocusSettings(a.settingsSections[0])
	}
	root = a.settingsSections[1]
	position := m.Point{}
	if a.settingsScroll[2] != 0 {
		b := m.Bounds(m.Send(a.settingsScroll[2], "contentView"))
		position = m.Point{X: b.X, Y: b.Y}
	}
	a.resetSettingsReferences()
	m.ClearContent(root)
	scroll, r := m.ScrollStack(root)
	m.Edges(scroll, root, 0)
	a.settingsScroll[2] = scroll
	titleText(r, "应用设置", 22)
	titleText(r, "应用外观", 15)
	index := 0
	if a.store.Data.Preferences.Appearance == "dark" {
		index = 1
	}
	var control m.ID
	control = m.Segmented(r, []string{"浅色", "深色"}, index, m.R(0, 0, 250, 28), func(i int) { a.setAppearance([]string{"light", "dark"}[i]) })
	m.Arrange(r, control, false)
	a.appearanceControl = control
	a.appearanceNotice = message(r)
	bodyText(r, "仅改变"+appDisplayName+"的界面，选择会自动保存。", 12, true)
	m.Gap(r, 16)
	a.settingsNotifications(r)
	a.settingsNotice = a.notificationSaveNotice
	a.settingsNotices[2] = a.settingsNotice
	a.settingsErrors[2] = a.settingsErrorKey()
	m.RestoreScroll(scroll, position)
	m.KeyLoop(a.main.NativeHandle())
}

func copyFocusEffects(dst *session.Config, source session.Config) {
	dst.Sound, dst.Volume, dst.Ambience = source.Sound, source.Volume, source.Clone().Ambience
	dst.Entry, dst.Voice, dst.ReminderSound = source.Entry, source.Voice, source.ReminderSound
}

func (a *application) buildFocusSettings(root m.ID) {
	scroll, content := m.ScrollStack(root)
	m.Edges(scroll, root, 0)
	titleText(content, "专注设置", 22)
	bodyText(content, "沉浸模式用于下次专注。声音与入场是自定义专注的默认值，已保存模版沿用各自的配置。", 12, true)
	cfg := a.store.Data.Preferences.Config().IndependentAudio()
	f := &configForm{
		get:     func() *session.Config { return &cfg },
		changed: func() { a.focusEffectsDirty = true; a.quitDiscarded = false },
	}
	a.focusEffectsForm = f
	immersion := row(content, 12)
	titleText(immersion, "沉浸模式", 15)
	m.Flex(immersion)
	immersive := a.store.Data.Preferences.Immersion
	toggle := m.Switch(immersion, "沉浸模式", immersive, func(on bool) {
		immersive = on
		f.changed()
	})
	m.Arrange(immersion, toggle, false)
	bodyText(content, "将选中的工作窗口分别切入原生全屏。需要辅助功能权限；其他应用的通知仍按系统设置显示。", 12, true)
	action(content, "检查辅助功能权限…", func() {
		a.showConfigurationDetails("沉浸模式权限", func(r m.ID) {
			a.immersionPermissionView(r, "", nil, nil)
		})
	})
	m.Gap(content, 6)
	a.buildFocusEffects(content, f, true)
	f.notice = message(content)
	feedback := message(content)
	save := action(content, "保存专注设置", func() {
		next := a.store.Data.Preferences
		next.Immersion = immersive
		current := next.Config()
		copyFocusEffects(&current, cfg)
		next.SetConfig(current)
		if err := a.store.SetPreferences(next); err != nil {
			showMessage(feedback, preferenceSaveError)
			return
		}
		if a.focusDraft != nil && a.focusDraft.sourceID == "" {
			copyFocusEffects(&a.focusDraft.config, cfg)
			copyFocusEffects(&a.focusDraft.baseline, cfg)
			a.refreshConfigurationDirty()
		}
		a.focusEffectsDirty = false
		a.refreshReadySummary()
		showMessage(feedback, "已保存；沉浸模式用于下一次专注。")
	})
	m.Primary(save)
}
func (a *application) setAppearance(value string) {
	old := a.store.Data.Preferences.Appearance
	next := a.store.Data.Preferences
	next.Appearance = value
	if err := a.store.SetPreferences(next); err != nil {
		index := 0
		if old == "dark" {
			index = 1
		}
		m.Send(a.appearanceControl, "setSelectedSegment:", int64(index))
		showMessage(a.appearanceNotice, "外观未保存，请检查磁盘后重试。")
		return
	}
	if err := m.ApplyAppearance(value); err != nil {
		a.store.SetPreferences(func() store.Preferences { p := a.store.Data.Preferences; p.Appearance = old; return p }())
		m.ApplyAppearance(old)
		showMessage(a.appearanceNotice, err.Error())
		return
	}
	showMessage(a.appearanceNotice, "外观已保存。")
}
func (a *application) settingsErrorKey() string {
	key := a.mediaFailureCaption()
	if a.protectionFailure != nil {
		key += a.controller.Snapshot().Phase + "\n" + a.protectionFailure.Error()
	}
	return key
}
func (a *application) preparationFailureView(r m.ID) {
	if a.protectionFailure == nil || a.controller.Snapshot().Phase != "paused" {
		return
	}
	if immersionPermissionIssue(a.protectionFailure) {
		a.immersionPermissionView(r, "恢复沉浸专注", func() {
			a.acceptLimitedApps = false
			a.resumeWithConsent()
		}, a.resumeOrdinaryFocus)
		action(r, "结束本次", func() {
			a.detail.Close()
			a.end()
		})
		return
	}
	box, g := m.GroupStack(r, 8)
	m.Arrange(r, box, true)
	titleText(g, "本次环境准备未完成", 17)
	bodyText(g, "计时保持暂停。以下是实际失败对象与原因：", 12, true)
	for _, leaf := range protectionLeaves(a.protectionFailure) {
		bodyText(g, leaf.Error(), 13, false)
	}
	bodyText(g, "重试会重新核对；结束会恢复本应用已确认的改动。", 12, true)
	buttons := row(g, 8)
	b := action(buttons, "重新检查并恢复", func() { a.acceptLimitedApps = false; a.hideMain(); a.resumeWithConsent() })
	m.Primary(b)
	action(buttons, "结束本次", a.end)
	m.Flex(buttons)
	if a.controller.Snapshot().Immersion {
		action(g, "本次改用普通专注", a.resumeOrdinaryFocus)
	} else if consentedApplications(a.protectionFailure, a.applicationConsent()) {
		bodyText(g, "明确继续后，上述应用不会获得限制；其他可用保护继续，系统通知和声音仍可能出现。", 12, true)
		action(g, "接受以上应用限制缺失并恢复", func() { a.acceptLimitedApps = true; a.hideMain(); a.resumeWithConsent() })
	}
	m.Gap(r, 12)
}
func (a *application) acceptWorkApp(app store.App, err error) {
	a.acceptWorkAppAt(app, err, a.settingsNotice)
}
func (a *application) acceptWorkAppAt(app store.App, err error, notice m.ID) {
	if errors.Is(err, m.ErrSelectionCancelled) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "choose work app:", err)
		showMessage(notice, "无法读取该应用。请选择有效的 .app 应用后重试。")
		return
	}
	if a.fixtureRoot != "" && !a.ownFixture(journal.Ref{BundleID: app.ID, Path: app.Path}) {
		showMessage(notice, "隔离验收只能选择本轮 Fixture A／B。")
		return
	}
	next, lastAdded := mergeWorkApps(a.store.Data.Preferences.Apps, []store.App{app})
	if !slices.Equal(next, a.store.Data.Preferences.Apps) && !a.updatePreferences(func(p *store.Preferences) { p.Apps = next }) {
		showMessage(notice, preferenceSaveError)
		return
	}
	if a.appList != nil {
		a.appList.Reload(lastAdded)
	}
	if lastAdded < 0 {
		showMessage(notice, "此应用已在默认列表中，位置已核对。")
	}
	a.refreshReadySummary()
}
func (a *application) refreshAudition() {
	if a.previewVideo.Player != 0 {
		ready, err := a.previewVideo.State()
		if err == nil && !ready && !a.previewAt.IsZero() && time.Since(a.previewAt) >= 6*time.Second {
			err = errors.New("入场预览加载超时")
		}
		if err != nil {
			sceneID := a.previewSceneID
			a.stopAudition()
			a.recordMediaFailure(sceneID, ".mp4", err)
			a.auditionError = "入场预览未能播放，请重新读取资源后重试。"
		} else if a.previewVideo.Completed {
			a.stopAudition()
		}
	}
	if a.controller == nil {
		return
	}
	phase := a.controller.Snapshot().Phase
	available := phase != "entry" && !session.Working(phase)
	for _, f := range []*configForm{a.focusForm, a.templateForm, a.focusEffectsForm} {
		if f == nil || f.auditionButton == 0 {
			continue
		}
		label := "试听当前组合 5 秒"
		if (a.audition.Player != 0 || a.auditionNature.Player != 0) && a.auditionNotice == f.notice {
			label = "停止试听"
		}
		m.Send(f.auditionButton, "setTitle:", m.NS(label))
		m.Enabled(f.auditionButton, available)
	}
	if !available {
		showMessage(a.auditionNotice, "暂停专注后可试听。")
	} else if a.auditionError != "" {
		showMessage(a.auditionNotice, a.auditionError)
	} else if a.audition.Player != 0 || a.auditionNature.Player != 0 {
		if a.audition.Player != 0 && m.Str(m.Send(m.Send(a.audition.Player, "url"), "path")) == filepath.Join(a.resources, a.catalog.Guide.Path) {
			showMessage(a.auditionNotice, "正在预听入场人声。")
		} else {
			showMessage(a.auditionNotice, "正在试听当前组合，再次点击可停止。")
		}
	} else {
		showMessage(a.auditionNotice, "")
	}
}
func (a *application) stopAudition() {
	a.auditionError = ""
	a.auditionGeneration++
	if a.auditionTimer != nil {
		a.auditionTimer.Stop()
		a.auditionTimer = nil
	}
	a.audition.Stop()
	a.auditionNature.Stop()
	a.previewVideo.Stop()
	a.previewSceneID, a.previewAt = "", time.Time{}
	if a.previewWindow != nil {
		w := a.previewWindow
		a.previewWindow = nil
		w.Close()
	}
	showMessage(a.auditionNotice, "")
	a.refreshAudition()
}
func (a *application) draftChanged() {
	a.rememberNotificationDraft()
	if a.draftNotice == 0 || a.notificationDraft == nil {
		return
	}
	p := a.store.Data.Preferences
	text := ""
	if a.notificationDraft.profile != p.FocusProfile || a.notificationDraft.linked != p.FocusLinked {
		text = "有未保存的更改。保存只更新配置，不开启系统模式。"
		showMessage(a.settingsNotice, "")
	}
	showMessage(a.draftNotice, text)
}
func (a *application) settingsNotifications(r m.ID) {
	titleText(r, "通知", 20)
	bodyText(r, "系统专注模式联动是实验性功能，通知效果仍待验证。", 12, true)
	box, state := m.GroupStack(r, 8)
	m.Arrange(r, box, true)
	titleText(state, "当前状态", 13)
	a.notificationStatuses[0] = bodyText(state, a.notificationCaption(), 13, false)
	permission := bodyText(state, "", 12, true)
	m.Send(permission, "setAccessibilityLabel:", m.NS("当前版本辅助功能权限"))
	checkPermission := func() {
		trusted, err := m.FocusAccess()
		m.Text(permission, notificationPermissionCaption(trusted, err))
	}
	checkPermission()
	links := row(state, 10)
	action(links, "辅助功能权限…", m.OpenAccessibilitySettings)
	action(links, "重新检查权限", checkPermission)
	m.Flex(links)
	p := a.store.Data.Preferences
	if a.notificationDraft == nil {
		a.notificationDraft = &notificationDraft{p.FocusProfile, p.FocusLinked}
	}
	box, g := m.GroupStack(r, 10)
	m.Arrange(r, box, true)
	m.Hidden(box, true)
	titleText(g, "联动配置", 13)
	titleText(g, "系统专注模式名称", 13)
	a.profileField = field(g, a.notificationDraft.profile, "例如，专注力", "系统专注模式名称，须与控制中心完全一致")
	m.ObserveText(a.profileField, func() { a.quitDiscarded = false; a.draftChanged() })
	a.linkedField = m.Check(g, "开始与恢复时自动联动", a.notificationDraft.linked, m.R(0, 0, 400, 26), func(bool) { a.quitDiscarded = false; a.draftChanged() })
	m.Arrange(g, a.linkedField, true)
	bodyText(g, "保存后用于下次开始或恢复，不会立即开启系统模式。", 12, true)
	a.draftNotice = message(g)
	a.settingsNotice = message(g)
	a.notificationSaveNotice = a.settingsNotice
	saveRow := row(g, 8)
	action(saveRow, "系统专注设置…", m.OpenSettings)
	m.Flex(saveRow)
	save := action(saveRow, "保存配置", a.saveNotificationDraft)
	m.Primary(save)
	a.draftChanged()
	bodyText(r, "仅覆盖系统 Focus 可控制的标准通知。关键警报、部分系统／来电及自绘提醒除外；联动需要辅助功能权限。", 12, true)
}
func (a *application) saveNotificationDraft() {
	a.rememberNotificationDraft()
	draft := a.notificationDraft
	if draft == nil {
		return
	}
	name := strings.TrimSpace(draft.profile)
	if strings.ContainsRune(name, 0) || utf8.RuneCountInString(name) > 80 || (draft.linked && name == "") {
		showMessage(a.settingsNotice, "请输入 1–80 字、与控制中心完全一致的模式名称。")
		m.Send(m.ID(a.main.NativeHandle()), "makeFirstResponder:", a.profileField)
		return
	}
	if !a.updatePreferences(func(p *store.Preferences) { p.FocusLinked = draft.linked; p.FocusProfile = name }) {
		return
	}
	draft.profile = name
	m.Text(a.profileField, name)
	a.draftChanged()
	showMessage(a.settingsNotice, "配置已保存；通知效果仍待验证。")
}
func (a *application) resetSettingsReferences() {
	a.profileField = 0
	a.linkedField = 0
	a.draftNotice = 0
	a.notificationSaveNotice = 0
	a.notificationStatuses = [2]m.ID{}
}

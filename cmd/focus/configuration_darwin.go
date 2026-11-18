//go:build darwin

package main

import (
	"errors"
	"fmt"
	"focus.local/zhuanzhuli/internal/media"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"github.com/egoist/mygo"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type focusDraft struct {
	config, baseline     session.Config
	sourceID, sourceName string
	duration             durationEdit
}
type durationEdit struct {
	invalid string
	last    int
}
type configForm struct {
	isFocus                                 bool
	grid                                    *m.SceneGrid
	auditionButton                          m.ID
	scenePicker, scenePreview, sceneCaption m.ID
	sceneIDs                                []string
	refreshScene                            func()
	repair                                  m.ID
	onClose                                 func()
	mediaGroup, mediaCaption                m.ID
	apps                                    *m.AppList
	minutes, notice, empty, volumeCaption   m.ID
	minuteSlider                            m.ID
	get                                     func() *session.Config
	changed                                 func()
}
type featureState struct {
	historySection                                                                                int
	historyReportRoot                                                                             m.ID
	settingsSection                                                                               int
	settingsSections                                                                              [2]m.ID
	settingsTabs                                                                                  m.ID
	focusEffectsForm                                                                              *configForm
	refreshFocusSettings                                                                          func()
	report                                                                                        focusReport
	templateDurationOwner                                                                         *store.Template
	templateDuration                                                                              durationEdit
	quitPrompt                                                                                    bool
	windowSizing                                                                                  map[string]any
	notificationSaveNotice, templatePicker                                                        m.ID
	quitDiscarded                                                                                 bool
	focusDraft                                                                                    *focusDraft
	focusForm, templateForm                                                                       *configForm
	templateDraft                                                                                 *store.Template
	templateBaseline                                                                              *store.Template
	templateRoot, templateName, templateNotice, focusDirty, templatePopup                         m.ID
	templateFromFocus                                                                             bool
	pickerTarget                                                                                  *configForm
	appearanceControl, appearanceNotice                                                           m.ID
	detail                                                                                        *mygo.Window
	historyQuery, historyFrom, historyTo                                                          string
	historyPage                                                                                   int
	historyFiltered                                                                               []session.State
	historySearch, historyFromField, historyToField, historyPageCaption, historyPrev, historyNext m.ID
	historyEmpty                                                                                  m.ID
	historyFromPicker, historyToPicker                                                            *m.DateFilter
}

func (a *application) ensureFocusDraft() {
	if a.focusDraft == nil {
		c := a.store.Data.Preferences.Config()
		a.focusDraft = &focusDraft{config: c, baseline: c.Clone()}
	}
}
func (a *application) focusConfiguration() session.Config {
	a.ensureFocusDraft()
	if a.minutes != 0 {
		n, err := strconv.Atoi(strings.TrimSpace(m.Value(a.minutes)))
		if err == nil {
			a.focusDraft.config.Minutes = n
		} else {
			a.focusDraft.config.Minutes = 0
		}
	}
	return a.focusDraft.config.Clone()
}
func (a *application) effectiveConfig() session.Config {
	s := a.controller.Snapshot()
	if session.Exists(s.Phase) {
		if s.Config != nil {
			c := s.Config.Clone()
			c.Apps = append(c.Apps, s.TemporaryApps...)
			return c
		}
		c := a.store.Data.Preferences.Config()
		c.Minutes = s.Minutes
		c.Scene = s.Scene
		return c
	}
	a.ensureFocusDraft()
	return a.focusDraft.config.Clone()
}
func (a *application) focusIsDirty() bool {
	return a.focusDraft != nil && !reflect.DeepEqual(a.focusDraft.config, a.focusDraft.baseline)
}
func (a *application) refreshConfigurationDirty() {
	if a.focusDirty != 0 {
		text := "本次调整不会自动保存到模版。"
		if a.focusIsDirty() {
			text = "本次已调整 · 明确保存才更新模版"
		}
		m.Text(a.focusDirty, text)
	}
}
func (a *application) confirmAction(title, detail, verb string, done func()) {
	a.ensureMain()
	if a.mainModal() {
		return
	}
	a.main.Show()
	a.main.Focus()
	sheet, err := m.NewAlertSheet(a.main.NativeHandle(), title, detail, []string{"取消", verb}, func(i int) {
		a.unknownSheet = nil
		a.refreshNavigation()
		if i == 1 {
			done()
		}
	})
	if err != nil {
		a.fail(noticeInfo, "无法打开确认，请稍后重试。", err)
		return
	}
	sheet.CancelFirst()
	a.unknownSheet = sheet
	a.refreshNavigation()
}
func (a *application) selectFocusTemplate(index int) {
	apply := func() {
		goal := m.Value(a.goal)
		if index == 0 {
			a.focusDraft.sourceID = ""
			a.focusDraft.sourceName = ""
		} else if index-1 < len(a.store.Data.Templates) {
			v := a.store.Data.Templates[index-1]
			a.focusDraft = &focusDraft{config: v.Config.Clone(), baseline: v.Config.Clone(), sourceID: v.ID, sourceName: v.Name}
		}
		a.draftGoal = goal
		a.renderReady()
	}
	if index == 0 {
		apply()
		return
	}
	if a.focusIsDirty() {
		m.Send(a.templatePopup, "selectItemAtIndex:", int64(a.focusTemplateIndex()))
		a.confirmAction("替换本次配置？", "本次未保存的配置会被所选模版替换，目标会保留。", "替换本次配置", apply)
	} else {
		apply()
	}
}
func (a *application) focusTemplateIndex() int {
	if a.focusDraft != nil {
		for i, v := range a.store.Data.Templates {
			if v.ID == a.focusDraft.sourceID {
				return i + 1
			}
		}
	}
	return 0
}
func (a *application) saveFocusTemplate() {
	a.focusConfiguration()
	for _, v := range a.store.Data.Templates {
		if v.ID == a.focusDraft.sourceID {
			v.Config = a.focusDraft.config.Clone()
			saved, err := a.store.SaveTemplate(v)
			if err != nil {
				showMessage(a.appsError, "模版未保存："+err.Error())
				return
			}
			a.focusDraft.baseline = saved.Config.Clone()
			a.refreshConfigurationDirty()
			showMessage(a.appsError, "模版已更新。")
			return
		}
	}
	a.saveFocusAsTemplate()
}
func (a *application) saveFocusAsTemplate() {
	save := func() {
		a.focusConfiguration()
		a.templateDraft = &store.Template{Config: a.focusDraft.config.Clone()}
		a.templateBaseline = nil
		a.templateFromFocus = true
		a.templateRoot = 0
		a.showPage(pageTemplates)
	}
	if a.templateIsDirty() {
		a.confirmAction("替换未保存的模版草稿？", "原草稿尚未保存。取消后可先回模版页保存。", "放弃原草稿并另存", save)
	} else {
		save()
	}
}

func (f *configForm) close() {
	if f != nil && f.apps != nil {
		if f.grid != nil {
			f.grid.Close()
		}
		if f.onClose != nil {
			f.onClose()
		}
		f.apps.Close()
		f.apps = nil
		f.mediaGroup = 0
		f.mediaCaption = 0
	}
}
func (f *configForm) reloadApps() {
	f.apps.Reload(-1)
	m.Hidden(f.empty, len(f.get().Apps) > 0)
	m.Hidden(f.apps.View, len(f.get().Apps) == 0)
	if f.repair != 0 {
		m.Hidden(f.repair, len(f.get().Apps) == 0)
		m.Enabled(f.repair, f.apps.Selected() >= 0)
	}
	f.changed()
}
func (a *application) buildConfigForm(root m.ID, get func() *session.Config, edit *durationEdit, changed func(), focus bool) *configForm {
	f := &configForm{get: get, changed: changed, isFocus: focus}
	c := get()
	titleText(root, "专注时长", 13)
	duration := row(root, 8)
	errorLabel := message(root)
	initial := strconv.Itoa(c.Minutes)
	if c.Minutes >= 1 && c.Minutes <= 180 {
		edit.last = c.Minutes
	} else if edit.last != 0 {
		initial = edit.invalid
	} else {
		edit.last = 1
		initial = ""
	}
	setMinutes := func(n int) {
		get().Minutes = n
		edit.last = n
		edit.invalid = ""
		m.Text(f.minutes, strconv.Itoa(n))
		showMessage(errorLabel, "")
		changed()
	}
	marks := []int{5, 15, 30, 60}
	track := m.Stack(duration, true, 0)
	m.Arrange(duration, track, false)
	slider, ruler := m.MinuteSlider(track, edit.last, marks, setMinutes)
	f.minuteSlider = slider
	m.Arrange(track, slider, true)
	m.Arrange(track, ruler, true)
	m.Minimum(track, "width", 90)
	m.Fixed(slider, "height", 24)
	m.Fixed(ruler, "height", 6)
	f.minutes = m.Field(duration, initial, "1–180", m.R(0, 0, 60, 30))
	m.Arrange(duration, f.minutes, false)
	m.Fixed(f.minutes, "height", 30)
	m.Send(f.minutes, "setAccessibilityLabel:", m.NS("专注时长，1 到 180 分钟"))
	m.Fixed(f.minutes, "width", 60)
	bodyText(duration, "分钟", 12, true)
	m.Send(slider, "setContentHuggingPriority:forOrientation:", float32(249), int64(0))
	m.Send(track, "setContentHuggingPriority:forOrientation:", float32(249), int64(0))
	ticks := row(root, 6)
	bodyText(ticks, "常用刻度", 12, true)
	for _, n := range marks {
		action(ticks, fmt.Sprintf("%d 分", n), func() {
			m.Send(slider, "setIntegerValue:", int64(n))
			setMinutes(n)
		})
	}
	m.Flex(ticks)
	if c.Minutes < 1 || c.Minutes > 180 {
		showMessage(errorLabel, "请输入 1–180 的整数分钟。")
	}
	m.ObserveText(f.minutes, func() {
		input := m.Value(f.minutes)
		n, err := strconv.Atoi(strings.TrimSpace(input))
		if err != nil || n < 1 || n > 180 {
			get().Minutes = 0
			edit.invalid = input
			showMessage(errorLabel, "请输入 1–180 的整数分钟。")
		} else {
			get().Minutes = n
			edit.last = n
			edit.invalid = ""
			m.Send(slider, "setIntegerValue:", int64(n))
			showMessage(errorLabel, "")
		}
		changed()
	})
	if focus {
		a.minutes = f.minutes
		a.minuteError = errorLabel
	}
	titleText(root, "专注应用", 13)
	listRoot := m.Stack(root, true, 8)
	m.Arrange(root, listRoot, true)
	visibleRows := m.AppVisibleRows
	if focus {
		visibleRows = 5
	}
	f.apps = m.NewAppList(listRoot, func() []store.App { return get().Apps }, func(i int) {
		if f.repair != 0 {
			m.Enabled(f.repair, i >= 0)
		}
	}, visibleRows)
	m.Arrange(listRoot, f.apps.View, true)
	f.apps.SetRemoveHandler(func(id string) {
		apps := []session.App{}
		for _, v := range get().Apps {
			if v.ID != id {
				apps = append(apps, v)
			}
		}
		get().Apps = apps
		f.reloadApps()
	})
	f.empty = bodyText(listRoot, "尚未选择专注应用。", 12, true)
	m.Hidden(f.empty, len(c.Apps) > 0)
	m.Hidden(f.apps.View, len(c.Apps) == 0)
	controls := row(listRoot, 8)
	action(controls, "添加应用…", func() { a.openApplicationPicker(f) })
	f.repair = action(controls, "更新应用位置…", func() {
		i := f.apps.Selected()
		if i < 0 || i >= len(get().Apps) {
			showMessage(f.notice, "请先选中要更新位置的应用。")
			return
		}
		id := get().Apps[i].ID
		v, err := m.ChooseApp()
		if errors.Is(err, m.ErrSelectionCancelled) {
			return
		}
		if err != nil {
			showMessage(f.notice, "无法读取应用："+err.Error())
			return
		}
		if v.ID != id {
			showMessage(f.notice, "请选择同一应用的新位置；其他应用请用添加。")
			return
		}
		get().Apps[i] = v
		f.reloadApps()
	})
	m.Hidden(f.repair, len(c.Apps) == 0)
	m.Enabled(f.repair, false)
	m.Flex(controls)
	a.buildEnvironmentControls(root, f)
	return f
}

// auditionConfig shares the existing short-lived player and generation, not session audio.
func (a *application) auditionConfig(c session.Config, notice m.ID) {
	if session.Working(a.controller.Snapshot().Phase) || a.controller.Snapshot().Phase == "entry" {
		showMessage(notice, "暂停专注后可试听。")
		return
	}
	if a.audition.Player != 0 || a.auditionNature.Player != 0 {
		a.stopAudition()
		showMessage(notice, "")
		return
	}
	a.stopAudition()
	a.auditionNotice = notice
	c = c.IndependentAudio()
	if c.Sound && c.Volume > 0 {
		if err := a.audition.PlayOnce(a.mediaFor(c.Scene, ".wav"), c.Volume); err != nil {
			a.recordMediaFailure(c.Scene, ".wav", err)
			a.auditionError = media.SceneName(c.Scene) + "的背景音乐无法试听，请重新读取资源。"
			showMessage(notice, a.auditionError)
			return
		}
	}
	path, _, err := a.ambiencePath(c)
	if err == nil && path != "" {
		err = a.auditionNature.PlayOnce(path, c.Ambience.Volume)
	}
	if err != nil {
		a.audition.Stop()
		a.auditionNature.Stop()
		a.auditionError = "所选白噪音无法试听，请重新读取资源。"
		showMessage(notice, a.auditionError)
		return
	}
	if a.audition.Player == 0 && a.auditionNature.Player == 0 {
		a.auditionError = "当前组合为静音。"
		showMessage(notice, a.auditionError)
		return
	}
	a.scheduleAuditionStop()
	showMessage(notice, "正在试听 5 秒，再次点击可停止。")
}
func (a *application) validateFocusConfig(c session.Config, checkAppPaths bool) error {
	if err := c.Validate(true); err != nil {
		return err
	}
	if checkAppPaths {
		for _, v := range c.Apps {
			if app, err := m.AppAt(v.Path); err != nil || app.ID != v.ID {
				return fmt.Errorf("无法确认 %s 的应用位置，请更新位置或移除后重试", v.Name)
			}
		}
	}
	if _, ok := a.catalog.Scene(c.Scene); !ok {
		return fmt.Errorf("%s暂无正式素材，请重新选择自然环境", media.SceneName(c.Scene))
	}
	return nil
}

func (a *application) scheduleAuditionStop() {
	generation := a.auditionGeneration
	a.auditionTimer = time.AfterFunc(5*time.Second, func() {
		mygo.RunOnMain(func() {
			if generation == a.auditionGeneration {
				a.stopAudition()
			}
		})
	})
}
func (a *application) notificationDraftDirty() bool {
	a.rememberNotificationDraft()
	return a.notificationDraft != nil && (a.notificationDraft.profile != a.store.Data.Preferences.FocusProfile || a.notificationDraft.linked != a.store.Data.Preferences.FocusLinked)
}
func (a *application) showConfigurationDetails(title string, build func(m.ID)) {
	if a.mainModal() {
		return
	}
	a.ensureMain()
	a.main.Show()
	w := window(title, 560, 540, false)
	a.detail = w
	root := m.FixedSurface(w.NativeHandle(), 560, 540, false)
	r, footer, _ := contentFooter(root)
	build(r)
	close := action(footer, "关闭", w.Close)
	m.Send(close, "setKeyEquivalent:", m.NS("\x1b"))
	handle := w.NativeHandle()
	w.OnClose(func(*mygo.CloseEvent) { m.EndSheet(handle) })
	w.OnClosed(func() {
		m.EndSheet(handle)
		m.UnbindTree(root)
		if a.detail == w {
			a.detail = nil
		}
		a.refreshNavigation()
	})
	if !m.BeginSheet(a.main.NativeHandle(), handle) {
		w.Close()
		return
	}
	a.refreshNavigation()
	m.KeyLoop(handle)
}

func (a *application) refreshImmersionPermission() {
	if a.mainPage == pageSettings && a.main != nil && a.main.IsVisible() {
		if a.settingsSection == 0 && a.inlinePermissionRefresh != nil {
			a.inlinePermissionRefresh(false)
		} else if a.settingsSection == 1 {
			a.refreshNotificationPermission()
		}
	}
}

func immersionPermissionStatus(err error) (text, color string) {
	switch {
	case err == nil:
		return "正在运行的归径已获辅助功能权限。", "systemGreenColor"
	case errors.Is(err, errImmersionPermission):
		return "正在运行的归径未获辅助功能权限。", "systemRedColor"
	default:
		return "系统的辅助功能查询组件未能返回状态，无法判断是否已授权。请重新检查。", "labelColor"
	}
}

func (a *application) immersionPermissionCheck(initialErr error, update func(error)) func(bool) {
	lastReadFailed := errors.Is(initialErr, errImmersionPermissionCheck)
	return func(explicit bool) {
		err := a.checkImmersionAccess()
		readFailed := errors.Is(err, errImmersionPermissionCheck)
		if readFailed && (explicit || !lastReadFailed) {
			a.debugEvent(debugAccessFailed, a.debugAttempt, "", 0)
		}
		lastReadFailed = readFailed
		update(err)
	}
}

func (a *application) mediaFailureControls(root m.ID) (m.ID, m.ID) {
	return a.mediaFailureControlsFor(root, func() string { return a.effectiveConfig().Scene })
}
func (a *application) mediaFailureControlsFor(root m.ID, sceneID func() string) (m.ID, m.ID) {
	group := m.Stack(root, true, 8)
	m.Arrange(root, group, true)
	caption := bodyText(group, a.mediaFailureCaptionFor(sceneID()), 12, true)
	action(group, "重新读取正式资源", func() {
		a.retrySceneMedia(sceneID())
		text := a.mediaFailureCaptionFor(sceneID())
		if text == "" {
			text = "正式资源已重新读取。"
		}
		m.Text(caption, text)
	})
	bodyText(group, "可在本次配置中关闭对应效果，或重新安装完整应用。", 12, true)
	m.Hidden(group, a.mediaFailureCaptionFor(sceneID()) == "")
	return group, caption
}

func (a *application) retrySceneMedia(sceneID string) {
	for _, role := range mediaRoles {
		if role.ext == ".nature" || role.ext == ".guide" {
			continue
		}
		path := a.mediaFor(sceneID, role.ext)
		if path != "" && role.ext == ".wav" {
			a.recordMediaFailure(sceneID, role.ext, m.ValidateAudio(path))
		}
	}
	cfg := a.effectiveConfig().IndependentAudio()
	if !session.Exists(a.controller.Snapshot().Phase) {
		for _, f := range []*configForm{a.focusForm, a.templateForm} {
			if f != nil && f.apps != nil && ((a.mainPage == pageFocus && f == a.focusForm) || (a.mainPage == pageTemplates && f == a.templateForm && a.templateDraft != nil)) && f.get().Scene == sceneID {
				cfg = f.get().IndependentAudio()
			}
		}
	}
	if cfg.Scene == sceneID {
		path, _, err := a.ambiencePath(cfg)
		if err == nil && path != "" {
			err = m.ValidateAudio(path)
		}
		a.recordMediaFailure(sceneID, ".nature", err)
	}
	guidePath, guideErr := media.Resolve(a.resources, a.catalog.Guide)
	if guideErr == nil {
		guideErr = m.ValidateAudio(guidePath)
	}
	a.recordMediaFailure(sceneID, ".guide", guideErr)
	s := a.controller.Snapshot()
	if a.effectiveConfig().Scene == sceneID && (s.Phase == "entry" || session.Working(s.Phase)) {
		a.backgroundsOn()
		a.environmentSound()
	}
	for _, form := range []*configForm{a.focusForm, a.templateForm} {
		if form != nil && form.apps != nil && form.get().Scene == sceneID && form.refreshScene != nil {
			form.refreshScene()
		}
	}
	if a.notice.Kind == noticeMedia && a.effectiveConfig().Scene == sceneID && a.mediaFailureCaptionFor(sceneID) == "" {
		a.notice = Notice{}
	}
	a.refresh()
}
func (a *application) openFailureDetails() {
	a.showConfigurationDetails("本次专注问题", func(r m.ID) {
		if a.protectionFailure != nil {
			a.preparationFailureView(r)
		}
		if a.mediaFailureCaption() != "" {
			a.mediaFailureControls(r)
			action(r, "结束本次并调整配置", func() {
				if a.detail != nil {
					a.detail.Close()
				}
				a.end()
			})
		}
	})
}

func (a *application) resumeEntryPresentation() {
	if a.controller.Snapshot().Phase != "entry" || a.busy {
		return
	}
	if !a.countdownAt.IsZero() {
		a.advanceCountdown(a.countdownAt)
	} else if a.entryAt.IsZero() {
		a.beginEntry()
	} else {
		a.openEntry()
	}
}

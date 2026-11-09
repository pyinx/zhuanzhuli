//go:build darwin

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"focus.local/zhuanzhuli/internal/media"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/ebitengine/purego/objc"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Native construction/regression through real AppKit controls. Never a visual verdict.
func (a *application) nativeCheck(uiChecks map[string]any) {
	result := map[string]any{"evidenceKind": "native-construction", "buildID": buildID, "goarch": runtime.GOARCH, "visualVerification": false}
	failures := []string{}
	check := func(name string, value bool) {
		result[name] = value
		if !value {
			failures = append(failures, name)
		}
	}
	for key, value := range uiChecks {
		result[key] = value
	}
	handle := a.main.NativeHandle()
	m.Send(a.shell, "layoutSubtreeIfNeeded")
	initial, _ := mainWindowSizes(m.VisibleContentSize(handle))
	b := m.Bounds(m.Send(m.ID(handle), "contentView"))
	check("defaultWindowSize", abs(b.W-initial.X) < 1 && abs(b.H-initial.Y) < 1)
	check("fourPageNavigation", len(pageRows) == 4 && pageCount == 4)
	check("startTitleAndBoundary", m.Str(m.Send(a.primary, "title")) == "开始" && nativeButton(a.root, "以部分保护开始") == 0)
	check("nativeFields", objc.Send[bool](a.goal, m.S("isKindOfClass:"), m.Class("NSTextField")))
	// Exercise real image-button actions and file-backed players at an inaudible
	// level. Separately verify that zero-volume configurations allocate no player.
	initialConfig := a.focusDraft.config.Clone()
	selectScene := func(f *configForm, index int) {
		m.Send(f.grid.Buttons[index], "performClick:", m.ID(0))
	}
	sceneSelections := len(a.catalog.Scenes) == 6
	for index, scene := range a.catalog.Scenes {
		selectScene(a.focusForm, index)
		sceneSelections = sceneSelections && a.focusDraft.config.Scene == scene.ID && strings.Contains(m.Value(a.focusForm.sceneCaption), scene.Name) && m.Send(a.focusForm.scenePreview, "image") != 0
		quiet := a.focusDraft.config.Clone()
		quiet.Sound, quiet.Volume = true, .000001
		quiet.Ambience = &session.AmbienceConfig{Mode: "off"}
		a.auditionConfig(quiet, a.focusForm.notice)
		path := m.Str(m.Send(m.Send(a.audition.Player, "url"), "path"))
		sceneSelections = sceneSelections && a.audition.Player != 0 && path == a.mediaFor(scene.ID, ".wav")
		a.stopAudition()
	}
	check("natureSceneSelectionAndAudition", sceneSelections)
	independent := true
	for _, music := range []bool{false, true} {
		for _, mode := range []string{"off", "rain", "birds", "waves", "stream", "auto"} {
			for _, volume := range []float64{0, .000001} {
				cfg := initialConfig.IndependentAudio()
				cfg.Sound, cfg.Volume = music, volume
				cfg.Ambience = &session.AmbienceConfig{Mode: mode, Volume: volume}
				a.auditionConfig(cfg, a.focusForm.notice)
				independent = independent && (a.audition.Player != 0) == (music && volume > 0) && (a.auditionNature.Player != 0) == (mode != "off" && volume > 0)
				a.stopAudition()
			}
		}
	}
	check("independentAudioCombinationsAndZeroVolumes", independent)
	a.focusDraft.config = initialConfig.Clone()
	a.focusForm.refreshScene()
	oldTemplate, oldBaseline := a.templateDraft, a.templateBaseline
	a.templateDraft = &store.Template{Config: initialConfig.Clone()}
	a.templateDraft.Config.Scene = "pond"
	a.showPage(pageTemplates)
	check("natureDraftScenesIndependent", a.templateForm != nil && strings.Contains(m.Value(a.templateForm.sceneCaption), "池塘") && a.focusDraft.config.Scene == initialConfig.Scene)
	a.templateDraft, a.templateBaseline = nil, nil
	a.renderTemplates()
	create := nativeButton(a.templateRoot, "新建模版")
	m.Send(create, "performClick:", m.ID(0))
	cancel := nativeButton(a.templateRoot, "取消修改")
	m.Send(cancel, "performClick:", m.ID(0))
	a.refreshAudition() // The next UI tick must not touch the disposed form.
	check("newTemplateCancelKeepsPageAlive", create != 0 && cancel != 0 && a.templateDraft == nil && a.templateForm == nil && nativeButton(a.templateRoot, "新建模版") != 0)
	a.templateDraft, a.templateBaseline = oldTemplate, oldBaseline
	a.renderTemplates()
	a.showPage(pageFocus)
	quietConfig := initialConfig.Clone()
	quietConfig.Sound, quietConfig.Volume = true, .000001
	quietConfig.Ambience = &session.AmbienceConfig{Mode: "birds", Volume: .000001}
	quietConfig.Apps = []store.App{{ID: "native.editor", Path: "/Applications/Editor.app"}}
	if err := a.controller.Start(session.State{ID: "native-scene-audio", Goal: "声音连续性", Config: &quietConfig, Partial: true}); err == nil {
		a.environmentSound()
		player := a.audio.Player
		nature := a.nature.Player
		a.controller.Enter()
		a.environmentSound()
		check("natureAudioSurvivesEntry", player != 0 && player == a.audio.Player && nature != 0 && nature == a.nature.Player && a.audioScene == quietConfig.Scene)
		a.controller.Pause()
		a.environmentSound()
		check("naturePausedAudioStops", a.audio.Player == 0 && a.nature.Player == 0 && a.audioSession == "")
		a.controller.End()
	} else {
		check("natureAudioSurvivesEntry", false)
		check("naturePausedAudioStops", false)
	}
	check("legacySceneNamesPreserved", media.SceneName("snow") == "雪地" && media.SceneName("snow-mountain") == "雪山")
	a.nativeMediaRecoveryChecks(check)
	m.Text(a.goal, "切页保留的目标")
	a.draftGoal = m.Value(a.goal)
	beforeAudition := a.focusDraft.config.Clone()
	silent := beforeAudition.Clone()
	silent.Sound, silent.Volume = true, .000001
	silent.Ambience = &session.AmbienceConfig{Mode: "off"}
	a.auditionConfig(silent, a.focusForm.notice)
	playing := a.audition.Player != 0 && objc.Send[bool](a.audition.Player, m.S("isPlaying"))
	a.auditionConfig(silent, a.focusForm.notice)
	check("auditionStartStopPreservesConfiguration", playing && a.audition.Player == 0 && reflect.DeepEqual(beforeAudition, a.focusDraft.config) && m.Value(a.focusForm.notice) == "")
	a.focusDraft.config.Volume = .17
	a.focusDraft.config.Minutes = 45
	m.Text(a.minutes, "45")
	for _, page := range []mainPage{pageTemplates, pageHistory, pageSettings, pageFocus} {
		a.showPage(page)
	}
	check("oneWindowAndDrafts", a.main.NativeHandle() == handle && m.Value(a.goal) == "切页保留的目标" && a.focusDraft.config.Volume == .17 && m.Value(a.minutes) == "45")
	cfg := a.focusDraft.config.Clone()
	cfg.Apps = []store.App{{ID: "native.editor", Name: "很长的专注应用名称用于原生行宽度和图标对齐检查", Path: "/nonexistent/NativeEditor.app"}, {ID: "native.other", Name: "另一个应用", Path: "/nonexistent/Other.app"}}
	saved, err := a.store.SaveTemplate(store.Template{Name: "原生模版回归", Config: cfg})
	check("templateDurableSave", err == nil)
	if err == nil {
		a.selectFocusTemplate(0)
		a.focusDraft.config = a.focusDraft.baseline.Clone()
		a.selectFocusTemplate(1)
		check("templateSelectionLoadsConfig", a.focusDraft.sourceID == saved.ID && a.focusDraft.config.Volume == cfg.Volume && m.Value(a.goal) == "切页保留的目标")
		a.focusDraft.config.Volume = .4
		check("draftDoesNotWriteTemplate", a.store.Data.Templates[0].Config.Volume == .17)
		a.saveFocusTemplate()
		check("explicitTemplateUpdate", a.store.Data.Templates[0].Config.Volume == .4)
		a.renderReady()
		m.Send(a.root, "layoutSubtreeIfNeeded")
		icon, text, ok := a.focusForm.apps.RowGeometry(0)
		check("appRowsCentered", ok && abs((icon.Y+icon.H/2)-(text.Y+text.H/2)) < 1)
		check("appSelectionFitsViewport", a.focusForm.apps.RowFitsViewport(0))
		remove := nativeButton(a.root, "移除")
		if remove != 0 {
			m.Send(remove, "performClick:", m.ID(0))
		}
		check("rowRemovalUsesIdentity", len(a.focusDraft.config.Apps) == 1 && a.focusDraft.config.Apps[0].ID == "native.other")
	}
	a.openSettings()
	m.Text(a.profileField, "跨页通知草稿")
	a.rememberNotificationDraft()
	a.setAppearance("dark")
	check("darkAppearanceApplied", strings.Contains(m.Str(m.Send(m.Send(m.Send(m.Class("NSApplication"), "sharedApplication"), "effectiveAppearance"), "name")), "Dark"))
	a.showPage(pageHistory)
	a.openSettings()
	check("notificationDraftSurvivesThemeAndNavigation", m.Value(a.profileField) == "跨页通知草稿" && a.store.Data.Preferences.FocusProfile == "")
	path := a.store.Path
	a.store.Path = filepath.Join(path, "blocked")
	a.setAppearance("light")
	check("appearanceFailureRetainsValue", a.store.Data.Preferences.Appearance == "dark" && strings.Contains(m.Value(a.appearanceNotice), "未保存"))
	a.store.Path = path
	a.setAppearance("light")
	reopened, openErr := store.Open(filepath.Dir(path))
	check("appearanceDurable", openErr == nil && reopened.Data.Preferences.Appearance == "light")
	// Template edits cannot affect a session or its saved history snapshot.
	run := cfg.Clone()
	run.Apps = []store.App{{ID: "native.editor", Name: "Editor", Path: "/Applications/Editor.app"}}
	err = a.controller.Start(session.State{ID: "native-snapshot", Goal: "快照回归", Config: &run, TemplateID: saved.ID, TemplateName: saved.Name, Partial: true})
	check("configuredSessionStarts", err == nil)
	if err == nil {
		a.controller.Enter()
		run.Volume = .9
		a.controller.Pause()
		a.controller.ResumeWithApps(true, []string{"wrong"})
		check("sessionConfigurationIsolated", a.controller.Snapshot().Config.Volume == .17 && !a.controller.Allowed("wrong"))
		a.showPage(pageTemplates)
		a.showPage(pageSettings)
		check("sessionSurvivesNavigation", a.controller.Snapshot().ID == "native-snapshot" && a.controller.Snapshot().Phase == "active")
		a.controller.End()
	}
	a.store.Data.History = nil
	for i := 0; i < 41; i++ {
		c := cfg.Clone()
		a.store.Data.History = append(a.store.Data.History, session.State{ID: fmt.Sprint(i), Goal: fmt.Sprintf("写作记录 %d", i), Started: time.Now().Add(-time.Duration(i) * time.Hour), Phase: "ended", Config: &c, TemplateName: "原生模版回归", Partial: true})
	}
	a.openHistory()
	a.renderHistory()
	check("historyFirstPage", len(a.historyFiltered) == 20 && strings.Contains(m.Value(a.historyPageCaption), "41 条"))
	m.Send(a.historyNext, "performClick:", m.ID(0))
	m.Send(a.historyNext, "performClick:", m.ID(0))
	check("historyLastPage", a.historyPage == 2 && len(a.historyFiltered) == 1)
	a.deleteHistoryRecord("40")
	check("historyDeletionClampsPage", a.historyPage == 1 && len(a.historyFiltered) == 20)
	a.undoHistoryDeletion()
	check("historyUndoPreservesAll", len(a.store.Data.History) == 41)
	a.historyQuery = "记录 40"
	a.historyPage = 0
	a.refreshHistoryResults()
	check("historySearchAllPages", len(a.historyFiltered) == 1 && a.historyFiltered[0].ID == "40")
	a.openHistoryDetails("40")
	check("historyDetailsAttached", a.detail != nil && m.Send(m.ID(handle), "attachedSheet") != 0)
	if a.detail != nil {
		a.detail.Close()
	}
	a.historyQuery = "无结果"
	a.refreshHistoryResults()
	check("historyEmptyResults", len(a.historyFiltered) == 0 && !objc.Send[bool](a.historyEmpty, m.S("isHidden")))
	a.openMain()
	before := a.focusDraft.config.Clone()
	a.openApplicationPicker(a.focusForm)
	check("pickerAttached", a.appPicker != nil && m.Send(m.ID(handle), "attachedSheet") != 0)
	check("pickerSelectionFitsViewport", a.pickerList != nil && a.pickerList.RowFitsViewport(0))
	a.showPage(pageSettings)
	check("pickerBlocksNavigation", a.mainPage == pageFocus)
	if a.appPicker != nil {
		a.appPicker.Close()
	}
	check("pickerCancelPreservesDraft", reflect.DeepEqual(before, a.focusDraft.config))
	a.openApplicationSettings()
	check("recoveryControlsRemoved", nativeButton(a.settingsRoot, "处理系统专注模式…") == 0 && nativeButton(a.settingsRoot, "核对全屏窗口…") == 0 && nativeButton(a.settingsRoot, "重试恢复环境") == 0)
	a.openMain()
	check("startRecoveryLinkRemoved", nativeButton(a.pageRoot(pageFocus), "处理待恢复改动…") == 0)
	originalDraft := &store.Template{Name: "尚未保存的模版", Config: cfg.Clone()}
	a.templateDraft = originalDraft
	a.templateBaseline = nil
	a.saveFocusAsTemplate()
	check("saveAsPreservesOtherDraft", a.unknownSheet != nil && a.templateDraft == originalDraft)
	if a.unknownSheet != nil {
		sheet := m.Send(m.ID(handle), "attachedSheet")
		m.Send(nativeButton(m.Send(sheet, "contentView"), "取消"), "performClick:", m.ID(0))
	}
	check("saveAsCancelPreservesDraft", a.templateDraft == originalDraft)
	a.quit()
	check("quitChecksUnsavedDraft", a.unknownSheet != nil && !a.quitReady)
	if a.unknownSheet != nil {
		sheet := m.Send(m.ID(handle), "attachedSheet")
		cancel := nativeButton(m.Send(sheet, "contentView"), "取消")
		discard := nativeButton(m.Send(sheet, "contentView"), "放弃并退出")
		check("confirmationEscCancels", m.Str(m.Send(cancel, "keyEquivalent")) == "\x1b" && m.Str(m.Send(discard, "keyEquivalent")) != "\x1b")
	}
	if a.unknownSheet != nil {
		sheet := m.Send(m.ID(handle), "attachedSheet")
		m.Send(nativeButton(m.Send(sheet, "contentView"), "取消"), "performClick:", m.ID(0))
	}
	a.templateDraft = nil
	a.controller.Start(session.State{ID: "native-hidden-entry-quit", Goal: "隐藏入场退出回归", Config: &cfg, Partial: true})
	a.entryError = "隔离退出确认检查"
	a.main.Hide()
	a.quit()
	check("hiddenEntryQuitConfirmationVisible", a.main.IsVisible() && a.quitPrompt && a.unknownSheet != nil)
	a.escape(handle)
	check("entryQuitCancellationPreservesSession", a.controller.Snapshot().Phase == "entry" && !a.quitPrompt && !a.quitReady)
	// Continue the disposable probe even when the preceding regression fails.
	a.quitPrompt = false
	a.quit()
	pendingSheet := a.unknownSheet
	a.quit()
	check("repeatQuitRetainsPrompt", pendingSheet != nil && a.unknownSheet == pendingSheet && a.quitPrompt)
	if a.unknownSheet != nil {
		a.escape(handle)
	}
	a.quitPrompt = false

	a.controller.End()
	a.stopEnvironment()
	a.entryError = ""
	a.openMain()
	withoutBackground := cfg.Clone()
	withoutBackground.Background, withoutBackground.Entry, withoutBackground.Voice = false, true, true
	if err := a.controller.Start(session.State{ID: "native-no-background", Goal: "关闭背景回归", Config: &withoutBackground, Partial: true}); err == nil {
		a.beginEntry()
		a.openEntry()
		check("backgroundOffDoesNotShowEntryScene", !a.entryPresentation && a.video.Player == 0 && len(a.backgrounds) == 0 && a.entryGuideOnly())
		a.controller.End()
		a.stopEnvironment()
	} else {
		check("backgroundOffDoesNotShowEntryScene", false)
	}
	a.controller.Start(session.State{ID: "native-preparation-failure", Goal: "准备失败回归", Config: &cfg, Partial: true})
	a.controller.Pause()
	a.protectionFailure = protectionIssue("应用限制", "native.failure", errors.New("原生失败对象"))
	a.openFailureDetails()
	check("preparationDetailsReachable", a.detail != nil && nativeButton(m.Send(m.ID(a.detail.NativeHandle()), "contentView"), "重新检查并恢复") != 0)
	if a.detail != nil {
		a.detail.Close()
	}
	a.protectionFailure = protectionIssue("沉浸模式", "", errImmersionPermission)
	a.openFailureDetails()
	check("immersionPermissionHasSettingsAction", a.detail != nil && nativeButton(m.Send(m.ID(a.detail.NativeHandle()), "contentView"), "打开辅助功能权限…") != 0)
	if a.detail != nil {
		a.detail.Close()
	}
	a.protectionFailure = nil
	a.controller.End()
	m.ContentSize(a.main.NativeHandle(), minimumWidth, minimumHeight)
	m.Send(a.shell, "layoutSubtreeIfNeeded")
	for _, page := range []mainPage{pageFocus, pageTemplates, pageHistory, pageSettings} {
		a.showPage(page)
		m.Send(a.pages[page], "layoutSubtreeIfNeeded")
		result[fmt.Sprintf("page%dBounds", page)] = m.Bounds(a.pages[page])
		check(fmt.Sprintf("page%dFitsMinimum", page), m.Bounds(a.pages[page]).W >= minimumWidth-176-1)
	}
	a.hideMain()
	a.reopenMain()
	check("mainCloseReopensSameWindow", a.main.NativeHandle() == handle && a.main.IsVisible())
	a.nativeFullscreenCheck(func(axWritesVerified bool, err error) {
		if err != nil {
			result["fullscreenError"] = err.Error()
		}
		check("nativeFullscreenOwnership", err == nil)
		result["nativeFullscreenAXWritesVerified"] = axWritesVerified
		check("noPendingEffects", a.pendingEnvironment() == 0 && !a.focusPending)
		if len(failures) > 0 {
			result["error"] = strings.Join(failures, ", ")
		}
		result["checks"] = len(result)
		result["sessionPhase"] = a.controller.Snapshot().Phase
		result["pendingChanges"] = a.pendingEnvironment()
		if err := journal.WriteJSON(filepath.Join(a.dir, "native-check.json"), result); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		a.notificationDraft = nil
		a.focusDraft.baseline = a.focusDraft.config.Clone()
		a.templateDraft = nil
		a.store.Data.History = nil
		a.quit()
	})
}

// A distinct disposable bundle must be refused before countdown or storage.
// This check only creates its own UI and does not need keyboard focus.
func (a *application) nativeImmersionPermissionChecks(result map[string]any) {
	workApp, workErr := m.AppAt("/System/Applications/TextEdit.app")
	trusted, accessErr := m.FocusAccess()
	if workErr != nil || accessErr != nil || trusted {
		result["immersionDeniedBeforeCountdown"] = false
		return
	}
	beforePermission := a.store.Data.Preferences
	beforeSession := a.store.Data.Current
	beforeGoal, beforeNotice := a.draftGoal, a.notice
	a.store.Data.Preferences.Immersion = true
	cfg := beforePermission.Config()
	cfg.Apps = []store.App{workApp}
	reset := func() {
		if a.detail != nil {
			a.detail.Close()
		}
		a.stopCountdown()
		a.controller.End()
		a.store.Data.Current = beforeSession
		a.draftGoal, a.notice = beforeGoal, beforeNotice
	}
	defer func() {
		a.readImmersionAccess = nil
		a.protectionFailure = nil
		reset()
		result["immersionFixturePreferencesRestored"] = a.store.SetPreferences(beforePermission) == nil
		a.renderMain()
	}()
	rejected := !a.startConfigured("沉浸权限原生回归", cfg, "", "")
	result["immersionDeniedBeforeCountdown"] = rejected && a.detail != nil && !session.Exists(a.controller.Snapshot().Phase) &&
		a.countdownAt.IsZero() && reflect.DeepEqual(beforeSession, a.store.Data.Current)
	if !rejected || a.detail == nil {
		return
	}
	detail := a.detail
	result["immersionPermissionRepeatKeepsSheet"] = !a.startConfigured("重复点击", cfg, "", "") && a.detail == detail &&
		!session.Exists(a.controller.Snapshot().Phase) && a.countdownAt.IsZero()
	root := m.Send(m.ID(detail.NativeHandle()), "contentView")
	next := nativeButton(root, "开始沉浸专注")
	recheck := nativeButton(root, "重新检查权限")
	status := nativeControl(root, "辅助功能检查结果")
	enabled := func(button m.ID) bool { return button != 0 && objc.Send[bool](button, m.S("isEnabled")) }
	result["immersionPermissionLocatesCurrentApp"] = m.CurrentApplicationPath() != "" &&
		nativeButton(root, "在 Finder 中显示") != 0 && recheck != 0 && next != 0 && !enabled(next)
	// Only change the read result on the disposable UI. No synthetic grant is
	// ever used to launch, hide, or change another application's windows.
	var readErr error
	a.readImmersionAccess = func() (bool, error) { return trusted, readErr }
	trusted = true
	a.refreshImmersionPermission()
	grantRefreshed := enabled(next) && !session.Exists(a.controller.Snapshot().Phase) && a.countdownAt.IsZero()
	readErr = errors.New("隔离回归的权限读取失败")
	m.Send(recheck, "performClick:", m.ID(0))
	result["immersionPermissionRefreshIsReadOnly"] = grantRefreshed && !enabled(next) &&
		strings.Contains(m.Value(status), "查询组件未能返回状态") &&
		!session.Exists(a.controller.Snapshot().Phase) && reflect.DeepEqual(beforeSession, a.store.Data.Current)
	readErr = nil
	a.refreshImmersionPermission()
	m.Send(next, "performClick:", m.ID(0))
	started := a.controller.Snapshot()
	result["immersionPermissionRequiresExplicitStart"] = grantRefreshed && started.Phase == "entry" && started.Immersion
	reset()

	trusted, readErr = false, nil
	if a.startConfigured("普通专注回退原生回归", cfg, "", "") || a.detail == nil {
		return
	}
	root = m.Send(m.ID(a.detail.NativeHandle()), "contentView")
	ordinary := nativeButton(root, "本次改用普通专注")
	m.Send(ordinary, "performClick:", m.ID(0))
	started = a.controller.Snapshot()
	result["immersionPermissionOrdinaryStartIsOneTime"] = ordinary != 0 && started.Phase == "entry" &&
		!started.Immersion && a.store.Data.Preferences.Immersion
	reset()

	if err := a.controller.Start(session.State{ID: "native-permission-resume", Goal: "恢复权限原生回归", Config: &cfg, Partial: true, Immersion: true}); err != nil {
		return
	}
	a.controller.Pause()
	paused := a.controller.Snapshot()
	a.resumeWithConsent()
	if a.detail == nil {
		return
	}
	root = m.Send(m.ID(a.detail.NativeHandle()), "contentView")
	next = nativeButton(root, "恢复沉浸专注")
	blocked := errors.Is(a.protectionFailure, errImmersionPermission) && next != 0 && !enabled(next)
	trusted = true
	a.refreshImmersionPermission()
	result["immersionPermissionResumeStaysPaused"] = blocked && enabled(next) &&
		reflect.DeepEqual(paused, a.controller.Snapshot()) && !a.busy
}
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func (a *application) nativeMediaRecoveryChecks(check func(string, bool)) {
	originalResources, originalScenes := a.resources, a.catalog.Scenes
	originalGuide := a.catalog.Guide
	originalConfig := a.focusDraft.config.Clone()
	defer func() {
		a.stopSceneAudio()
		a.controller.End()
		a.resources, a.catalog.Scenes = originalResources, originalScenes
		a.catalog.Guide = originalGuide
		a.mediaFailures = nil
		a.notice = Notice{}
		a.focusDraft.config = originalConfig
		a.renderReady()
	}()
	root := filepath.Join(a.dir, "native-media-retry")
	if err := os.MkdirAll(filepath.Join(root, "media"), 0700); err != nil {
		check("natureRetryFixture", false)
		return
	}
	scene, _ := a.catalog.Scene("forest")
	copyRole := func(file media.File, name string) (media.File, []byte) {
		path, err := media.Resolve(originalResources, file)
		if err != nil {
			return media.File{}, nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return media.File{}, nil
		}
		file.Path = "media/" + name
		if err := os.WriteFile(filepath.Join(root, file.Path), body, 0600); err != nil {
			return media.File{}, nil
		}
		return file, body
	}
	background, imageBytes := copyRole(scene.Background, "background.png")
	pondBackground, _ := copyRole(scene.Background, "pond.png")
	work, audioBytes := copyRole(scene.Work, "work.wav")
	entry, _ := copyRole(scene.Entry, "entry.mp4")
	guide, _ := copyRole(a.catalog.Guide, "guide.m4a")
	if len(imageBytes) == 0 || len(audioBytes) == 0 || entry.Path == "" {
		check("natureRetryFixture", false)
		return
	}
	scene.Background, scene.Preview, scene.Work, scene.Entry = background, background, work, entry
	pond := scene
	pond.ID, pond.Name, pond.Background, pond.Preview = "pond", "池塘", pondBackground, pondBackground
	a.resources, a.catalog.Scenes = root, []media.Scene{scene, pond}
	a.catalog.Guide = guide
	a.mediaFailures = nil
	a.focusDraft.config.Scene = "forest"
	a.renderReady()
	pondPath := filepath.Join(root, pondBackground.Path)
	if err := os.Remove(pondPath); err != nil {
		check("natureUnselectedSceneRecovery", false)
		return
	}
	a.focusForm.refreshScene()
	unavailable := !objc.Send[bool](a.focusForm.grid.Buttons[1], m.S("isEnabled")) && strings.Contains(a.formMediaFailureCaption(a.focusForm), "池塘") && !objc.Send[bool](a.focusForm.mediaGroup, m.S("isHidden"))
	if err := os.WriteFile(pondPath, imageBytes, 0600); err != nil {
		check("natureUnselectedSceneRecovery", false)
		return
	}
	a.retrySceneMedia("forest")
	check("natureUnselectedSceneRecovery", unavailable && objc.Send[bool](a.focusForm.grid.Buttons[1], m.S("isEnabled")) && a.focusDraft.config.Scene == "forest")
	imagePath := filepath.Join(root, background.Path)
	if err := os.Remove(imagePath); err != nil {
		check("natureRetryFixture", false)
		return
	}
	a.focusForm.refreshScene()
	if err := os.WriteFile(imagePath, imageBytes, 0600); err != nil {
		check("natureRetryFixture", false)
		return
	}
	m.Send(nativeButton(a.root, "重新读取正式资源"), "performClick:", m.ID(0))
	check("natureRetryRestoresPreview", m.Send(a.focusForm.scenePreview, "image") != 0 && a.mediaFailureCaptionFor("forest") == "")
	bad := []byte("matching hash, not a decodable image")
	hash := sha256.Sum256(bad)
	a.catalog.Scenes[0].Background.SHA256 = hex.EncodeToString(hash[:])
	if err := os.WriteFile(imagePath, bad, 0600); err != nil {
		check("natureRetryFixture", false)
		return
	}
	a.focusForm.refreshScene()
	check("natureUndecodablePreviewReported", m.Send(a.focusForm.scenePreview, "image") == 0 && a.mediaFailureCaptionFor("forest") != "")
	a.catalog.Scenes[0].Background = background
	if err := os.WriteFile(imagePath, imageBytes, 0600); err != nil {
		check("natureRetryFixture", false)
		return
	}
	a.focusForm.refreshScene()
	cfg := originalConfig.Clone()
	a.focusDraft.config.Sound, a.focusDraft.config.Volume = true, .000001
	a.focusDraft.config.Ambience = &session.AmbienceConfig{Mode: "off"}
	badAudio := []byte("matching hash, not decodable audio")
	audioHash := sha256.Sum256(badAudio)
	a.catalog.Scenes[0].Work.SHA256 = hex.EncodeToString(audioHash[:])
	if err := os.WriteFile(filepath.Join(root, work.Path), badAudio, 0600); err != nil {
		check("natureRetryFixture", false)
		return
	}
	a.auditionConfig(a.focusDraft.config.Clone(), a.focusForm.notice)
	check("natureUndecodableAudioReported", a.audition.Player == 0 && a.mediaFailureCaptionFor("forest") != "")
	a.retrySceneMedia("forest")
	check("natureRetryRetainsUndecodableAudio", a.audio.Player == 0 && a.audition.Player == 0 && a.mediaFailureCaptionFor("forest") != "")
	a.catalog.Scenes[0].Work = work
	if err := os.WriteFile(filepath.Join(root, work.Path), audioBytes, 0600); err != nil {
		check("natureRetryFixture", false)
		return
	}
	a.retrySceneMedia("forest")
	check("natureReadyRetrySilentAfterRepair", a.audio.Player == 0 && a.audition.Player == 0 && a.mediaFailureCaptionFor("forest") == "")
	cfg.Scene, cfg.Sound, cfg.Volume, cfg.Background = "forest", true, .000001, false
	cfg.Ambience = &session.AmbienceConfig{Mode: "off"}
	cfg.Apps = []store.App{{ID: "native.editor", Path: "/Applications/Editor.app"}}
	if err := a.controller.Start(session.State{ID: "native-audio-retry", Goal: "声音重读", Config: &cfg, Partial: true}); err != nil {
		check("natureRetryRestoresAudio", false)
		return
	}
	if err := os.Remove(filepath.Join(root, work.Path)); err != nil {
		check("natureRetryFixture", false)
		return
	}
	a.environmentSound()
	if err := os.WriteFile(filepath.Join(root, work.Path), audioBytes, 0600); err != nil {
		check("natureRetryFixture", false)
		return
	}
	m.Send(nativeButton(a.root, "重新读取正式资源"), "performClick:", m.ID(0))
	player := a.audio.Player
	check("natureRetryRestoresAudio", player != 0 && a.audioScene == "forest")
	m.Send(nativeButton(a.root, "重新读取正式资源"), "performClick:", m.ID(0))
	check("natureRepeatRetryKeepsAudio", player != 0 && a.audio.Player == player)
}
func nativeButton(root m.ID, title string) m.ID {
	if objc.Send[bool](root, m.S("isKindOfClass:"), m.Class("NSButton")) && m.Str(m.Send(root, "title")) == title {
		return root
	}
	children := m.Send(root, "subviews")
	for i := uint64(0); i < objc.Send[uint64](children, m.S("count")); i++ {
		if b := nativeButton(m.Send(children, "objectAtIndex:", i), title); b != 0 {
			return b
		}
	}
	return 0
}

func nativeControl(root m.ID, label string) m.ID {
	if objc.Send[bool](root, m.S("isKindOfClass:"), m.Class("NSControl")) && m.Str(m.Send(root, "accessibilityLabel")) == label {
		return root
	}
	children := m.Send(root, "subviews")
	for i := uint64(0); i < objc.Send[uint64](children, m.S("count")); i++ {
		if found := nativeControl(m.Send(children, "objectAtIndex:", i), label); found != 0 {
			return found
		}
	}
	return 0
}

func (a *application) benchmarkHistory(count int) {
	a.store.Data.History = make([]session.State, count)
	for i := range a.store.Data.History {
		a.store.Data.History[i] = session.State{ID: fmt.Sprintf("scale-%d", i), Goal: fmt.Sprintf("合成记录 %d：测量原生清单", i), Minutes: 30, Scene: "forest", Phase: "ended", Started: time.Now().Add(-time.Duration(i) * time.Hour), Elapsed: 30 * time.Minute}
	}
	started := time.Now()
	err := a.store.Save()
	write := time.Since(started)
	started = time.Now()
	a.openHistory()
	m.Send(a.historyRoot, "layoutSubtreeIfNeeded")
	opened := time.Since(started)
	started = time.Now()
	doc := m.Send(a.historyScroll, "documentView")
	m.RestoreScroll(a.historyScroll, m.Point{Y: m.Bounds(doc).H})
	m.Send(a.historyRoot, "layoutSubtreeIfNeeded")
	scrolled := time.Since(started)
	var usage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	result := map[string]any{"count": count, "writeMS": float64(write.Microseconds()) / 1000, "openAndLayoutMS": float64(opened.Microseconds()) / 1000, "scrollAndLayoutMS": float64(scrolled.Microseconds()) / 1000, "peakRSSBytes": usage.Maxrss, "historyCount": len(a.store.Data.History), "allocatedRows": a.historyList.AllocatedRows(), "scope": "isolated synthetic history; no Focus or workspace changes"}
	if err != nil {
		result["error"] = err.Error()
	}
	if e := journal.WriteJSON(filepath.Join(a.dir, "history-benchmark.json"), result); e != nil {
		fmt.Fprintln(os.Stderr, e)
	}
	a.quit()
}

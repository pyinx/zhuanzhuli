//go:build darwin

package main

import (
	"focus.local/zhuanzhuli/internal/media"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"path/filepath"
)

// Read-only evidence for the explicitly isolated fixture UI run. This does not
// drive controls, activate applications, or inspect other applications' windows.
func (a *application) acceptanceState() {
	if a.fixtureRoot == "" {
		return
	}
	windows := map[string]any{}
	for name, w := range map[string]*mygo.Window{"main": a.main, "detail": a.detail, "control": a.control, "entry": a.entry, "appPicker": a.appPicker, "choice": a.choice} {
		if w == nil || w.NativeHandle() == 0 {
			continue
		}
		n := m.ID(w.NativeHandle())
		responder := m.Send(n, "firstResponder")
		windows[name] = map[string]any{
			"frame": m.FrameOf(n), "contentBounds": m.Bounds(m.Send(n, "contentView")),
			"number":         objc.Send[int64](n, m.S("windowNumber")),
			"visible":        objc.Send[bool](n, m.S("isVisible")),
			"key":            objc.Send[bool](n, m.S("isKeyWindow")),
			"onActiveSpace":  objc.Send[bool](n, m.S("isOnActiveSpace")),
			"level":          objc.Send[int64](n, m.S("level")),
			"responderClass": m.Str(m.Send(m.Send(responder, "class"), "description")),
		}
	}
	state := map[string]any{"session": a.controller.Snapshot(), "windows": windows, "hostActive": m.SelfActive(),
		"appearance":    m.Str(m.Send(m.Send(m.Send(m.Class("NSApplication"), "sharedApplication"), "effectiveAppearance"), "name")),
		"pendingChoice": a.pending, "pendingChanges": a.pendingEnvironment(),
		"focusPending": a.focusPending,
		"busy":         a.busy, "backgroundCount": len(a.backgrounds),
		"audioPlaying":   a.audio.Player != 0 && objc.Send[bool](a.audio.Player, m.S("isPlaying")),
		"guidePlaying":   a.guide.Player != 0 && objc.Send[bool](a.guide.Player, m.S("isPlaying")),
		"videoAllocated": a.video.Player != 0}
	state["auditionPlaying"] = a.audition.Player != 0 && objc.Send[bool](a.audition.Player, m.S("isPlaying"))
	state["auditionNaturePlaying"] = a.auditionNature.Player != 0 && objc.Send[bool](a.auditionNature.Player, m.S("isPlaying"))
	state["naturePlaying"] = a.nature.Player != 0 && objc.Send[bool](a.nature.Player, m.S("isPlaying"))
	if a.nature.Player != 0 {
		state["natureTime"] = objc.Send[float64](a.nature.Player, m.S("currentTime"))
		state["natureVolume"] = objc.Send[float32](a.nature.Player, m.S("volume"))
	}
	state["auditionGeneration"] = a.auditionGeneration
	state["audioScene"] = a.audioScene
	state["audioSession"] = a.audioSession
	state["audioPlayer"] = uintptr(a.audio.Player)
	if a.audio.Player != 0 {
		state["audioTime"] = objc.Send[float64](a.audio.Player, m.S("currentTime"))
		state["audioVolume"] = objc.Send[float32](a.audio.Player, m.S("volume"))
	}
	if a.focusDraft != nil {
		state["focusScene"] = a.focusDraft.config.Scene
	}
	if a.templateDraft != nil {
		state["templateScene"] = a.templateDraft.Config.Scene
	}
	state["trayTemplate"] = a.trayTemplate
	state["buildID"] = buildID
	state["entryPresentation"] = a.entryPresentation
	state["entryNumber"] = m.Value(a.countdownOverlay.Label)
	if n := a.countdownOverlay.Window; n != 0 {
		state["countdown"] = map[string]any{
			"visible": objc.Send[bool](n, m.S("isVisible")), "frame": m.FrameOf(n),
			"screen":       m.DisplayFrame(a.countdownOverlay.Display, false),
			"canBecomeKey": objc.Send[bool](n, m.S("canBecomeKeyWindow")),
		}
	}
	state["quickTemplate"] = a.store.Data.Preferences.QuickTemplate
	if n := a.counterOverlay.Window; n != 0 {
		state["counterOverlay"] = map[string]any{"frame": m.FrameOf(n), "key": objc.Send[bool](n, m.S("isKeyWindow")), "main": objc.Send[bool](n, m.S("isMainWindow")), "canBecomeKey": objc.Send[bool](n, m.S("canBecomeKeyWindow")), "ignoresMouseEvents": objc.Send[bool](n, m.S("ignoresMouseEvents")), "opaque": objc.Send[bool](n, m.S("isOpaque")), "visible": objc.Send[bool](n, m.S("isVisible")), "text": m.Value(a.counterOverlay.Label)}
	}
	state["notice"] = a.notice
	state["mainPage"] = a.mainPage
	state["windowSizing"] = a.windowSizing
	if a.main != nil {
		state["currentWindowMetrics"] = m.WindowMetrics(a.main.NativeHandle())
	}
	state["mainModal"] = a.mainModal()
	state["historyCount"] = len(a.store.Data.History)
	state["templateCount"] = len(a.store.Data.Templates)
	state["templateDraftDirty"] = a.templateIsDirty()
	state["focusDraftDirty"] = a.focusIsDirty()
	state["historyQuery"] = a.historyQuery
	state["historyPage"] = a.historyPage
	state["historyVisibleCount"] = len(a.historyFiltered)
	state["configuredAppearance"] = a.store.Data.Preferences.Appearance
	state["notificationDraftDirty"] = a.profileField != 0 && (m.Value(a.profileField) != a.store.Data.Preferences.FocusProfile || m.Checked(a.linkedField) != a.store.Data.Preferences.FocusLinked)
	state["draftGoalLength"] = len([]rune(m.Value(a.goal)))
	state["readyErrors"] = map[string]string{"goal": m.Value(a.goalError), "minutes": m.Value(a.minuteError), "apps": m.Value(a.appsError)}
	if a.main != nil {
		state["navigation"] = map[string]any{"selectedRow": objc.Send[int64](a.navigation.Table, m.S("selectedRow")), "enabled": objc.Send[bool](a.navigation.Table, m.S("isEnabled")), "attachedSheet": m.Send(m.ID(a.main.NativeHandle()), "attachedSheet") != 0}
	}
	f := a.focusForm
	if a.mainPage == pageTemplates {
		f = a.templateForm
	}
	if f != nil && f.apps != nil {
		state["duration"] = map[string]any{
			"minutes": f.get().Minutes, "input": m.Value(f.minutes),
			"slider": objc.Send[int64](f.minuteSlider, m.S("integerValue")),
		}
		state["sceneControl"] = map[string]any{"scene": f.get().Scene,
			"title":   media.SceneName(f.get().Scene),
			"caption": m.Value(f.sceneCaption), "count": len(f.sceneIDs),
			"previewLoaded": m.Send(f.scenePreview, "image") != 0,
			"config":        f.get().Clone()}
		list := f.apps
		apps := f.get().Apps
		rows := []map[string]any{}
		for i := range apps {
			if i >= 6 {
				break
			}
			icon, text, ok := list.RowGeometry(i)
			rows = append(rows, map[string]any{"index": i, "measured": ok, "icon": icon, "text": text})
		}
		state["appList"] = map[string]any{"count": len(apps), "selected": list.Selected(), "rows": rows, "bounds": m.Bounds(list.View), "canvasMatches": objc.Send[bool](m.Send(list.Table, "backgroundColor"), m.S("isEqual:"), m.CanvasColor())}
	}
	state["displays"] = mygo.Screen.Displays()
	backgrounds := []map[string]any{}
	for _, w := range a.backgrounds {
		n := m.ID(w.NativeHandle())
		if n == 0 {
			continue
		}
		backgrounds = append(backgrounds, map[string]any{
			"frame": m.FrameOf(n), "screenFrame": m.FrameOf(m.Send(n, "screen")),
			"visible":            objc.Send[bool](n, m.S("isVisible")),
			"ignoresMouseEvents": objc.Send[bool](n, m.S("ignoresMouseEvents")),
			"onActiveSpace":      objc.Send[bool](n, m.S("isOnActiveSpace")),
			"level":              objc.Send[int64](n, m.S("level")),
			"collectionBehavior": objc.Send[uint64](n, m.S("collectionBehavior")),
		})
	}
	state["backgroundWindows"] = backgrounds
	workspace := m.Send(m.Class("NSWorkspace"), "sharedWorkspace")
	state["accessibility"] = map[string]bool{
		"voiceOverEnabled":   objc.Send[bool](workspace, m.S("isVoiceOverEnabled")),
		"reduceMotion":       m.ReducedMotion(),
		"reduceTransparency": objc.Send[bool](workspace, m.S("accessibilityDisplayShouldReduceTransparency")),
		"increaseContrast":   objc.Send[bool](workspace, m.S("accessibilityDisplayShouldIncreaseContrast")),
	}
	if a.main != nil && a.goal != 0 && a.minutes != 0 {
		editor := m.Send(m.ID(a.main.NativeHandle()), "firstResponder")
		delegate := m.ID(0)
		if objc.Send[bool](editor, m.S("respondsToSelector:"), m.S("delegate")) {
			delegate = m.Send(editor, "delegate")
		}
		state["keyboard"] = map[string]any{
			"editingGoal": delegate == a.goal, "editingMinutes": delegate == a.minutes,
			"goalNextClass":        m.Str(m.Send(m.Send(m.Send(a.goal, "nextValidKeyView"), "class"), "description")),
			"minutesPreviousClass": m.Str(m.Send(m.Send(m.Send(a.minutes, "previousValidKeyView"), "class"), "description")),
		}
	}
	if err := journal.WriteJSON(filepath.Join(a.dir, "acceptance-state.json"), state); err != nil {
		a.fail(noticeStorage, "无法保存隔离验收状态", err)
	}
}

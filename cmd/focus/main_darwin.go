//go:build darwin

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"focus.local/zhuanzhuli/internal/media"
	"focus.local/zhuanzhuli/internal/notification"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"focus.local/zhuanzhuli/pkg/journal"
	probe "focus.local/zhuanzhuli/pkg/observe"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type application struct {
	featureState
	uiState
	store                                         *store.Store
	controller                                    *session.Controller
	journal                                       *journal.Journal
	immersionJournal                              *journal.Journal
	fullscreenLeases                              map[journal.Ref]*m.FullscreenWindow // serial effect worker only
	dir, resources                                string
	catalog                                       media.Catalog
	mediaFailures                                 map[string]error
	nativeReady                                   bool
	startDisplay                                  m.Display
	counterOverlay                                m.CounterOverlay
	entryPresentation                             bool
	previousPresentation                          uint64
	reminderUntil                                 time.Time
	reminderPlaybackFailed                        bool
	main, choice                                  *mygo.Window
	backgrounds                                   []*mygo.Window
	root, goal, minutes, status, primary, preview m.ID
	busy, quitting, quitReady                     bool
	pending                                       *journal.Ref
	queuedOutside                                 *journal.Ref
	outsideResult                                 string
	lastWork                                      *journal.Ref
	expected                                      map[int]bool
	audio                                         m.Audio
	nature                                        m.Audio
	natureID                                      string
	audioSession, audioScene                      string
	audioGeneration                               uint64
	guide                                         m.Audio
	previewVideo                                  m.Video
	previewWindow                                 *mygo.Window
	previewSceneID                                string
	previewAt                                     time.Time
	video                                         m.Video
	entryAt                                       time.Time
	countdownAt                                   time.Time
	countdownTimer                                *time.Timer
	countdownDone                                 func()
	countdownOverlay                              m.CounterOverlay
	entryError                                    string
	tray                                          *mygo.Tray
	notice                                        Notice
	undo                                          []deleted
	stop                                          chan struct{}
	off                                           func()
	offEscape                                     func()
	needsRestore                                  bool
	environmentRestored                           bool // serial effect worker only
	fixtureRoot                                   string
	draftGoal                                     string
	protectionFailure                             error
	readImmersionAccess                           func() (bool, error) // nil uses the current process's OS grant
	protectionGeneration                          uint64
	preparing                                     *protectionPlan
	limitedAppConsent                             map[string]bool
	acceptLimitedApps                             bool
	choiceButtons                                 []m.ID
	choiceStatus                                  m.ID
	settingsNotice                                m.ID
	notificationStatuses                          [2]m.ID
	notifications                                 *notification.Link
	notificationNotice                            string
	focusPending                                  bool
}
type deleted struct {
	record session.State
	index  int
}

var errHideCancelled = errors.New("专注已暂停或结束，取消收起")
var appDisplayName = "归径"
var defaultDataDirectory = "Zhuanzhuli"

const settingsCaption = "通知联动配置将在下次开始或恢复时使用。"
const preferenceSaveError = "设置未保存，请检查磁盘和本地数据目录后重试。"

func main() {
	mygo.App.SetName(appDisplayName)
	mygo.App.SetLanguages("zh-Hans")
	user, _ := os.UserConfigDir()
	dir := flag.String("data-dir", filepath.Join(user, defaultDataDirectory), "local data directory")
	check := flag.Bool("self-check", false, "validate resources and local data without starting a session")
	downgradeJournal := flag.Bool("downgrade-journal", false, "explicit offline journal downgrade; refuses unresolved changes; requires --data-dir")
	nativeCheck := flag.Bool("native-check", false, "construct owned native windows and exit; not visual verification")
	historyBenchmark := flag.Int("history-benchmark", -1, "native-check only: isolated history scale 0, 100, 1000 or 10000")
	notificationCheck := flag.Bool("notification-check", false, "audit existing notification linkage permission without requesting it or switching system Focus")
	panelCloseProbe := flag.Bool("panel-close-probe", false, "read-only owned-panel Escape candidate; requires notification-check and an explicit empty data directory")
	fixtureCheck := flag.String("fixture-check", "", "diagnose app effects against only two explicitly identified local fixtures")
	fixtureUI := flag.String("fixture-ui", "", "interactive acceptance with effects restricted to two local fixtures; requires --data-dir")
	fixtureAppearance := flag.String("fixture-appearance", "", "isolated fixture UI only: light or dark; does not change system settings")
	flag.Parse()
	explicitDataDir := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "data-dir" {
			explicitDataDir = true
		}
	})
	if *downgradeJournal && (!explicitDataDir || *check || *nativeCheck || *notificationCheck || *fixtureCheck != "" || *fixtureUI != "" || *panelCloseProbe || *historyBenchmark != -1 || *fixtureAppearance != "") {
		startupError("journal downgrade requires an explicit data directory and no other operation", true)
	}
	if *historyBenchmark != -1 {
		validSize := *historyBenchmark == 0 || *historyBenchmark == 100 || *historyBenchmark == 1000 || *historyBenchmark == 10000
		if !validSize || !explicitDataDir || !*nativeCheck || *check || *notificationCheck || *panelCloseProbe || *fixtureCheck != "" || *fixtureUI != "" {
			startupError("history benchmark requires native-check, an explicit fresh data directory and size 0, 100, 1000 or 10000", true)
		}
		entries, err := os.ReadDir(*dir)
		if (err != nil && !errors.Is(err, os.ErrNotExist)) || len(entries) != 0 {
			startupError("history benchmark requires a fresh empty data directory", true)
		}
	}
	if *panelCloseProbe {
		if !explicitDataDir || !*notificationCheck || *nativeCheck || *check || *fixtureCheck != "" || *fixtureUI != "" {
			startupError("panel close probe requires --notification-check and an explicit isolated data directory, without other checks", true)
		}
	}
	if *fixtureAppearance != "" && (*fixtureUI == "" || (*fixtureAppearance != "light" && *fixtureAppearance != "dark")) {
		startupError("fixture appearance requires --fixture-ui and light or dark", true)
	}
	if *fixtureUI != "" {
		if !explicitDataDir || *fixtureCheck != "" {
			startupError("fixture UI requires an explicit isolated data directory and no scripted check", true)
		}
	}
	if *nativeCheck || *notificationCheck || *fixtureCheck != "" {
		// Reject persisted state before creating a lock or opening journals.
		// Even an otherwise empty store may have a production recovery lease.
		if !explicitDataDir {
			startupError("diagnostics require an explicit fresh empty data directory", true)
		}
		entries, err := os.ReadDir(*dir)
		if (err != nil && !errors.Is(err, os.ErrNotExist)) || len(entries) != 0 {
			startupError("diagnostics require a fresh empty data directory; existing files were not changed", true)
		}
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		startupError("无法打开本地数据目录，请检查访问权限。", *check || *nativeCheck)
	}
	lock, err := os.OpenFile(filepath.Join(*dir, "app.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		startupError("无法打开本地数据锁，请检查访问权限。", *check || *nativeCheck)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		startupError("此数据目录已有运行中的「"+appDisplayName+"」。请从 Dock 返回现有应用。", *check || *nativeCheck)
	}
	if *downgradeJournal {
		j, err := journal.Open(filepath.Join(*dir, "environment.json"))
		if err != nil {
			startupError(err.Error(), true)
		}
		if err = j.Downgrade(); err != nil {
			startupError(err.Error(), true)
		}
		fmt.Println("journal is v1; backup retained; no applications or system settings were changed")
		return
	}
	s, e := store.Open(*dir)
	if e != nil {
		startupError(e.Error(), *check || *nativeCheck)
	}
	if (*nativeCheck || *notificationCheck || *fixtureCheck != "") && (s.Data.Current != nil || len(s.Data.History) > 0 || len(s.Data.Preferences.Apps) > 0) {
		startupError("诊断必须使用独立的空数据目录，原数据未修改。", true)
	}
	exe, _ := os.Executable()
	resources := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "Resources"))
	catalog, resourceErr := media.Load(resources)
	if resourceErr != nil && *check {
		startupError("正式场景资源无法校验："+resourceErr.Error(), *check || *nativeCheck)
	}
	if resourceErr != nil {
		parsed, parseErr := media.Read(resources)
		catalog = media.Catalog{}
		if parseErr == nil {
			catalog = parsed
		}
	}
	if *check {
		fmt.Println("resources and local data: valid")
		return
	}
	j, e := journal.Open(filepath.Join(*dir, "environment.json"))
	if e != nil {
		startupError("环境恢复记录无法读取，已保留原文件，未启动新的专注。", *check || *nativeCheck)
	}
	a := &application{store: s, journal: j, dir: *dir, resources: resources, controller: session.New(time.Now), expected: map[int]bool{}, stop: make(chan struct{})}
	a.immersionJournal, e = journal.Open(filepath.Join(*dir, "fullscreen.json"))
	if e != nil {
		startupError("全屏恢复记录无法读取，已保留原文件："+e.Error(), *check || *nativeCheck)
	}
	a.catalog = catalog
	if resourceErr != nil {
		a.notice = Notice{noticeInfo, "部分正式资源不可用；入场可重试或跳过。"}
	}
	a.notifications, e = notification.Open(filepath.Join(*dir, "notifications.json"))
	if e != nil {
		startupError("通知恢复记录无法读取，已保留原文件："+e.Error(), *check || *nativeCheck)
	}
	a.notificationNotice = "通知联动未开启"
	fixturePath := *fixtureCheck
	if *fixtureUI != "" {
		fixturePath = *fixtureUI
	}
	if fixturePath != "" {
		root, err := filepath.EvalSymlinks(fixturePath)
		if err != nil || filepath.Base(root) != "bundles" || filepath.Base(filepath.Dir(root)) != ".build" {
			startupError("invalid fixture boundary", true)
		}
		a.fixtureRoot = root
		if err := a.validateFixtureRecovery(); err != nil {
			startupError(err.Error(), true)
		}
	}
	if s.Data.Current != nil {
		a.controller.Recover(*s.Data.Current)
		a.notice = Notice{noticeInfo, "上次专注已暂停，未知间隔未计时。请检查环境后主动恢复。"}
		if s.Data.Current.TemporaryAllowed == nil || s.Data.Current.LegacyAppScope {
			a.notice.Text += "旧版会话的原应用名单将保留到本次结束。"
		}
	}
	if err := a.ignoreEnvironment(); err != nil {
		startupError("无法保存旧环境记录的处理结果："+err.Error(), *nativeCheck || *notificationCheck || *fixtureCheck != "" || *fixtureUI != "")
	}
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		if e := m.Init(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			mygo.App.Quit()
			return
		}
		a.nativeReady = true
		if err := m.ApplyAppearance(a.store.Data.Preferences.Appearance); err != nil {
			startupError(err.Error(), true)
		}
		if *fixtureAppearance != "" {
			name := "NSAppearanceNameAqua"
			if *fixtureAppearance == "dark" {
				name = "NSAppearanceNameDarkAqua"
			}
			appearance := m.Send(m.Class("NSAppearance"), "appearanceNamed:", m.NS(name))
			if appearance == 0 {
				startupError("isolated appearance unavailable", true)
			}
			m.Send(m.Send(m.Class("NSApplication"), "sharedApplication"), "setAppearance:", appearance)
		}
		a.menu()
		a.openMain()
		if !*nativeCheck && !*notificationCheck {
			a.off = m.Observe(a.workspace, func(issue *m.AppIdentityError) {
				if a.fixtureRoot != "" {
					return
				}
				if !session.Exists(a.controller.Snapshot().Phase) || a.controller.Snapshot().Phase == "paused" {
					return
				}
				if a.limitedAppConsent[issue.Key()] {
					return
				}
				a.protectionFailure = protectionIssue("应用限制", issue.Key(), issue)
				a.pause("无法确认新应用身份，已暂停：" + issue.Label)
			})
		}
		a.offEscape = m.Escape(a.escape)
		mygo.Screen.OnDisplaysChanged(func() {
			a.counterOverlay.Reposition()
			a.countdownOverlay.Reposition()
			if session.Working(a.controller.Snapshot().Phase) {
				a.backgroundsOn()
			}
		})
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
		go func() {
			select {
			case <-signals:
				mygo.RunOnMain(a.quit)
			case <-a.stop:
			}
			signal.Stop(signals)
		}()
		if *nativeCheck || *notificationCheck || a.fixtureRoot != "" {
			self, err := m.Describe(m.Send(m.Class("NSRunningApplication"), "currentApplication"))
			if err != nil {
				startupError("could not identify application fixture host", true)
			}
			if err = journal.WriteJSON(filepath.Join(a.dir, "instance.json"), self); err != nil {
				startupError("could not write fixture host identity", true)
			}
		}
		if *nativeCheck {
			if *historyBenchmark >= 0 {
				a.benchmarkHistory(*historyBenchmark)
				return
			}
			// Geometry and empty-state checks do not require keyboard ownership.
			// Preserve their results even when the separate activation check fails.
			uiChecks := map[string]any{"buildID": buildID}
			a.nativeImmersionPermissionChecks(uiChecks)
			a.openMain()
			// Match the real user precondition before testing explicit keyboard
			// focus. LaunchServices activation completes after WhenReady returns.
			var readiness map[string]bool
			a.effect(func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				ticks := time.NewTicker(40 * time.Millisecond)
				defer ticks.Stop()
				_, err := probe.AwaitChange(ctx, func() error {
					// A relocated diagnostic bundle may not be activated by repeated
					// LaunchServices launches. Request this host once, after the event
					// loop starts; still require observed activation before testing keys.
					mygo.RunOnMain(func() {
						mygo.App.Focus()
						a.main.Focus()
					})
					return nil
				}, func() (bool, error) {
					ready := false
					mygo.RunOnMain(func() {
						app := m.Send(m.Class("NSApplication"), "sharedApplication")
						readiness = map[string]bool{
							"selfActive": m.SelfActive(), "appActive": objc.Send[bool](app, m.S("isActive")),
							"visible": a.main.IsVisible(), "focused": a.main.IsFocused(),
							"keyMatchesMain": m.Send(app, "keyWindow") == m.ID(a.main.NativeHandle()),
						}
						ready = readiness["selfActive"] && readiness["visible"] && readiness["focused"]
					})
					return ready, nil
				}, ticks.C)
				return err
			}, func(err error) {
				if err != nil {
					uiChecks["error"] = "native host foreground precondition: " + err.Error()
					uiChecks["readiness"] = readiness
					_ = journal.WriteJSON(filepath.Join(a.dir, "native-check.json"), uiChecks)
					a.quit()
					return
				}
				a.nativeCheck(uiChecks)
			})
			return
		}
		if *notificationCheck {
			a.notificationCheck(*panelCloseProbe)
			return
		}
		if *fixtureCheck != "" {
			go a.fixtureCheck()
			return
		}
		mygo.Power.OnSuspend(func() { a.pauseForSystem("系统睡眠，已自动暂停") })
		mygo.Power.OnLockScreen(func() { a.pauseForSystem("屏幕锁定，已自动暂停") })
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-a.stop:
					return
				case <-t.C:
					mygo.RunOnMain(a.tick)
				}
			}
		}()
	})
	mygo.App.OnActivate(func(bool) { a.reopenMain() })
	mygo.App.OnDidBecomeActive(a.refreshImmersionPermission)
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !a.quitReady {
			e.PreventDefault()
			a.quit()
		}
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *nativeCheck || *notificationCheck || *fixtureCheck != "" || *fixtureUI != "" {
		if err := journal.WriteJSON(filepath.Join(a.dir, "normal-exit.json"), map[string]any{"eventLoopReturned": true, "pendingChanges": a.pendingEnvironment()}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
func startupError(message string, headless bool) {
	fmt.Fprintln(os.Stderr, message)
	if !headless {
		mygo.App.WhenReady(func() {
			if m.Init() == nil {
				alert := m.Send(m.Class("NSAlert"), "new")
				m.Send(alert, "setMessageText:", m.NS("暂时无法打开"+appDisplayName))
				m.Send(alert, "setInformativeText:", m.NS(message))
				m.Send(alert, "addButtonWithTitle:", m.NS("好"))
				m.Send(alert, "runModal")
				m.Release(alert)
			}
			mygo.App.Quit()
		})
		_ = mygo.App.Run()
	}
	os.Exit(1)
}
func (a *application) media(ext string) string {
	return a.mediaFor(a.effectiveConfig().Scene, ext)
}

func (a *application) mediaFor(sceneID, ext string) string {
	scene, ok := a.catalog.Scene(sceneID)
	if !ok {
		a.recordMediaFailure(sceneID, ext, fmt.Errorf("场景 %s 尚无正式素材", media.SceneName(sceneID)))
		return ""
	}
	var role media.File
	switch ext {
	case ".png":
		role = scene.Background
	case ".mp4":
		role = scene.Entry
	case ".wav":
		role = scene.Work
	}
	path, err := media.Resolve(a.resources, role)
	a.recordMediaFailure(sceneID, ext, err)
	return path
}

func (a *application) recordMediaFailure(sceneID, ext string, err error) {
	key := sceneID + ext
	if err == nil {
		delete(a.mediaFailures, key)
		return
	}
	if a.mediaFailures == nil {
		a.mediaFailures = map[string]error{}
	}
	a.mediaFailures[key] = err
}

var mediaRoles = []struct{ ext, name string }{{".png", "背景／预览"}, {".mp4", "入场短片"}, {".wav", "背景音乐"}, {".nature", "白噪音"}, {".guide", "入场人声"}}

func (a *application) mediaFailureCaption() string {
	if len(a.mediaFailures) == 0 {
		return ""
	}
	return a.mediaFailureCaptionFor(a.effectiveConfig().Scene)
}
func (a *application) mediaFailureCaptionFor(sceneID string) string {
	var names []string
	for _, role := range mediaRoles {
		if a.mediaFailures[sceneID+role.ext] != nil {
			names = append(names, media.SceneName(sceneID)+"的"+role.name+"不可用")
		}
	}
	return strings.Join(names, "、")
}
func (a *application) fail(kind noticeKind, message string, e error) {
	a.notice = Notice{kind, message}
	if e != nil {
		a.diagnostic("operation", fmt.Sprintf("notice-%d", kind))
		fmt.Fprintln(os.Stderr, message+":", e)
	}
	a.refresh()
}
func (a *application) updatePreferences(change func(*store.Preferences)) bool {
	next := a.store.Data.Preferences
	next.Apps = append([]store.App{}, next.Apps...)
	change(&next)
	if e := a.store.SetPreferences(next); e != nil {
		fmt.Fprintln(os.Stderr, "save preferences:", e)
		if a.settingsNotice != 0 {
			showMessage(a.settingsNotice, preferenceSaveError)
		}
		return false
	}
	if a.settingsNotice != 0 {
		caption := settingsCaption
		if a.settingsPane() == 0 || a.settingsPane() == 1 {
			caption = "更改后自动保存。"
		}
		showMessage(a.settingsNotice, caption)
	}
	return true
}
func (a *application) persist() bool {
	s := a.controller.Snapshot()
	var err error
	if session.Exists(s.Phase) {
		err = a.store.Checkpoint(s)
	} else {
		err = a.store.Save()
	}
	if err != nil {
		a.diagnostic("checkpoint", "write-failed")
		a.controller.Pause()
		a.stopEnvironment()
		text := "无法保存专注记录，已暂停。请检查磁盘空间与访问权限。"
		if a.notice.Kind == noticeMedia && a.notice.Text != "" {
			text += "\n" + a.notice.Text
		} else if s.Reminder && time.Now().Before(a.reminderUntil) {
			text += "\n本次预设时间已到。"
		}
		a.notice = Notice{noticeStorage, text}
		if a.nativeReady {
			a.renderMain()
			a.openMain()
		}
		if !a.busy {
			a.effect(a.restore, nil)
		}
		return false
	}
	return true
}
func (a *application) effect(fn func() error, done func(error)) {
	if a.busy {
		return
	}
	a.busy = true
	a.refresh()
	go func() {
		e := fn()
		mygo.RunOnMain(func() {
			a.busy = false
			if done != nil {
				done(e)
			} else if e != nil {
				a.fail(noticeRecovery, "环境操作未完成", e)
			}
			if a.quitReady {
				return
			}
			if a.needsRestore && !a.busy {
				a.needsRestore = false
				a.effect(a.restore, nil)
			}
			a.refresh()
			if a.quitting && !a.busy {
				a.quit()
			}
			a.processOutside()
		})
	}()
}
func (a *application) waitHidden(r journal.Ref, hidden bool) error {
	return a.waitHiddenFor(r, hidden, nil)
}
func (a *application) waitHiddenFor(r journal.Ref, hidden bool, plan *protectionPlan, guard ...func() bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	_, e := probe.AwaitChange(ctx, func() error {
		var e error
		mygo.RunOnMain(func() {
			valid := a.preparationAllowed()
			if plan != nil {
				valid = a.planValid(plan)
			}
			if len(guard) > 0 {
				valid = valid && guard[0]()
			}
			if hidden && !valid {
				e = errHideCancelled
				return
			}
			a.expected[r.PID] = hidden
			e = m.Hide(r, hidden)
		})
		return e
	}, func() (bool, error) {
		var got bool
		var e error
		mygo.RunOnMain(func() { got, e = m.IsHidden(r) })
		return got == hidden, e
	}, tick.C)
	mygo.RunOnMain(func() { delete(a.expected, r.PID) })
	return e
}
func (a *application) hide(r journal.Ref) error {
	return a.hideFor(r, nil)
}
func (a *application) hideFor(r journal.Ref, plan *protectionPlan, guard ...func() bool) error {
	var hidden bool
	var e error
	mygo.RunOnMain(func() { hidden, e = m.IsHidden(r) })
	if e != nil || hidden {
		return e
	}
	if a.environmentRestored {
		if e = a.ignoreEnvironment(); e != nil {
			return protectionIssue("存储", "", e)
		}
	}
	id := fmt.Sprintf("hide-%d-%d", r.PID, time.Now().UnixNano())
	if e = a.journal.Begin(id, r, false); e != nil {
		return protectionIssue("存储", "", e)
	}
	a.environmentRestored = false
	if e = a.waitHiddenFor(r, true, plan, guard...); e != nil {
		if errors.Is(e, errHideCancelled) {
			if abandonErr := a.journal.Abandon(r, "cancelled before request"); abandonErr != nil {
				return protectionIssue("存储", "", abandonErr)
			}
			if len(guard) != 0 {
				return errHideCancelled
			}
			return nil
		}
		return fmt.Errorf("%s 未确认收起：%w", r.Label, e)
	}
	return protectionIssue("存储", "", a.journal.Applied(id))
}
func (a *application) protect() error {
	var plan *protectionPlan
	mygo.RunOnMain(func() {
		plan = a.newProtectionPlan(false, a.controller.Snapshot().AllowedIDs(), nil)
	})
	defer mygo.RunOnMain(func() {
		if a.preparing == plan {
			a.preparing = nil
		}
	})
	return a.protectPlan(plan)
}
func (a *application) protectPlan(plan *protectionPlan) error {
	if plan.immersion {
		if err := a.checkImmersionAccess(); err != nil {
			return protectionIssue("沉浸模式", "", err)
		}
	}
	if err := a.ignoreEnvironment(); err != nil {
		return protectionIssue("存储", "", err)
	}
	a.environmentRestored = false
	var refs []journal.Ref
	var allowed []string
	var listErr error
	mygo.RunOnMain(func() { refs, listErr = m.Running(); allowed = plan.allowed })
	var errs []error
	if listErr != nil && a.fixtureRoot == "" {
		for _, leaf := range protectionLeaves(listErr) {
			key := ""
			if item, ok := leaf.(*m.AppIdentityError); ok {
				key = item.Key()
			}
			errs = append(errs, protectionIssue("应用限制", key, leaf))
		}
		if !consentedApplications(errors.Join(errs...), plan.consent) {
			return errors.Join(errs...)
		}
	}
	for _, r := range refs {
		if !a.preparationValid(plan) {
			return errHideCancelled
		}
		if a.fixtureRoot != "" && !a.ownFixture(r) {
			continue
		}
		skip := m.SystemInteraction(r.BundleID)
		for _, id := range allowed {
			if id == r.BundleID {
				skip = true
			}
		}
		if !skip {
			if e := a.hideFor(r, plan); e != nil {
				var storage *protectionError
				if errors.As(e, &storage) {
					return errors.Join(append(errs, e)...)
				}
				errs = append(errs, protectionIssue("应用限制", fmt.Sprintf("%s:%d:%.6f:%s", r.BundleID, r.PID, r.Launched, e.Error()), e))
			}
		}
	}
	if !plan.immersion && consentedApplications(errors.Join(errs...), plan.consent) && a.preparationValid(plan) {
		if err := a.protectNotifications(plan.focusLinked, plan.focusProfile); err != nil {
			errs = append(errs, protectionIssue("通知联动", "", err))
		}
	}
	return errors.Join(errs...)
}

// End tracking without acting on a window or changing the system Focus mode.
func (a *application) ignoreEnvironment() error {
	if err := a.validateFixtureRecovery(); err != nil {
		return err
	}
	if a.fixtureRoot != "" {
		if a.notifications.Pending() {
			return errors.New("fixture recovery contains an out-of-scope record for notifications")
		}
		for _, e := range a.immersionJournal.Snapshot().Entries {
			if (e.Status == "pending" || e.Status == "applied") && !a.ownFixture(e.Ref) {
				return errors.New("fixture recovery contains an out-of-scope record for fullscreen")
			}
		}
	}
	var focusErr error
	if a.notifications.Pending() {
		focusErr = a.notifications.KeepCurrent()
	}
	const reason = "prior environment left unchanged"
	return errors.Join(a.journal.AbandonPending(reason), a.immersionJournal.AbandonPending(reason), focusErr)
}

func (a *application) restore() (result error) {
	if err := a.validateFixtureRecovery(); err != nil {
		return err
	}
	if a.environmentRestored {
		return a.ignoreEnvironment()
	}
	// An unsuccessful cleanup must not replay effects later in the same process.
	a.environmentRestored = true
	defer func() {
		for ref, w := range a.fullscreenLeases {
			w.Close()
			delete(a.fullscreenLeases, ref)
		}
		focusPending := a.notifications.Pending()
		result = errors.Join(result, a.ignoreEnvironment())
		if focusPending && !a.notifications.Pending() {
			a.publishNotifications("系统专注模式保持当前状态")
		}
	}()
	var errs []error
	if err := a.restoreImmersion(); err != nil {
		errs = append(errs, err)
	}
	for _, entry := range a.journal.Snapshot().Entries {
		if entry.Status != "applied" && entry.Status != "pending" {
			continue
		}
		r := entry.Ref
		var hidden bool
		var e error
		mygo.RunOnMain(func() { hidden, e = m.IsHidden(r) })
		if e != nil {
			if !errors.Is(e, m.ErrInstanceGone) && !errors.Is(e, m.ErrInstanceChanged) {
				errs = append(errs, fmt.Errorf("%s 身份暂不可核对，保留恢复责任：%w", r.Label, e))
				continue
			}
			if e = a.journal.Abandon(r, "instance ended or replaced; do not affect a replacement"); e != nil {
				errs = append(errs, e)
			}
			continue
		}
		if !hidden {
			if e = a.journal.Abandon(r, "already visible; retain external change"); e != nil {
				errs = append(errs, e)
			}
			continue
		}
		if entry.Status == "pending" {
			errs = append(errs, fmt.Errorf("%s 的收起归属未知，保留当前状态", r.Label))
			continue
		}
		if e = a.waitHidden(r, false); e == nil {
			e = a.journal.Release(r)
		}
		if e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}
func (a *application) workspace(r journal.Ref, event string) {
	if a.fixtureRoot != "" && !a.ownFixture(r) {
		return
	}
	if r.PID == os.Getpid() {
		return
	}
	if strings.Contains(event, "Unhide") || strings.Contains(event, "Hide") {
		hidden := !strings.Contains(event, "Unhide")
		if want, ok := a.expected[r.PID]; ok && want == hidden {
			delete(a.expected, r.PID)
		} else if !hidden {
			if e := a.journal.Abandon(r, "external unhide observed"); e != nil {
				a.fail(noticeStorage, "无法记录环境状态改变", nil)
			}
		}
	}
	if !session.Working(a.controller.Snapshot().Phase) {
		return
	}
	if strings.Contains(event, "Activate") {
		if a.controller.Allowed(r.BundleID) {
			a.lastWork = &r
			return
		}
		if m.SystemInteraction(r.BundleID) {
			return
		}
		a.queuedOutside = &r
		a.processOutside()
	}
}
func (a *application) processOutside() {
	if a.busy || a.quitting || a.quitReady || a.queuedOutside == nil {
		return
	}
	ref := *a.queuedOutside
	a.queuedOutside = nil
	s := a.controller.Snapshot()
	generation := a.protectionGeneration
	valid := func() bool {
		current := a.controller.Snapshot()
		return current.ID == s.ID && generation == a.protectionGeneration &&
			session.Working(current.Phase) && !a.controller.Allowed(ref.BundleID) && !a.quitting
	}
	active, err := m.Active(ref)
	if !valid() || err != nil || !active {
		return
	}
	stillActive := func() bool {
		active, err := m.Active(ref)
		return valid() && a.queuedOutside == nil && err == nil && active
	}
	var hideErr, handoffErr error
	a.effect(func() error {
		hideErr = a.hideFor(ref, nil, stillActive)
		if hideErr == nil {
			var shouldReturn bool
			mygo.RunOnMain(func() { shouldReturn = valid() && a.queuedOutside == nil })
			if shouldReturn {
				handoffErr = a.returnToWorkFor(s.AllowedIDs(), func() bool { return valid() && a.queuedOutside == nil })
			}
		}
		return nil
	}, func(error) {
		if !valid() || a.queuedOutside != nil || errors.Is(hideErr, errHideCancelled) {
			return
		}
		if _, err := m.Instance(ref); err != nil {
			return
		}
		a.outsideResult = "范围外应用已收起。加入本次不会改动模版。"
		if hideErr != nil {
			a.outsideResult = "未确认收起；可重试返回工作，或手动切回专注应用。"
			a.diagnostic("outside", "hide-unconfirmed")
		} else if handoffErr != nil {
			a.outsideResult = "应用已收起，系统未确认返回工作；可从 Dock 或 ⌘Tab 手动切回。"
			a.diagnostic("outside", "handoff-unconfirmed")
		}
		a.closeChoice()
		a.pending = &ref
		a.openChoice()
	})
}
func (a *application) start() {
	if a.busy || session.Exists(a.controller.Snapshot().Phase) {
		return
	}
	goal := strings.TrimSpace(m.Value(a.goal))
	if !a.validateStart(goal) {
		return
	}
	p := a.focusConfiguration()
	showMessage(a.appsError, "")
	a.startConfigured(goal, p, a.focusDraft.sourceID, a.focusDraft.sourceName)
}

func (a *application) startConfigured(goal string, p session.Config, sourceID, sourceName string) bool {
	return a.startConfiguredMode(goal, p, sourceID, sourceName, a.store.Data.Preferences.Immersion)
}

func (a *application) startConfiguredMode(goal string, p session.Config, sourceID, sourceName string, immersion bool) bool {
	if a.busy || a.mainModal() || session.Exists(a.controller.Snapshot().Phase) {
		return false
	}
	a.stopAudition()
	if len(a.catalog.Scenes) == 0 {
		a.fail(noticeInfo, "应用资源不完整，暂时无法开始。请重新安装应用后重试。", nil)
		return false
	}
	if err := a.validateFocusConfig(p); err != nil {
		a.fail(noticeInfo, err.Error(), nil)
		return false
	}
	if immersion && a.checkImmersionAccess() != nil {
		a.showConfigurationDetails("沉浸模式权限", func(r m.ID) {
			a.immersionPermissionView(r, "开始沉浸专注",
				func() { a.startConfiguredMode(goal, p, sourceID, sourceName, true) },
				func() { a.startConfiguredMode(goal, p, sourceID, sourceName, false) })
		})
		return false
	}
	a.ensureMain()
	handle := uintptr(0)
	if a.main.IsVisible() {
		handle = a.main.NativeHandle()
	}
	a.startDisplay = m.CaptureDisplay(handle)
	e := a.controller.Start(session.State{ID: fmt.Sprint(time.Now().UnixNano()), Goal: goal, Config: &p, TemplateID: sourceID, TemplateName: sourceName, Partial: true, Immersion: immersion})
	if e != nil {
		a.fail(noticeInfo, e.Error(), nil)
		return false
	}
	last := a.store.Data.Preferences
	lastUsed := p
	copyFocusEffects(&lastUsed, last.Config())
	last.SetConfig(lastUsed)
	if err := a.store.SetPreferences(last); err != nil {
		a.controller.Pause()
		a.fail(noticeStorage, preferenceSaveError, err)
		return false
	}
	a.notice = Notice{}
	a.entryAt = time.Time{}
	a.reminderUntil = time.Time{}
	a.reminderPlaybackFailed = false
	a.draftGoal = ""
	a.protectionFailure = nil
	a.acceptLimitedApps = false
	a.limitedAppConsent = nil
	if !a.persist() {
		return false
	}
	a.beginCountdown(a.prepareEntry)
	a.refresh()
	return true
}

func (a *application) prepareEntry() {
	plan := a.newProtectionPlan(false, a.controller.Snapshot().AllowedIDs(), nil)
	a.effect(func() error { return a.protectPlan(plan) }, func(e error) {
		valid := a.planValid(plan)
		a.preparing = nil
		if !valid {
			a.effect(a.restore, nil)
			a.renderMain()
			return
		}
		if a.controller.Snapshot().Phase == "ended" {
			a.effect(a.restore, nil)
			a.renderMain()
			a.openMain()
			return
		}
		if e != nil || a.controller.Snapshot().Phase != "entry" {
			a.protectionStopped(e)
			return
		}
		a.beginEntry()
	})
}
func (a *application) cancelEntry() {
	a.protectionGeneration++
	s := a.controller.Snapshot()
	if s.Phase != "entry" {
		return
	}
	a.draftGoal = s.Goal
	a.stopEnvironment()
	previous := a.store.Data.Current
	a.store.Data.Current = nil
	if err := a.store.Save(); err != nil {
		a.store.Data.Current = previous
		a.controller.Pause()
		a.notice = Notice{noticeStorage, "取消入场后无法保存状态，已暂停，请检查磁盘。"}
	} else {
		a.controller.End()
		a.notice = Notice{}
	}
	a.renderMain()
	if a.busy {
		a.needsRestore = true
	} else {
		a.effect(a.restore, nil)
	}
}
func (a *application) showsEntryScene() bool {
	cfg := a.effectiveConfig()
	return cfg.Background && cfg.Entry && !m.ReducedMotion()
}

func (a *application) beginEntry() {
	if a.quitPrompt {
		return
	}
	a.video.Stop()
	a.guide.Stop()
	a.entryAt = time.Time{}
	a.entryError = ""
	a.backgroundsOn()
	a.environmentSound()
	if a.showsEntryScene() {
		a.startEntryVideo(true)
	} else if a.playEntryGuide() {
		a.entryAt = time.Now()
		a.notice = Notice{noticeInfo, "正在播放入场人声，按 Esc 跳过。"}
		a.refresh()
	} else {
		a.enter()
	}
}

func (a *application) beginCountdown(done func()) {
	a.countdownDone = done
	a.countdownAt = time.Now()
	a.advanceCountdown(a.countdownAt)
}

func (a *application) advanceCountdown(at time.Time) {
	if at.IsZero() || a.countdownAt != at || a.controller.Snapshot().Phase != "entry" {
		return
	}
	if a.quitPrompt {
		return
	}
	elapsed := time.Since(at)
	if elapsed >= 3*time.Second {
		done := a.countdownDone
		a.stopCountdown()
		if done != nil {
			done()
		}
		return
	}
	step := elapsed / time.Second
	a.countdownOverlay.ShowCountdown(a.startDisplay, int(3-step))
	a.countdownTimer = time.AfterFunc(time.Until(at.Add((step+1)*time.Second)), func() {
		mygo.RunOnMain(func() { a.advanceCountdown(at) })
	})
}

func (a *application) stopCountdown() {
	a.countdownAt = time.Time{}
	a.countdownDone = nil
	a.countdownOverlay.Close()
	if a.countdownTimer != nil {
		a.countdownTimer.Stop()
		a.countdownTimer = nil
	}
}

func (a *application) startEntryVideo(playGuide bool) {
	a.video.Stop()
	a.guide.Stop()
	a.entryAt = time.Now()
	a.entryError = ""
	a.renderMain()
	if playGuide && a.entryError == "" {
		a.playEntryGuide()
	}
}

func (a *application) playEntryGuide() bool {
	p := a.effectiveConfig()
	if !p.Voice {
		return false
	}
	path, err := media.Resolve(a.resources, a.catalog.Guide)
	if err == nil {
		err = a.guide.PlayOnce(path, 0.55)
	}
	a.recordMediaFailure(p.Scene, ".guide", err)
	if err != nil {
		a.fail(noticeInfo, "入场引导未播放", err)
	}
	return err == nil
}

func (a *application) entryGuideOnly() bool {
	return a.video.Player == 0 && a.guide.Player != 0
}

func (a *application) enter() {
	if a.busy || a.controller.Snapshot().Phase != "entry" {
		return
	}
	a.stopCountdown()
	a.entryAt = time.Time{}
	a.video.Stop()
	a.restoreEntryPresentation()
	a.guide.Stop()
	a.handoffPreparedWork(false, nil, a.controller.Snapshot().AllowedIDs())
}

// A successful handoff is the work-entry commit point. Neither environment
// preparation nor launching/waiting for the work app advances the session clock.
func (a *application) handoffPreparedWork(resume bool, defaults, allowed []string) {
	before := a.controller.Snapshot()
	generation := a.protectionGeneration
	valid := func() bool {
		s := a.controller.Snapshot()
		return !a.quitting && s.ID == before.ID && s.Phase == before.Phase && generation == a.protectionGeneration
	}
	a.notice = Notice{noticeHandoffProgress, "正在返回专注应用，计时尚未开始…"}
	a.effect(func() error {
		if before.Immersion {
			return a.enterImmersiveWork(before, valid)
		}
		return a.returnToWorkFor(allowed, valid)
	}, func(err error) {
		if !valid() {
			a.effect(a.restore, nil)
			return
		}
		if err != nil {
			a.protectionStopped(protectionIssue("工作交接", "", err))
			return
		}
		if err = a.controller.CommitWork(resume, defaults, a.store.Checkpoint); err != nil {
			a.stopEnvironment()
			a.protectionFailure = protectionIssue("存储", "", err)
			a.fail(noticeStorage, "无法保存工作开始状态，计时保持暂停。请检查磁盘后重试。", err)
			a.renderMain()
			a.openMain()
			a.effect(a.restore, nil)
			return
		}
		if resume {
			a.reminderUntil = time.Time{}
		}
		a.notice = Notice{}
		if a.protectionFailure != nil {
			a.notice = Notice{noticeProtection, "当前按已确认的应用边界继续，系统通知仍可能出现。"}
		}
		a.backgroundsOn()
		a.environmentSound()
		a.renderMain()
		if a.main != nil {
			if a.goal != 0 && strings.TrimSpace(m.Value(a.goal)) == before.Goal {
				m.Text(a.goal, "")
			}
			a.main.Hide()
		}
		a.closeEntryPresentation()
		if a.control != nil {
			a.control.Hide()
		}
	})
}
func (a *application) activateWork() {
	previous := a.notice
	if previous.Text == "" || previous.Kind == noticeInfo {
		a.notice = Notice{noticeHandoffProgress, "正在返回专注应用…"}
	}
	a.refresh()
	a.effect(a.returnToWork, func(err error) {
		if err != nil && session.Working(a.controller.Snapshot().Phase) {
			a.fail(noticeHandoff, "尚未返回专注应用；可从 Dock 选择，或在设置中重新选择应用", err)
			a.openMain()
		} else if err == nil && session.Working(a.controller.Snapshot().Phase) && a.main != nil {
			a.main.Hide()
			if a.entry != nil {
				a.entry.Hide()
			}
			if a.control != nil {
				a.control.Hide()
			}
		}
		if err == nil && a.notice.Kind == noticeHandoffProgress {
			a.notice = previous
			a.refresh()
		}
	})
}
func (a *application) returnToWork() error {
	var allowed []string
	mygo.RunOnMain(func() { allowed = a.controller.Snapshot().AllowedIDs() })
	return a.returnToWorkFor(allowed, func() bool { return session.Working(a.controller.Snapshot().Phase) })
}
func (a *application) returnToWorkFor(allowed []string, valid func() bool) error {
	var target *journal.Ref
	var apps []store.App
	var err error
	var alreadyActive bool
	allowedID := func(id string) bool {
		for _, value := range allowed {
			if value == id {
				return true
			}
		}
		return false
	}
	choose := func() {
		var fallback *journal.Ref
		if a.lastWork != nil && allowedID(a.lastWork.BundleID) {
			if _, e := m.Instance(*a.lastWork); e == nil {
				r := *a.lastWork
				fallback = &r
			}
		}
		// Running returns individually verified refs even if an unrelated app
		// cannot be described. Only the selected target's identity is relevant.
		refs, _ := m.Running()
		for _, r := range refs {
			if a.fixtureRoot != "" && !a.ownFixture(r) {
				continue
			}
			if allowedID(r.BundleID) {
				r := r
				active, e := m.Active(r)
				if e != nil {
					continue
				}
				if active {
					target = &r
					a.lastWork = &r
					return
				}
				if fallback == nil {
					fallback = &r
				}
			}
		}
		target = fallback
		if target != nil {
			a.lastWork = target
		}
	}
	mygo.RunOnMain(func() {
		if !valid() {
			err = errors.New("session no longer active")
			return
		}
		choose()
		if target != nil {
			alreadyActive, err = m.Active(*target)
			if err != nil || alreadyActive {
				return
			}
		}
		for _, id := range allowed {
			if a.fixtureRoot != "" {
				for _, candidate := range a.effectiveConfig().Apps {
					if candidate.ID == id && a.ownFixture(journal.Ref{BundleID: candidate.ID, Path: candidate.Path}) {
						apps = append(apps, candidate)
					}
				}
				continue
			}
			if candidate, e := m.AppForID(id); e == nil {
				apps = append(apps, candidate)
				continue
			}
			for _, candidate := range a.effectiveConfig().Apps {
				if candidate.ID == id {
					apps = append(apps, candidate)
				}
			}
		}
		if target == nil && len(apps) == 0 {
			err = errors.New("本次专注应用已不可用，请在设置中重新选择应用")
			return
		}
		if target == nil {
			err = m.OpenApp(apps[0])
		}
	})
	if err != nil {
		return err
	}
	if alreadyActive {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	wait := func(request func() error, observe func() (bool, error)) error {
		_, e := probe.AwaitChange(ctx, request, observe, tick.C)
		return e
	}
	if target == nil {
		if err = wait(func() error { return nil }, func() (bool, error) {
			mygo.RunOnMain(func() {
				if !valid() {
					err = errHideCancelled
					return
				}
				choose()
			})
			return target != nil, err
		}); err != nil {
			return fmt.Errorf("未观察到专注应用启动：%w", err)
		}
	}
	permitted := func() error {
		var working bool
		mygo.RunOnMain(func() { working = valid() })
		if !working {
			return errors.New("session no longer active")
		}
		return nil
	}
	err = wait(func() error {
		var e error
		mygo.RunOnMain(func() {
			if !valid() {
				e = errHideCancelled
				return
			}
			choose()
			if target == nil {
				e = errors.New("专注应用已退出")
				return
			}
			if active, activeErr := m.Active(*target); activeErr != nil || active {
				e = activeErr
				return
			}
			e = m.Activate(*target)
		})
		return e
	}, func() (bool, error) {
		if e := permitted(); e != nil {
			return false, e
		}
		var active bool
		var e error
		mygo.RunOnMain(func() {
			choose()
			if target != nil {
				active, e = m.Active(*target)
			}
		})
		return active, e
	})
	if err != nil {
		return fmt.Errorf("未观察到专注应用返回前台：%w", err)
	}
	return nil
}
func (a *application) environmentSound() {
	p := a.effectiveConfig().IndependentAudio()
	s := a.controller.Snapshot()
	if !(s.Phase == "entry" || session.Working(s.Phase)) {
		a.stopSceneAudio()
		return
	}
	if a.audioSession != s.ID || a.audioScene != p.Scene {
		a.stopSceneAudio()
	}
	if !p.Sound || p.Volume == 0 {
		a.audio.Stop()
	} else if a.audio.Player == 0 {
		if e := a.audio.Play(a.mediaFor(p.Scene, ".wav"), 0); e != nil {
			a.recordMediaFailure(p.Scene, ".wav", e)
			a.fail(noticeMedia, media.SceneName(p.Scene)+"的背景音乐未播放", e)
		}
	}
	naturePath, natureID, err := a.ambiencePath(p)
	if err != nil {
		a.recordMediaFailure(p.Scene, ".nature", err)
		a.fail(noticeMedia, "所选白噪音未播放", err)
	}
	if natureID != a.natureID || naturePath == "" {
		a.nature.Stop()
		a.natureID = ""
	}
	if naturePath != "" && a.nature.Player == 0 {
		if err := a.nature.Play(naturePath, 0); err != nil {
			a.recordMediaFailure(p.Scene, ".nature", err)
			a.fail(noticeMedia, "所选白噪音未播放", err)
		} else {
			a.natureID = natureID
		}
	}
	if a.audioSession == s.ID && a.audioScene == p.Scene {
		// A repaired resource joins the existing session at its configured level.
		// The initial fade belongs only to a newly started session/environment.
		a.audio.Volume(p.Volume)
		a.nature.Volume(p.Ambience.Volume)
		return
	}
	a.audioSession, a.audioScene = s.ID, p.Scene
	generation := a.audioGeneration
	for step := 1; step <= 8; step++ {
		step := step
		time.AfterFunc(time.Duration(step)*100*time.Millisecond, func() {
			mygo.RunOnMain(func() {
				if a.audioGeneration == generation {
					a.audio.Volume(p.Volume * float64(step) / 8)
					a.nature.Volume(p.Ambience.Volume * float64(step) / 8)
				}
			})
		})
	}
}
func (a *application) stopSceneAudio() {
	a.audioGeneration++
	a.audio.Stop()
	a.nature.Stop()
	a.natureID = ""
	a.audioSession, a.audioScene = "", ""
}
func (a *application) entryVolume() {
	a.video.Volume(0)
}
func (a *application) pause(reason string) {
	a.protectionGeneration++
	if !session.Exists(a.controller.Snapshot().Phase) {
		return
	}
	a.controller.Pause()
	a.reminderUntil = time.Time{}
	a.acceptLimitedApps = false
	a.pending = nil
	a.queuedOutside = nil
	a.closeChoice()
	a.stopEnvironment()
	a.notice = Notice{noticeInfo, reason}
	a.persist()
	a.renderMain()
	if a.busy {
		a.needsRestore = true
		a.notice = Notice{noticeInfo, reason + "；正在完成环境操作"}
		return
	}
	a.effect(a.restore, nil)
}
func (a *application) pauseByUser(reason string) {
	if !session.Exists(a.controller.Snapshot().Phase) {
		return
	}
	a.pause(reason)
	if a.main != nil && (a.control == nil || !a.control.IsVisible()) {
		a.openMain()
	}
}
func (a *application) pauseForSystem(reason string) {
	if a.appPicker != nil {
		a.appPicker.Close()
	}
	if a.unknownSheet != nil {
		a.unknownSheet.Close()
		a.unknownSheet = nil
	}
	a.refreshNavigation()
	a.quitting = false
	a.pause(reason)
}
func (a *application) resume() {
	if a.controller.Snapshot().Phase == "paused" {
		a.openControl(false)
	}
}
func (a *application) resumeOrdinaryFocus() {
	if a.busy || a.mainModal() || a.controller.Snapshot().Phase != "paused" {
		return
	}
	if err := a.controller.UseOrdinaryFocus(a.store.Checkpoint); err != nil {
		a.fail(noticeStorage, preferenceSaveError, err)
		return
	}
	a.acceptLimitedApps = false
	a.hideMain()
	a.resumeWithConsent()
}

func (a *application) resumeWithConsent() {
	a.stopAudition()
	if a.busy || a.mainModal() || a.controller.Snapshot().Phase != "paused" {
		return
	}
	if a.controller.Snapshot().Config == nil {
		a.reviewLegacyConfiguration()
		return
	}
	defaults := make([]string, 0, len(a.effectiveConfig().Apps))
	for _, app := range a.effectiveConfig().Apps {
		defaults = append(defaults, app.ID)
	}
	allowed := append(append([]string{}, defaults...), a.controller.Snapshot().TemporaryAllowed...)
	if len(allowed) == 0 {
		a.fail(noticeInfo, "请至少选择一个专注应用", nil)
		return
	}
	if a.controller.Snapshot().Immersion {
		if err := a.checkImmersionAccess(); err != nil {
			a.protectionFailure = protectionIssue("沉浸模式", "", err)
			a.fail(noticeProtection, immersionPermissionPaused, nil)
			a.openFailureDetails()
			return
		}
	}
	a.notice = Notice{}
	consent := map[string]bool{}
	if a.acceptLimitedApps {
		consent = a.applicationConsent()
	}
	plan := a.newProtectionPlan(true, allowed, consent)
	a.effect(func() error { return a.protectPlan(plan) }, func(e error) {
		valid := a.planValid(plan)
		a.preparing = nil
		if !valid {
			a.effect(a.restore, nil)
			return
		}
		if !consentedApplications(e, plan.consent) {
			a.protectionStopped(e)
			return
		}
		a.protectionFailure = e
		a.limitedAppConsent = plan.consent
		if e != nil {
			a.notice = Notice{noticeProtection, "部分应用限制未生效，当前以可用保护继续。"}
		}
		a.handoffPreparedWork(true, defaults, plan.allowed)
	})
}
func (a *application) end() {
	a.protectionGeneration++
	a.reminderUntil = time.Time{}
	a.pending = nil
	a.queuedOutside = nil
	a.closeChoice()
	a.stopEnvironment()
	if session.Exists(a.controller.Snapshot().Phase) {
		v := a.controller.Snapshot()
		v.Phase = "ended"
		if e := a.store.Finish(v); e != nil {
			a.controller.Pause()
			a.fail(noticeStorage, "记录尚未保存，已暂停，请检查磁盘后重试退出", nil)
			a.renderMain()
			a.openMain()
			if a.busy {
				a.needsRestore = true
			} else {
				a.effect(a.restore, nil)
			}
			return
		}
		a.controller.End()
		if a.historyRoot != 0 {
			a.renderHistory()
		}
	}
	if a.busy {
		a.needsRestore = true
		a.notice = Notice{}
		a.renderMain()
		a.openMain()
		return
	}
	a.effect(a.restore, func(e error) {
		if e != nil {
			a.fail(noticeRecovery, "部分环境尚未恢复", e)
		} else {
			a.notice = Notice{}
		}
		a.renderMain()
		a.openMain()
	})
}
func (a *application) quit() {
	if a.quitPrompt {
		if a.main != nil {
			a.main.Show()
			a.main.Focus()
		}
		return
	}
	if !a.quitReady && !a.quitDiscarded && (a.focusIsDirty() || a.templateIsDirty() || a.notificationDraftDirty() || a.focusEffectsDirty) {
		if a.appPicker != nil {
			a.appPicker.Close()
		}
		if a.detail != nil {
			a.detail.Close()
		}
		a.ensureMain()
		a.main.Show()
		a.main.Focus()
		wasEntry := a.controller.Snapshot().Phase == "entry"
		countdownBeforePrompt := a.countdownAt
		promptStarted := time.Now()
		if a.countdownTimer != nil {
			a.countdownTimer.Stop()
			a.countdownOverlay.Close()
		}
		resumeEntry := func() {
			if !countdownBeforePrompt.IsZero() && a.countdownAt == countdownBeforePrompt {
				a.countdownAt = countdownBeforePrompt.Add(time.Since(promptStarted))
			}
			a.resumeEntryPresentation()
		}
		if wasEntry && a.entry != nil {
			a.entry.Hide()
		}
		a.quitPrompt = true
		sheet, err := m.NewAlertSheet(a.main.NativeHandle(), "退出并放弃未保存配置？", "未保存的本次调整、模版、专注和通知设置将被放弃。当前专注会在确认后按正常流程结束。", []string{"取消", "放弃并退出"}, func(i int) {
			a.unknownSheet = nil
			a.quitPrompt = false
			a.refreshNavigation()
			if i == 1 {
				a.quitDiscarded = true
				a.quit()
			} else if wasEntry {
				resumeEntry()
			}
		})
		if err != nil {
			a.quitPrompt = false
			a.fail(noticeInfo, "无法打开退出确认，请稍后重试。", err)
			if wasEntry {
				resumeEntry()
			}
			return
		}
		sheet.CancelFirst()
		a.unknownSheet = sheet
		a.refreshNavigation()
		return
	}

	a.protectionGeneration++
	a.stopAudition()
	if a.quitReady {
		return
	}
	a.pending = nil
	a.closeChoice()
	a.quitting = true
	if a.busy {
		a.controller.Pause()
		a.stopEnvironment()
		a.needsRestore = true
		a.refresh()
		return
	}
	a.stopEnvironment()
	if session.Exists(a.controller.Snapshot().Phase) {
		v := a.controller.Snapshot()
		v.Phase = "ended"
		if e := a.store.Finish(v); e != nil {
			a.quitting = false
			a.fail(noticeStorage, "无法保存记录，应用暂未退出", nil)
			a.openMain()
			return
		}
		a.controller.End()
	}
	a.effect(a.restore, func(e error) {
		continueQuit := a.quitting
		a.quitting = false
		if !continueQuit {
			if e != nil {
				a.fail(noticeRecovery, "环境未完全恢复", e)
			}
			return
		}
		if e != nil {
			a.diagnostic("quit", "environment-cleanup-incomplete")
			fmt.Fprintln(os.Stderr, "quit environment:", e)
		}
		a.quitReady = true
		if a.off != nil {
			a.off()
		}
		if a.offEscape != nil {
			a.offEscape()
		}
		close(a.stop)
		mygo.App.Quit()
	})
}
func (a *application) tick() {
	defer a.acceptanceState()
	s := a.controller.Snapshot()
	if s.Phase == "entry" && !a.quitPrompt && !a.busy && !a.entryAt.IsZero() && a.entryError == "" {
		if a.entryGuideOnly() {
			if !objc.Send[bool](a.guide.Player, m.S("isPlaying")) || time.Since(a.entryAt) >= 6*time.Second {
				a.enter()
			}
		} else {
			ready, err := a.video.State()
			if err != nil || (!ready && time.Since(a.entryAt) >= 6*time.Second) {
				a.entryError = "入场短片加载失败，可重试或跳过；目标与配置已保留。"
				a.guide.Stop()
				a.renderMain()
			} else if m.ReducedMotion() || (ready && a.video.Completed) {
				a.enter()
			}
		}
	}
	if a.controller.Tick() {
		a.notice = Notice{noticeInfo, "预设时间已到，可以继续，也可以暂停或退出。"}
		// The due event has already happened. A failed checkpoint must still
		// report it before the storage failure takes the session safely to pause.
		a.gentleReminder()
		if !a.persist() {
			return
		}
	}
	a.refresh()
	if session.Exists(s.Phase) && time.Now().Unix()%5 == 0 {
		a.persist()
	}
}
func (a *application) stopEnvironment() {
	a.stopCountdown()
	if a.unknownSheet != nil {
		a.unknownSheet.Close()
		a.unknownSheet = nil
	}
	a.stopSceneAudio()
	a.guide.Stop()
	a.video.Stop()
	a.closeEntryPresentation()
	a.counterOverlay.Close()
	for _, w := range a.backgrounds {
		w.Close()
	}
	a.backgrounds = nil
}

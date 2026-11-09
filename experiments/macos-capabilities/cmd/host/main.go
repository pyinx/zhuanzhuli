package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"focus.local/macos-capabilities/internal/fixture"
	"focus.local/macos-capabilities/internal/macos"
	"focus.local/macos-capabilities/internal/notificationaudit"
	"focus.local/macos-capabilities/internal/session"
	"focus.local/zhuanzhuli/pkg/journal"
	probe "focus.local/zhuanzhuli/pkg/observe"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type host struct {
	dir, token, run             string
	controller                  *session.Controller
	ledger                      *journal.Journal
	refs                        map[string]journal.Ref
	win, panel                  *mygo.Window
	choice                      *mygo.Window
	tray                        *mygo.Tray
	goal, minutes, status, mode string
	seq                         uint64
	busy                        bool
	partialChoice               bool
	partialConfirmed            bool
	readyToQuit                 bool
	webReady                    chan struct{}
	stopObserver                func()
	mu                          sync.Mutex // Serializes evidence appends, not session state.
	stop                        chan struct{}
	choiceDiagnostic            bool
	restorationDiagnostic       bool
	expected                    map[string]bool
}

// invalidateViews updates every live projection of shared session/busy state.
func (h *host) invalidateViews() {
	if h.win != nil {
		h.win.Invalidate()
	}
	if h.choice != nil {
		h.choice.Invalidate()
	}
}

func (h *host) event(kind string, value any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(h.dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("evidence: %v", err)
		return
	}
	defer f.Close()
	if err = json.NewEncoder(f).Encode(map[string]any{"at": time.Now().UTC(), "kind": kind, "value": value}); err != nil {
		log.Printf("evidence: %v", err)
	}
}
func (h *host) dispatch(kind string) error {
	if kind == "tick" {
		h.controller.Tick()
		if h.win != nil {
			h.invalidateViews()
		}
		return nil
	}
	before := h.controller.Snapshot()
	h.seq++
	minutes, err := strconv.Atoi(h.minutes)
	if err != nil && kind == "start" {
		return errors.New("invalid: 请输入 1–180 的整数分钟")
	}
	_, err = h.controller.Handle(session.Command{RunID: h.run, ID: fmt.Sprint(h.seq), Revision: h.controller.Snapshot().Revision, Kind: kind, Goal: h.goal, Minutes: minutes, Target: "B"})
	if err == nil && kind != "tick" {
		s := h.controller.Snapshot()
		s.Goal = ""
		h.event("session", map[string]any{"command": kind, "state": s})
	}
	if h.win != nil {
		h.invalidateViews()
	}
	after := h.controller.Snapshot()
	if err == nil && needsChoice(before, after) {
		h.presentChoice()
	}
	if err == nil && !after.PendingChoice && h.choice != nil {
		w := h.choice
		h.choice = nil
		w.Close()
	}
	return err
}
func (h *host) action(kind string) error {
	if h.busy {
		h.status = "正在处理上一项操作"
		return errors.New("conflict: operation pending")
	}
	if (kind == "start" || kind == "resume") && h.ledger.Pending() > 0 {
		h.status = "存在未释放改动，请先清理"
		return errors.New("restore-failed: unresolved journal")
	}
	if kind == "start" && !h.partialConfirmed {
		h.partialChoice = true
		h.status = "通知保护未验证。确认后仅以部分保护进入测试。"
		h.invalidateViews()
		return errors.New("conflict: explicit partial protection consent required")
	}
	if err := h.dispatch(kind); err != nil {
		h.status = err.Error()
		return err
	}
	h.partialConfirmed = false
	switch kind {
	case "entry-complete":
		h.launchEffect(func() error { return h.hideB() })
	case "return":
		h.launchEffect(func() error {
			if err := h.hideB(); err != nil {
				return err
			}
			return h.activateA()
		})
	case "pause", "end", "allow":
		h.launchEffect(h.cleanup)
	case "resume":
		if h.controller.Snapshot().AllowB {
			h.launchEffect(h.cleanup)
		} else {
			h.launchEffect(h.hideB)
		}
	default:
		h.status = "已接受：" + kind
	}
	return nil
}
func (h *host) launchEffect(fn func() error) {
	h.busy = true
	h.status = "等待请求与实际状态核对"
	h.invalidateViews()
	go func() {
		err := fn()
		mygo.RunOnMain(func() {
			h.busy = false
			if err != nil {
				h.status = err.Error()
			} else {
				h.status = "已观察并记录；通知保护仍未验证"
			}
			if h.win != nil {
				h.invalidateViews()
			}
		})
	}()
}
func (h *host) inspect(r journal.Ref) (macos.State, error) {
	var s macos.State
	var err error
	mygo.RunOnMain(func() { s, err = macos.Inspect(r) })
	return s, err
}
func (h *host) request(r journal.Ref, hidden bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Each observation crosses onto the main run loop; never spin on cached values.
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	ack, err := probe.AwaitChange(ctx, func() error {
		var e error
		mygo.RunOnMain(func() {
			h.expected[r.Label] = hidden
			e = macos.RequestHidden(r, hidden)
			if e != nil && !errors.Is(e, probe.Unacknowledged) {
				delete(h.expected, r.Label)
			}
		})
		return e
	}, func() (bool, error) { s, e := h.inspect(r); return s.Hidden == hidden, e }, tick.C)
	h.event("hidden-result", map[string]any{"fixture": r.Label, "expectedHidden": hidden, "acknowledged": ack, "observed": err == nil})
	return err
}
func (h *host) hideB() error {
	r := h.refs["B"]
	s, err := h.inspect(r)
	if err != nil {
		return err
	}
	if s.Hidden {
		h.event("already-hidden", map[string]string{"fixture": "B"})
		return nil
	}
	id := fmt.Sprintf("hide-%d", time.Now().UnixNano())
	if err = h.ledger.Begin(id, r, false); err != nil {
		return fmt.Errorf("journal before effect: %w", err)
	}
	if err = h.request(r, true); err != nil {
		return err
	}
	return h.ledger.Applied(id)
}
func (h *host) activateA() error {
	// A nonactivating choice panel does not make the host the active app.
	// The explicit Return action must first acquire activation before handing
	// it to A; background requests are allowed to be refused by macOS.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	_, err := probe.AwaitChange(ctx, func() error {
		mygo.RunOnMain(mygo.App.Focus)
		return nil
	}, func() (bool, error) {
		var active bool
		mygo.RunOnMain(func() { active = macos.SelfState().Active })
		return active, nil
	}, tick.C)
	h.event("return-host-activation", map[string]bool{"observed": err == nil})
	if err != nil {
		return fmt.Errorf("host activation not observed: %w", err)
	}
	return h.activateFixture("A")
}
func (h *host) activateFixture(label string) error {
	r := h.refs[label]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	ack, err := probe.AwaitChange(ctx, func() error { var e error; mygo.RunOnMain(func() { e = macos.RequestActivate(r) }); return e }, func() (bool, error) { s, e := h.inspect(r); return s.Active, e }, tick.C)
	h.event("activation-result", map[string]any{"fixture": label, "acknowledged": ack, "observed": err == nil})
	return err
}

func (h *host) cleanup() error {
	r := h.refs["B"]
	if h.ledger.HasUnknown(r) {
		return errors.New("restore-failed: pending request has unknown outcome; do not assume visible means cleaned")
	}
	s, err := h.inspect(r)
	if err != nil {
		if h.ledger.Pending() > 0 {
			return fmt.Errorf("restore-failed: %w", err)
		}
		return nil
	}
	if h.ledger.CanRestore(r, s.Hidden) {
		if err = h.request(r, false); err != nil {
			return fmt.Errorf("restore-failed: %w", err)
		}
		if err = h.ledger.Release(r); err != nil {
			return err
		}
	} else if !s.Hidden {
		if err = h.ledger.Abandon(r, "already visible or externally changed"); err != nil {
			return err
		}
	}
	if h.ledger.Pending() > 0 {
		return errors.New("restore-failed: unknown pending change; inspect journal before another run")
	}
	h.event("cleanup", map[string]any{"pending": h.ledger.Pending()})
	return nil
}
func (h *host) stopAll() {
	if h.busy {
		h.status = "请等待当前系统操作结束，再停止"
		return
	}
	h.busy = true
	h.invalidateViews()
	go func() {
		err := h.cleanup()
		if err == nil {
			err = journal.WriteJSON(filepath.Join(h.dir, "stop.json"), map[string]any{"token": h.token, "stop": true})
		}
		mygo.RunOnMain(func() {
			h.busy = false
			if err != nil {
				h.status = err.Error()
				h.invalidateViews()
				return
			}
			h.event("stopped", map[string]bool{"clean": true})
			h.readyToQuit = true
			mygo.App.Quit()
		})
	}()
}
func (h *host) view(c *ui.Context) {
	c.OnShortcut(ui.Cmd, ui.KeyQ, h.stopAll)
	s := h.controller.Snapshot()
	ui.Column(c).Fill().Padding(24).Gap(12).Children(func() {
		ui.Text(c, "专注力 · 能力验证").FontSize(22).Bold()
		ui.Text(c, "独立测试窗口 · "+h.mode+" · 通知保护未验证").FontSize(12)
		ui.TextInput(c, &h.goal).Label("合成专注目标").Placeholder("仅输入测试文字")
		ui.Row(c).Gap(8).Children(func() {
			for _, n := range []string{"15", "30", "60"} {
				n := n
				if ui.Button(c, n+" 分钟").Clicked() {
					h.minutes = n
				}
			}
			ui.TextInput(c, &h.minutes).Width(80).Label("自定义分钟")
		})
		ui.Textf(c, "状态 %s · 有效 %d 秒 · 到点提醒 %d 次", s.Phase, int(s.Elapsed/time.Second), s.Reminders)
		ui.Text(c, h.status).FontSize(12)
		ui.Row(c).Gap(8).Children(func() {
			for _, b := range []struct{ title, kind string }{{"开始测试", "start"}, {"跳过验证入场", "entry-complete"}, {"暂停", "pause"}, {"继续", "resume"}, {"结束测试", "end"}} {
				b := b
				if ui.Button(c, b.title).Disabled(h.busy).Clicked() {
					h.action(b.kind)
				}
			}
		})
		if h.partialChoice && (s.Phase == "idle" || s.Phase == "ended") {
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "仅以部分保护继续").Clicked() {
					h.partialConfirmed = true
					h.action("start")
				}
				if ui.Button(c, "取消开始").Clicked() {
					h.partialChoice = false
					h.partialConfirmed = false
					h.status = "已取消"
				}
			})
		}
		if s.PendingChoice {
			ui.Row(c).Gap(8).Children(func() {
				for _, b := range []struct{ title, kind string }{{"返回", "return"}, {"加入本次", "allow"}, {"退出专注", "end"}} {
					b := b
					if ui.Button(c, b.title).Clicked() {
						h.action(b.kind)
					}
				}
			})
		}
		ui.Divider(c)
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "切换到 WebView 对照").Clicked() {
				h.open("web")
			}
			if ui.Button(c, "停止并清理测试程序").Clicked() {
				h.stopAll()
			}
		})
		ui.Text(c, "锁屏、睡眠、VoiceOver、通知横幅与声音等待人工观察。关闭窗口不会结束测试。").FontSize(11)
	})
}

// Probe exposes only validated commands/snapshots to this app's own minimal page.
type Probe struct{ h *host }

func (p *Probe) State() session.View {
	var s session.View
	mygo.RunOnMain(func() { s = p.h.controller.Snapshot() })
	return s
}
func (p *Probe) Act(kind string) error {
	allowed := map[string]bool{"entry-complete": true, "pause": true, "resume": true, "end": true, "return": true, "allow": true}
	if !allowed[kind] {
		return errors.New("invalid: command")
	}
	var err error
	mygo.RunOnMain(func() { err = p.h.action(kind) })
	return err
}
func (p *Probe) Start(goal string, minutes int, partial bool) error {
	if !partial || len(goal) > 800 || minutes < 1 || minutes > 180 {
		return errors.New("invalid: explicit partial protection consent, goal and minutes required")
	}
	var err error
	mygo.RunOnMain(func() {
		p.h.goal = goal
		p.h.minutes = strconv.Itoa(minutes)
		p.h.partialConfirmed = true
		err = p.h.action("start")
	})
	return err
}
func (p *Probe) Native() { mygo.RunOnMain(func() { p.h.open("native") }) }
func (p *Probe) Stop()   { mygo.RunOnMain(p.h.stopAll) }

func (h *host) open(mode string) {
	old := h.win
	h.mode = mode
	if mode == "web" {
		h.win = mygo.NewWindow(mygo.WindowOptions{Title: "专注力技术验证 · WebView", Width: 700, Height: 490})
		h.webReady = make(chan struct{})
		ready := h.webReady
		var once sync.Once
		h.win.OnReadyToShow(func() { once.Do(func() { close(ready) }) })
		h.win.Page().LoadHTML(webPage, "")
		h.win.Page().OnWillNavigate(func(e *mygo.NavigateEvent) {
			if e.URL != "about:blank" {
				e.PreventDefault()
			}
		})
	} else {
		h.win = mygo.NewWindow(mygo.WindowOptions{Title: "专注力技术验证 · Native UI", Width: 760, Height: 490, Content: ui.View(h.view)})
	}
	if old != nil {
		old.Close()
	}
	h.event("window-created", map[string]string{"mode": mode})
}
func (h *host) auto() {
	result := map[string]any{"schemaVersion": 1, "evidenceKind": "automated-real", "controllerInput": "scripted test commands, not human choices"}
	defer func() {
		if err := journal.WriteJSON(filepath.Join(h.dir, "automated.json"), result); err != nil {
			log.Printf("result: %v", err)
		}
	}()
	beforeA, err := fixture.ReadState(filepath.Join(h.dir, "A.json"), h.token)
	if err != nil {
		result["error"] = err.Error()
	}
	beforeB, err := fixture.ReadState(filepath.Join(h.dir, "B.json"), h.token)
	if err != nil {
		result["error"] = err.Error()
	}
	if result["error"] == nil {
		err = h.hideB()
		result["hideB"] = err == nil
		if err != nil {
			result["error"] = err.Error()
		}
		if err == nil {
			// Wait for actual heartbeat progress while hidden, not an arbitrary screenshot delay.
			deadline := time.After(30 * time.Second)
			tick := time.NewTicker(100 * time.Millisecond)
		progress:
			for {
				select {
				case <-deadline:
					err = errors.New("timeout: background progress")
					break progress
				case <-tick.C:
					a, ea := fixture.ReadState(filepath.Join(h.dir, "A.json"), h.token)
					b, eb := fixture.ReadState(filepath.Join(h.dir, "B.json"), h.token)
					if ea == nil && eb == nil && a.Counter > beforeA.Counter && b.Counter > beforeB.Counter && b.Hidden {
						result["backgroundProgress"] = true
						result["counts"] = []uint64{beforeA.Counter, a.Counter, beforeB.Counter, b.Counter}
						break progress
					}
				}
			}
			tick.Stop()
		}
		if err != nil {
			result["error"] = err.Error()
		}
	}
	cleanErr := h.cleanup()
	result["cleanup"] = cleanErr == nil
	result["journalPending"] = h.ledger.Pending()
	if cleanErr != nil {
		result["cleanupError"] = cleanErr.Error()
	}
	if h.restorationDiagnostic && cleanErr == nil {
		r := h.diagnoseRestoration()
		result["restoration"] = r
		if r["error"] != nil || r["setupCleanupError"] != nil {
			result["error"] = "restoration diagnostic failed; inspect recorded stage"
			return
		}
	}
	// Real independent windows only; no old HTML prototype is loaded or inspected.
	var ready chan struct{}
	mygo.RunOnMain(func() { h.open("web"); ready = h.webReady })
	select {
	case <-ready:
		result["webReady"] = true
	case <-time.After(30 * time.Second):
		result["webReady"] = false
		result["webError"] = "page ready timeout"
	}
	mygo.RunOnMain(func() { h.open("native"); h.status = "自动系统采样结束；人工步骤仍未完成" })
	if h.choiceDiagnostic {
		h.diagnoseChoice()
	}
}

func main() {
	run := flag.String("run", "", "run ID")
	token := flag.String("token", "", "token")
	auto := flag.Bool("auto", false, "run approved fixture sampling")
	choiceDiagnostic := flag.Bool("choice-diagnostic", false, "measure owned panel after real fixture activation")
	notificationAudit := flag.Bool("notification-audit", false, "read this app's notification/Focus facts without permission requests")
	restorationDiagnostic := flag.Bool("restoration-diagnostic", false, "exercise owned baseline, repeat cleanup and synthetic external event")
	capture := flag.Bool("capture-check", false, "read capture authorization without prompting")
	owner := flag.Int("owner", 0, "read owner PID of one window")
	flag.Parse()
	if *capture {
		ok, err := macos.CaptureAllowed()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(ok)
		return
	}
	if *owner > 0 {
		pid, err := macos.Owner(*owner)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(pid)
		return
	}
	if len(*token) != 32 {
		log.Fatal("invalid: run token")
	}
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	base := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(exe))))
	dir, err := fixture.RunDir(filepath.Join(base, "..", "..", ".runs"), *run)
	if err != nil {
		log.Fatal(err)
	}
	if *notificationAudit {
		if err := notificationaudit.Run(dir, *token, "Host"); err != nil {
			log.Fatal(err)
		}
		return
	}
	ledger, err := journal.Open(filepath.Join(dir, "journal.json"))
	if err != nil {
		log.Fatal(err)
	}
	h := &host{dir: dir, token: *token, run: *run, ledger: ledger, controller: session.New(*run, time.Now), refs: map[string]journal.Ref{}, goal: "合成目标", minutes: "30", status: "通知保护未验证；先完成系统采样，再由用户选择测试", stop: make(chan struct{}), expected: map[string]bool{}}
	h.choiceDiagnostic = *choiceDiagnostic
	h.restorationDiagnostic = *restorationDiagnostic
	for _, label := range []string{"A", "B"} {
		s, err := fixture.ReadState(filepath.Join(dir, label+".json"), *token)
		if err != nil {
			log.Fatal(err)
		}
		h.refs[label] = s.Ref
	}
	mygo.Bind(&Probe{h})
	mygo.App.SetName("Focus Capability Probe")
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(visible bool) {
		if !visible {
			h.open("native")
		}
	})
	mygo.App.WhenReady(func() {
		for _, r := range h.refs {
			if _, err := macos.Inspect(r); err != nil {
				log.Fatal(err)
			}
		}
		self, err := macos.Self("Host", *token)
		if err != nil {
			log.Fatal(err)
		}
		h.open("native")
		menu := mygo.NewMenu([]*mygo.MenuItem{{Label: "显示验证主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { h.open("native") }}, {Label: "查看待处理选择", Click: func(*mygo.MenuItem, *mygo.Window) { h.presentChoice() }}, {Label: "暂停测试", Click: func(*mygo.MenuItem, *mygo.Window) { h.action("pause") }}, {Label: "继续测试", Click: func(*mygo.MenuItem, *mygo.Window) { h.action("resume") }}, {Label: "停止并清理", Click: func(*mygo.MenuItem, *mygo.Window) { h.stopAll() }}})
		h.tray, err = mygo.NewTray(mygo.TrayOptions{Title: "◉", ToolTip: "专注力能力验证", Menu: menu})
		if err != nil {
			h.event("tray-error", err.Error())
		}
		h.stopObserver, err = macos.Observe([]journal.Ref{h.refs["A"], h.refs["B"]}, func(label, name string) {
			h.event("workspace", map[string]string{"fixture": label, "event": name})
			if strings.Contains(name, "Unhide") || strings.Contains(name, "Hide") {
				hidden := !strings.Contains(name, "Unhide")
				if wanted, ok := h.expected[label]; ok && wanted == hidden {
					delete(h.expected, label)
				} else {
					r := h.refs[label]
					go func() {
						if err := h.ledger.Abandon(r, "external visibility event"); err != nil {
							h.event("journal-error", err.Error())
						}
					}()
				}
			}
			if label == "B" && strings.Contains(name, "Activate") {
				if err := h.dispatch("outside"); err != nil {
					h.status = err.Error()
				}
			}
		})
		if err != nil {
			h.event("observer-error", err.Error())
		}
		for _, p := range []struct {
			name, kind string
			on         func(func()) func()
		}{{"suspend", "suspend", mygo.Power.OnSuspend}, {"resume", "wake", mygo.Power.OnResume}, {"lock", "lock", mygo.Power.OnLockScreen}, {"unlock", "unlock", mygo.Power.OnUnlockScreen}} {
			p := p
			p.on(func() {
				h.event("power", p.name)
				if err := h.dispatch(p.kind); err != nil {
					h.status = err.Error()
				}
				if p.kind == "suspend" || p.kind == "lock" {
					if !h.busy {
						h.launchEffect(h.cleanup)
					}
				}
			})
		}
		go func() {
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				if fixture.StopRequested(filepath.Join(dir, "host-stop.json"), h.token) {
					mygo.RunOnMain(func() {
						if !h.busy {
							h.stopAll()
						}
					})
				}
				var state fixture.State
				mygo.RunOnMain(func() {
					state = fixture.State{Schema: 1, Ref: self, Windows: macos.Windows(h.win.NativeHandle())}
					if h.choice != nil {
						state.Windows = append(state.Windows, macos.Windows(h.choice.NativeHandle())...)
					}
					if err := h.dispatch("tick"); err != nil {
						h.status = err.Error()
					}
				})
				if err := journal.WriteJSON(filepath.Join(dir, "Host.json"), state); err != nil {
					log.Printf("handshake: %v", err)
				}
				select {
				case <-h.stop:
					return
				case <-tick.C:
				}
			}
		}()
		if *auto {
			h.busy = true
			h.invalidateViews()
			go func() { h.auto(); mygo.RunOnMain(func() { h.busy = false; h.invalidateViews() }) }()
		}
	})
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !h.readyToQuit {
			e.PreventDefault()
			h.stopAll()
		}
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if h.stopObserver != nil {
			h.stopObserver()
		}
		if h.tray != nil {
			h.tray.Destroy()
		}
	})
	mygo.App.OnQuit(func() { close(h.stop) })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

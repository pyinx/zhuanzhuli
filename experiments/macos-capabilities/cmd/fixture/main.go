package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"focus.local/macos-capabilities/internal/fixture"
	"focus.local/macos-capabilities/internal/macos"
	"focus.local/macos-capabilities/internal/notificationaudit"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	run := flag.String("run", "", "run ID")
	token := flag.String("token", "", "run token")
	label := flag.String("label", "", "A, B or C")
	notificationAudit := flag.Bool("notification-audit", false, "read only this app's notification/Focus facts")
	flag.Parse()
	if (*label != "A" && *label != "B" && *label != "C") || len(*token) != 32 {
		log.Fatal("invalid: fixture arguments")
	}
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	bundles := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(exe))))
	dir, err := fixture.RunDir(filepath.Join(bundles, "..", "..", ".runs"), *run)
	if err != nil {
		log.Fatal(err)
	}
	if *notificationAudit {
		if err := notificationaudit.Run(dir, *token, *label); err != nil {
			log.Fatal(err)
		}
		return
	}
	var win *mygo.Window
	var count uint64
	text := "这是合成文字，不是工作内容"
	status := "通知未触发；点击后可能需要你选择授权"
	var notes []*mygo.Notification
	stop := make(chan struct{})
	mygo.App.SetName("Focus Fixture " + *label)
	mygo.App.WhenReady(func() {
		ref, err := macos.Self(*label, *token)
		if err != nil {
			log.Fatal(err)
		}
		win = mygo.NewWindow(mygo.WindowOptions{Title: "技术验证夹具 " + *label, Width: 420, Height: 300, Content: ui.View(func(c *ui.Context) {
			ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
				ui.Text(c, "自有测试程序 "+*label).FontSize(20).Bold()
				ui.Textf(c, "后台进度：%d", count)
				ui.TextInput(c, &text).Label("合成文字")
				ui.Text(c, status).FontSize(12)
				if ui.Button(c, "发送测试通知").Clicked() {
					status = "等待系统通知授权或发送结果"
					n := mygo.NewNotification(mygo.NotificationOptions{ID: *run + "/" + *label + fmt.Sprint(count), Title: "专注力技术验证 · " + *label, Body: "这是一条合成测试通知", Silent: false})
					notes = append(notes, n)
					go func() {
						type authorization struct {
							granted bool
							err     error
						}
						answer := make(chan authorization, 1)
						mygo.RunOnMain(func() {
							macos.RequestFixtureNotificationPermission(func(g bool, e error) { answer <- authorization{g, e} })
						})
						var auth authorization
						select {
						case auth = <-answer:
						case <-time.After(60 * time.Second):
							auth.err = fmt.Errorf("notification authorization timed out")
						}
						err := auth.err
						if err == nil && !auth.granted {
							err = fmt.Errorf("notification authorization not granted")
						}
						if err == nil {
							err = n.Show()
						}
						result := map[string]any{"schemaVersion": 1, "fixture": *label, "requestID": n.ID(), "evidenceKind": "automated-real", "requested": err == nil, "popup": "untested", "sound": "untested"}
						result["authorizationGranted"] = auth.granted
						if auth.err != nil {
							result["authorizationError"] = auth.err.Error()
						}
						if err != nil {
							result["error"] = err.Error()
						}
						if writeErr := journal.WriteJSON(filepath.Join(dir, *label+"-notification.json"), result); writeErr != nil {
							log.Printf("notification evidence: %v", writeErr)
						}
						mygo.RunOnMain(func() {
							macos.NotificationFacts(func(facts map[string]any, auditErr error) {
								report := map[string]any{"facts": facts, "requestID": n.ID()}
								if auditErr != nil {
									report["error"] = auditErr.Error()
								}
								if e := journal.WriteJSON(filepath.Join(dir, *label+"-notification-settings.json"), report); e != nil {
									log.Printf("notification settings evidence: %v", e)
								}
							})
							if err != nil {
								status = err.Error()
							} else {
								status = "请求已提交；请分别观察横幅与声音"
							}
							win.Invalidate()
						})
					}()
				}
				if ui.Button(c, "清理本次测试通知").Clicked() {
					for _, n := range notes {
						n.Close()
					}
					notes = nil
					status = "已请求清理本次通知；未触碰其他应用通知"
				}
				if ui.Button(c, "退出夹具").Clicked() {
					mygo.App.Quit()
				}
			})
		})})
		// CUA can reach controls in a hidden window without making the app
		// frontmost. Use a real self-activation request for outside-app UI tests;
		// this fixture menu neither calls the host nor changes its session state.
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Label: "测试", Submenu: []*mygo.MenuItem{
				{Label: "显示并激活此夹具", Click: func(*mygo.MenuItem, *mygo.Window) { win.Show() }},
				{Label: "切换此夹具全屏", Click: func(*mygo.MenuItem, *mygo.Window) { win.ToggleFullScreen() }},
			}},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if b, err := os.ReadFile(filepath.Join(dir, "stop.json")); err == nil {
					var request struct {
						Token string `json:"token"`
						Stop  bool   `json:"stop"`
					}
					if json.Unmarshal(b, &request) == nil && request.Token == *token && request.Stop {
						mygo.App.Quit()
						return
					}
				}
				var state fixture.State
				mygo.RunOnMain(func() {
					count++
					s := macos.SelfState()
					state = fixture.State{Schema: 1, Ref: ref, Counter: count, Hidden: s.Hidden, Active: s.Active, Windows: macos.Windows(win.NativeHandle())}
					win.Invalidate()
				})
				if err := journal.WriteJSON(filepath.Join(dir, *label+".json"), state); err != nil {
					log.Printf("handshake: %v", err)
				}
				select {
				case <-stop:
					return
				case <-ticker.C:
				}
			}
		}()
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		for _, n := range notes {
			n.Close()
		}
	})
	mygo.App.OnQuit(func() { close(stop) })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

//go:build darwin

package main

import (
	"fmt"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"github.com/egoist/mygo"
	"os"
	"strings"
)

func (a *application) menu() {
	item := func(label, key string, fn func()) *mygo.MenuItem {
		menu := &mygo.MenuItem{Label: label, Accelerator: key, Click: func(*mygo.MenuItem, *mygo.Window) { fn() }}
		if key == "Cmd+," || key == "Cmd+1" || key == "Cmd+2" || key == "Cmd+Y" {
			a.pageMenus = append(a.pageMenus, menu)
		}
		return menu
	}
	a.pauseMenu = item("暂停", "Cmd+Shift+P", func() { a.pauseByUser("已暂停") })
	a.resumeMenu = item("恢复…", "Cmd+Shift+R", a.resume)
	a.endMenu = item("结束本次专注", "Cmd+Shift+E", a.end)
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: appDisplayName, Submenu: []*mygo.MenuItem{{Role: mygo.RoleAbout}, item("设置…", "Cmd+,", a.openSettings), mygo.Separator(), {Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide}, mygo.Separator(), item("退出"+appDisplayName, "Cmd+Q", a.quit)}},
		{Label: "编辑", Submenu: []*mygo.MenuItem{item("撤销", "Cmd+Z", a.undoCommand), {Role: mygo.RoleRedo}, mygo.Separator(), {Role: mygo.RoleCut}, {Role: mygo.RoleCopy}, {Role: mygo.RolePaste}, {Role: mygo.RoleSelectAll}}},
		{Label: "专注", Submenu: []*mygo.MenuItem{item("显示"+appDisplayName, "Cmd+1", a.openMain), item("本次控制", "Cmd+2", func() { a.openControl(false) }), item("查看范围外选择", "", a.showChoice), a.pauseMenu, a.resumeMenu, a.endMenu, item("模版", "", func() { a.showPage(pageTemplates) }), item("统计", "Cmd+Y", a.openHistory)}},
		{Label: "窗口", Role: mygo.RoleWindowMenu, Submenu: []*mygo.MenuItem{{Role: mygo.RoleClose}, {Role: mygo.RoleMinimize}, {Role: mygo.RoleFront}}},
	}))
	t, e := mygo.NewTray(mygo.TrayOptions{Title: "◉", ToolTip: appDisplayName})
	if e != nil {
		a.notice = Notice{noticeInfo, "菜单栏入口暂不可用；可从 Dock 返回"}
	} else {
		a.tray = t
		if png, err := m.SymbolPNG("leaf"); err == nil {
			if err = t.SetIcon(png, true); err == nil {
				a.trayTemplate = true
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "tray template:", err)
			}
		} else {
			fmt.Fprintln(os.Stderr, "tray symbol:", err)
		}
		t.OnClick(func() {
			if a.busy && a.controller.Snapshot().Phase == "entry" {
				return
			}
			if id := a.store.Data.Preferences.QuickTemplate; id != "" && !a.busy && !session.Exists(a.controller.Snapshot().Phase) {
				a.startSavedTemplate(id)
			} else {
				a.openControl(true)
			}
		})
		t.OnRightClick(func() {
			a.openControl(false)
		})
	}
}

func (a *application) undoCommand() {
	key := m.KeyWindow()
	if a.main != nil && a.mainPage == pageHistory && key == m.ID(a.main.NativeHandle()) && !m.EditingText(key) {
		a.undoHistoryDeletion()
		return
	}
	m.NativeUndo()
}

func (a *application) refreshTrayTitle(title string) {
	if a.tray == nil {
		return
	}
	if a.trayTemplate {
		title = strings.TrimSpace(strings.TrimPrefix(title, "◉"))
	}
	phase := a.controller.Snapshot().Phase
	if !session.Exists(phase) {
		if v, ok := a.savedTemplate(a.store.Data.Preferences.QuickTemplate); ok {
			if a.trayTemplate {
				title = ""
			} else {
				title = "◉ "
			}
			a.tray.SetTitle(fmt.Sprintf("%s部分保护 · %d 分", title, v.Config.Minutes))
			a.tray.SetToolTip(fmt.Sprintf("%s · 以部分保护开始「%s」· %d 分钟（右键打开）", appDisplayName, v.Name, v.Config.Minutes))
			return
		}
	}
	a.tray.SetTitle(title)
	a.tray.SetToolTip(appDisplayName + " · " + phaseName(phase))
}

//go:build darwin

package main

import (
	"errors"
	"fmt"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/egoist/mygo"
	"sort"
	"strings"
)

func (a *application) openRunningAppPicker(fromMain bool) {
	if a.focusForm != nil {
		a.openApplicationPicker(a.focusForm)
	}
}
func (a *application) chooseOtherWorkApp(fromMain bool) {
	f := a.pickerTarget
	if f == nil {
		f = a.focusForm
	}
	if f == nil {
		return
	}
	v, err := m.ChooseApp()
	if errors.Is(err, m.ErrSelectionCancelled) {
		return
	}
	if err != nil {
		showMessage(f.notice, "无法读取应用："+err.Error())
		return
	}
	if a.fixtureRoot != "" && !a.ownFixture(journal.Ref{BundleID: v.ID, Path: v.Path}) {
		showMessage(f.notice, "隔离验收仅允许自有夹具。")
		return
	}
	f.get().Apps, _ = mergeWorkApps(f.get().Apps, []store.App{v})
	f.reloadApps()
}
func (a *application) openApplicationPicker(f *configForm) {
	if a.appPicker != nil {
		a.appPicker.Show()
		a.appPicker.Focus()
		return
	}
	if a.mainModal() || a.quitReady {
		return
	}
	a.ensureMain()
	a.rememberPageResponder()
	w := window("添加专注应用", 560, 560, false)
	a.appPicker = w
	a.pickerTarget = f
	root := m.FixedSurface(w.NativeHandle(), 560, 560, false)
	r, footer, _ := contentFooter(root)
	titleText(r, "添加专注应用", 20)
	search := field(r, "", "搜索应用名称或标识", "搜索已安装应用")
	status := bodyText(r, "正在读取已安装应用…", 12, true)
	all := append([]store.App{}, f.get().Apps...)
	visible := append([]store.App{}, all...)
	chosen := map[string]store.App{}
	baseIDs := map[string]bool{}
	for _, v := range f.get().Apps {
		chosen[v.ID] = v
		baseIDs[v.ID] = true
	}
	selectedApps := func() []store.App { return sortedAppSelection(chosen) }

	var list, selected *m.AppList
	var add, count m.ID
	updating := false
	updateCount := func() {
		if count != 0 {
			m.Text(count, fmt.Sprintf("已选 %d 个", len(chosen)))
		}
		if add != 0 {
			m.Enabled(add, len(chosen) > 0)
		}
		if selected != nil {
			selected.Reload(-1)
		}
	}
	list = m.NewAppList(r, func() []store.App { return visible }, func(int) {
		if updating || list == nil {
			return
		}
		indices := list.SelectedIndices()
		selectedIDs := map[string]bool{}
		for _, i := range indices {
			if i >= 0 && i < len(visible) {
				selectedIDs[visible[i].ID] = true
			}
		}
		for _, v := range visible {
			if selectedIDs[v.ID] {
				chosen[v.ID] = v
			} else if !baseIDs[v.ID] {
				delete(chosen, v.ID)
			}
		}
		updateCount()
	}, m.AppVisibleRows)
	list.SelectMultiple()
	m.Arrange(r, list.View, true)
	a.pickerList = list
	titleText(r, "已选应用", 13)
	count = bodyText(r, "", 12, true)
	selected = m.NewAppList(r, selectedApps, nil, m.AppVisibleRows)
	m.Arrange(r, selected.View, true)
	reload := func() {
		updating = true
		visible = nil
		q := strings.TrimSpace(m.Value(search))
		for _, v := range all {
			if session.MatchApp(v, q) {
				visible = append(visible, v)
			}
		}
		list.Reload(-1)
		m.Send(list.Table, "deselectAll:", m.ID(0))
		for i, v := range visible {
			if _, ok := chosen[v.ID]; ok {
				m.Send(list.Table, "selectRowIndexes:byExtendingSelection:", m.Send(m.Class("NSIndexSet"), "indexSetWithIndex:", uint64(i)), true)
			}
		}
		updating = false
	}
	selected.SetRemoveHandler(func(id string) { delete(chosen, id); reload(); updateCount() })
	m.ObserveText(search, func() {
		reload()
		if len(visible) == 0 {
			m.Text(status, "没有匹配的应用，可更改关键词或从其他位置选择。")
		} else {
			m.Text(status, fmt.Sprintf("%d 个匹配应用", len(visible)))
		}
	})
	buttons := row(footer, 8)
	action(buttons, "其他应用…", func() {
		v, err := m.ChooseApp()
		if errors.Is(err, m.ErrSelectionCancelled) {
			return
		}
		if err != nil {
			m.Text(status, "无法读取应用："+err.Error())
			return
		}
		if a.fixtureRoot != "" && !a.ownFixture(journal.Ref{BundleID: v.ID, Path: v.Path}) {
			m.Text(status, "隔离验收仅允许自有夹具。")
			return
		}
		chosen[v.ID] = v
		all, _ = mergeWorkApps(all, []store.App{v})
		reload()
		updateCount()
	})
	m.Flex(buttons)
	action(buttons, "取消", w.Close)
	add = action(buttons, "添加所选", func() { f.get().Apps = selectedApps(); f.reloadApps(); w.Close() })
	m.Primary(add)
	updateCount()
	load := func() {
		m.Text(status, "正在读取已安装应用…")
		go func() {
			paths, issues := m.InstalledAppPaths()
			mygo.RunOnMain(func() {
				if a.appPicker != w {
					return
				}
				items := append([]store.App{}, f.get().Apps...)
				refs, err := m.Running()
				if err != nil {
					issues = append(issues, err.Error())
				}
				for _, ref := range refs {
					paths = append(paths, ref.Path)
				}
				for _, path := range paths {
					v, err := m.AppAt(path)
					if err != nil {
						issues = append(issues, path+"："+err.Error())
						continue
					}
					if a.fixtureRoot != "" && !a.ownFixture(journal.Ref{BundleID: v.ID, Path: v.Path}) {
						continue
					}
					exists := false
					for _, old := range items {
						if old.ID == v.ID {
							exists = true
							break
						}
					}
					if !exists {
						items = append(items, v)
					}
				}
				sort.Slice(items, func(i, j int) bool {
					left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
					if left == right {
						return items[i].ID < items[j].ID
					}
					return left < right
				})
				all = items
				reload()
				caption := fmt.Sprintf("已读取 %d 个应用", len(all))
				if len(issues) > 0 {
					caption += " · 部分位置无法读取，可重新读取或手动添加。"
					m.Send(status, "setToolTip:", m.NS(strings.Join(issues, "\n")))
				}
				m.Text(status, caption)
			})
		}()
	}
	action(footer, "重新读取", load)
	handle := w.NativeHandle()
	w.OnClose(func(*mygo.CloseEvent) { m.EndSheet(handle) })
	w.OnClosed(func() {
		m.EndSheet(handle)
		list.Close()
		selected.Close()
		m.UnbindTree(root)
		if a.appPicker == w {
			a.appPicker = nil
			a.pickerList = nil
			a.pickerTarget = nil
		}
		a.refreshNavigation()
	})
	if !m.BeginSheet(a.main.NativeHandle(), handle) {
		w.Close()
		showMessage(f.notice, "无法打开选择层，请关闭其他对话框后重试。")
		return
	}
	a.refreshNavigation()
	reload()
	load()
	m.KeyLoop(handle)
}
func mergeWorkApps(existing, selected []store.App) ([]store.App, int) {
	next := append([]store.App(nil), existing...)
	lastAdded := -1
	for _, app := range selected {
		found := false
		for i, old := range next {
			if old.ID == app.ID {
				next[i] = app
				found = true
				break
			}
		}
		if !found {
			next = append(next, app)
			lastAdded = len(next) - 1
		}
	}
	return next, lastAdded
}

func sortedAppSelection(chosen map[string]store.App) []store.App {
	items := []store.App{}
	for _, v := range chosen {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
	return items
}

//go:build darwin

package macos

import (
	"fmt"
	"github.com/ebitengine/purego/objc"
	"math"
	"time"
)

const sceneGridSpacing = 8.0
const sceneGridThreeColumns = 624.0

var grids = map[ID]*SceneGrid{}
var gridButtons = map[ID]*SceneGrid{}

type SceneChoice struct {
	ID, Name, Path string
	Available      bool
}
type SceneGrid struct {
	View    ID
	Buttons []ID
	Choices []SceneChoice
	height  ID
	columns int
}

func NewSceneGrid(parent ID, choices []SceneChoice, selected string, change func(string)) *SceneGrid {
	if objc.GetClass("FocusSceneGrid") == 0 {
		_, err := objc.RegisterClass("FocusSceneGrid", objc.GetClass("NSView"), nil, nil, []objc.MethodDef{
			{Cmd: S("isFlipped"), Fn: func(ID, objc.SEL) bool { return true }},
			{Cmd: S("layout"), Fn: func(id ID, sel objc.SEL) {
				id.SendSuper(sel)
				if g := grids[id]; g != nil {
					g.layout()
				}
			}},
		})
		if err != nil {
			panic(err)
		}
		_, err = objc.RegisterClass("FocusSceneButton", objc.GetClass("NSButton"), nil, nil, []objc.MethodDef{
			{Cmd: S("keyDown:"), Fn: func(id ID, sel objc.SEL, event ID) {
				key := objc.Send[uint16](event, S("keyCode"))
				g := gridButtons[id]
				if g != nil && key >= 123 && key <= 126 {
					for i, b := range g.Buttons {
						if b == id {
							step := 1
							if key == 123 {
								step = -1
							}
							if key == 125 {
								step = g.columns
							}
							if key == 126 {
								step = -g.columns
							}
							next := i + step
							for next >= 0 && next < len(g.Buttons) {
								if objc.Send[bool](g.Buttons[next], S("isEnabled")) {
									Send(Send(id, "window"), "makeFirstResponder:", g.Buttons[next])
									return
								}
								next += step
							}
							return
						}
					}
				}
				id.SendSuper(sel, event)
			}},
		})
		if err != nil {
			panic(err)
		}
	}
	g := &SceneGrid{View: Auto(View("FocusSceneGrid", R(0, 0, 600, 320))), Choices: append([]SceneChoice{}, choices...), columns: 2}
	Add(parent, g.View)
	Arrange(parent, g.View, true)
	grids[g.View] = g
	g.height = Send(Send(g.View, "heightAnchor"), "constraintEqualToConstant:", float64(320))
	Send(g.height, "setActive:", true)
	for _, c := range choices {
		choice := c
		b := View("FocusSceneButton", R(0, 0, 200, 164))
		Send(b, "setTitle:", NS(c.Name))
		Send(b, "setButtonType:", uint64(1))
		Send(b, "setBezelStyle:", uint64(6))
		Send(b, "setImagePosition:", uint64(5))
		Send(b, "setImageScaling:", uint64(3))
		Send(b, "setFont:", Send(Class("NSFont"), "systemFontOfSize:", float64(13)))
		Send(b, "setAccessibilityLabel:", NS("自然环境 "+c.Name))
		Send(b, "setWantsLayer:", true)
		Send(Send(b, "layer"), "setCornerRadius:", GroupRadius)
		Send(Send(b, "layer"), "setMasksToBounds:", true)
		Bind(b, func() { change(choice.ID) })
		Add(g.View, b)
		g.Buttons = append(g.Buttons, b)
		gridButtons[b] = g
		Release(b)
		g.SetImage(len(g.Buttons)-1, c.Path)
		Enabled(b, c.Available)
		if !c.Available {
			Send(b, "setToolTip:", NS("此环境暂不可用，请重新读取资源。"))
		}
	}
	g.Select(selected)
	Release(g.View)
	return g
}
func (g *SceneGrid) layout() {
	w := Bounds(g.View).W
	if w < 20 {
		return
	}
	cols := 2
	if w >= sceneGridThreeColumns {
		cols = 3
	}
	g.columns = cols
	cell := (w - float64(cols-1)*sceneGridSpacing) / float64(cols)
	h := cell*130/200 + 34
	rows := (len(g.Buttons) + cols - 1) / cols
	height := float64(rows)*(h+sceneGridSpacing) - sceneGridSpacing
	if math.Abs(objc.Send[float64](g.height, S("constant"))-height) > .5 {
		Send(g.height, "setConstant:", height)
	}
	for i, b := range g.Buttons {
		Frame(b, R(float64(i%cols)*(cell+sceneGridSpacing), float64(i/cols)*(h+sceneGridSpacing), cell, h))
	}
}
func (g *SceneGrid) SetImage(i int, path string) bool {
	if g == nil || i < 0 || i >= len(g.Buttons) {
		return false
	}
	image := Send(Send(Class("NSImage"), "alloc"), "initWithContentsOfFile:", NS(path))
	Send(g.Buttons[i], "setImage:", image)
	if image != 0 {
		Release(image)
	}
	return image != 0
}
func (g *SceneGrid) Select(id string) ID {
	selected := ID(0)
	for i, b := range g.Buttons {
		on := g.Choices[i].ID == id
		state := int64(0)
		title := g.Choices[i].Name
		color := Send(Class("NSColor"), "separatorColor")
		width := .5
		if on {
			state = 1
			title += " ✓"
			selected = b
			color = Send(Class("NSColor"), "controlAccentColor")
			width = 2
		}
		Send(b, "setState:", state)
		Send(b, "setTitle:", NS(title))
		layer := Send(b, "layer")
		Send(layer, "setBorderWidth:", width)
		Send(layer, "setBorderColor:", Send(color, "CGColor"))
	}
	return selected
}
func (g *SceneGrid) Close() {
	if g == nil {
		return
	}
	delete(grids, g.View)
	for _, b := range g.Buttons {
		delete(gridButtons, b)
		delete(targets, b)
	}
}

var showDatePopover func(ID, objc.SEL, Rect, ID, uint64)

type DateFilter struct {
	Button, picker, popover, root ID
	value, label                  string
	change                        func(string)
}

func NewDateFilter(parent ID, label, value string, change func(string)) *DateFilter {
	d := &DateFilter{value: value, label: label, change: change}
	d.Button = Button(parent, "", R(0, 0, 240, 28), d.open)
	Arrange(parent, d.Button, false)
	Send(d.Button, "setAccessibilityLabel:", NS("历史"+label))
	d.caption()
	return d
}
func (d *DateFilter) caption() {
	value := d.value
	if value == "" {
		value = "不限日期"
	}
	Send(d.Button, "setTitle:", NS(d.label+"："+value+" ▾"))
	FitButtonWidth(d.Button)
}
func (d *DateFilter) open() {
	d.closePopover()
	d.popover = Send(Class("NSPopover"), "new")
	Send(d.popover, "setBehavior:", int64(1))
	vc := Send(Class("NSViewController"), "new")
	d.root = View("NSView", R(0, 0, 292, 300))
	d.picker = View("NSDatePicker", R(12, 48, 268, 240))
	Send(d.picker, "setDatePickerStyle:", uint64(1))
	Send(d.picker, "setDatePickerElements:", uint64(0xe0))
	Send(d.picker, "setDatePickerMode:", uint64(0))
	Send(d.picker, "setTimeZone:", Send(Class("NSTimeZone"), "localTimeZone"))
	Send(d.picker, "setAccessibilityLabel:", NS(d.label+"日历"))
	date, err := time.ParseInLocation("2006-01-02", d.value, time.Local)
	if err != nil {
		date = time.Now()
	}
	Send(d.picker, "setDateValue:", Send(Class("NSDate"), "dateWithTimeIntervalSince1970:", float64(date.Unix())))
	Add(d.root, d.picker)
	Release(d.picker)
	Button(d.root, "不限日期", R(12, 10, 100, 28), func() { d.value = ""; d.closePopover(); d.caption(); d.change("") })
	done := Button(d.root, "完成", R(180, 10, 100, 28), func() {
		sec := objc.Send[float64](Send(d.picker, "dateValue"), S("timeIntervalSince1970"))
		d.value = time.Unix(int64(sec), 0).In(time.Local).Format("2006-01-02")
		d.closePopover()
		d.caption()
		d.change(d.value)
	})
	Primary(done)
	Send(vc, "setView:", d.root)
	Release(d.root)
	Send(d.popover, "setContentViewController:", vc)
	Release(vc)
	if showDatePopover == nil {
		panic(fmt.Errorf("date popover bridge is not initialized"))
	}
	showDatePopover(d.popover, S("showRelativeToRect:ofView:preferredEdge:"), Bounds(d.Button), d.Button, 3)
}
func (d *DateFilter) closePopover() {
	if d.popover != 0 {
		Send(d.popover, "close")
		UnbindTree(d.root)
		Send(d.popover, "autorelease")
		d.popover = 0
		d.root = 0
		d.picker = 0
	}
}
func (d *DateFilter) Close() {
	if d != nil {
		d.closePopover()
		delete(targets, d.Button)
	}
}
func (d *DateFilter) Dismiss() {
	if d != nil {
		d.closePopover()
	}
}

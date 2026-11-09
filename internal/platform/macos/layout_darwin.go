//go:build darwin

package macos

import (
	"errors"
	"os"

	"focus.local/zhuanzhuli/internal/store"
	"github.com/ebitengine/purego/objc"
)

// AlertSheet mounts in this main-thread transaction. MyGo's Message queues
// creation and cannot cancel a not-yet-mounted request; D13 needs cancellation
// to finish before lock/sleep returns. AppKit accepts a nil completion block.
type AlertSheet struct {
	alert, parent, window ID
	buttons               []ID
}

func NewAlertSheet(parent uintptr, message, detail string, titles []string, done func(int)) (*AlertSheet, error) {
	p := ID(parent)
	if p == 0 || !objc.Send[bool](p, S("isVisible")) || Send(p, "attachedSheet") != 0 {
		return nil, errors.New("sheet requires a visible, available parent")
	}
	s := &AlertSheet{alert: Send(Class("NSAlert"), "new"), parent: p}
	Send(s.alert, "setAlertStyle:", int64(1))
	Send(s.alert, "setMessageText:", NS(message))
	Send(s.alert, "setInformativeText:", NS(detail))
	for i, title := range titles {
		button := Send(s.alert, "addButtonWithTitle:", NS(title))
		key := ""
		if i == 0 {
			key = "\r"
		} else if i == len(titles)-1 {
			key = "\x1b"
		}
		Send(button, "setKeyEquivalent:", NS(key))
		s.buttons = append(s.buttons, button)
	}
	s.window = Send(s.alert, "window")
	Send(s.alert, "beginSheetModalForWindow:completionHandler:", p, ID(0))
	// NSAlert sets up its own targets while mounting. Bind after that pass.
	for i, button := range s.buttons {
		Bind(button, func() { s.Close(); done(i) })
		key := ""
		if i == 0 {
			key = "\r"
			Send(s.window, "setDefaultButtonCell:", Send(button, "cell"))
		}
		if i == len(s.buttons)-1 {
			key = "\x1b"
		}
		Send(button, "setKeyEquivalent:", NS(key))
	}
	return s, nil
}

func (s *AlertSheet) Close() {
	if s == nil || s.alert == 0 {
		return
	}
	for _, button := range s.buttons {
		delete(targets, button)
	}
	Send(s.parent, "endSheet:returnCode:", s.window, int64(1002))
	Send(s.window, "orderOut:", ID(0))
	// Keep the sender alive through the native button's action dispatch.
	Send(s.alert, "autorelease")
	s.alert = 0
}

// Generic confirmations cancel on Escape. D13 keeps its separate explicit keys.
func (s *AlertSheet) CancelFirst() {
	if s == nil || len(s.buttons) == 0 {
		return
	}
	for _, button := range s.buttons {
		Send(button, "setKeyEquivalent:", NS(""))
	}
	Send(s.window, "setDefaultButtonCell:", Send(s.buttons[0], "cell"))
	Send(s.buttons[0], "setKeyEquivalent:", NS("\x1b"))
}
func (s *AlertSheet) Cancel() {
	if s == nil || s.alert == 0 {
		return
	}
	if len(s.buttons) == 0 {
		s.Close()
		return
	}
	Send(s.buttons[0], "performClick:", ID(0))
}

func ContentSize(handle uintptr, w, h float64) {
	setSize(ID(handle), S("setContentSize:"), Point{w, h})
}
func WindowMetrics(handle uintptr) map[string]any {
	w := ID(handle)
	return map[string]any{"frame": FrameOf(w), "content": Bounds(Send(w, "contentView")), "min": getSize(w, S("minSize")), "contentMin": getSize(w, S("contentMinSize")), "max": getSize(w, S("maxSize")), "contentMax": getSize(w, S("contentMaxSize"))}
}
func ContentMinimum(handle uintptr, w, h float64) {
	setSize(ID(handle), S("setContentMinSize:"), Point{w, h})
}

func VisibleContentSize(handle uintptr) Point {
	frame := DisplayFrame(CaptureDisplay(handle), true)
	content := transformRect(ID(handle), S("contentRectForFrameRect:"), frame)
	return Point{content.W, content.H}
}
func FitWindowOnScreen(handle uintptr) {
	visible := DisplayFrame(CaptureDisplay(handle), true)
	frame := FrameOf(ID(handle))
	if visible.W <= 0 || visible.H <= 0 {
		return
	}
	frame.X = max(visible.X, min(frame.X, visible.X+visible.W-frame.W))
	frame.Y = max(visible.Y, min(frame.Y, visible.Y+visible.H-frame.H))
	frameWindow(ID(handle), S("setFrame:display:"), frame, true)
}
func TransparentSurface(handle uintptr) ID {
	container := Send(ID(handle), "contentView")
	v := View("NSView", Bounds(container))
	Send(v, "setAutoresizingMask:", uint64(18))
	Add(container, v)
	Release(v)
	return v
}
func FixedSurface(handle uintptr, w, h float64, transparent bool) ID {
	var root ID
	if transparent {
		root = TransparentSurface(handle)
	} else {
		root = FillSurface(handle)
	}
	// Fixed auxiliary windows must supply their content size to AppKit's
	// fitting pass; NSStackView's intrinsic width must not resize their window.
	Edges(root, Send(ID(handle), "contentView"), 0)
	Fixed(root, "width", w)
	Fixed(root, "height", h)
	return root
}

func RestoreScroll(scroll ID, p Point) {
	Send(Send(scroll, "superview"), "layoutSubtreeIfNeeded")
	clip := Send(scroll, "contentView")
	bounds := Bounds(clip)
	bounds.X, bounds.Y = p.X, p.Y
	bounds = transformRect(clip, S("constrainBoundsRect:"), bounds)
	setPoint(clip, S("scrollToPoint:"), Point{bounds.X, bounds.Y})
	Send(scroll, "reflectScrolledClipView:", clip)
}
func RectInView(id, parent ID) Rect {
	return convertRect(id, S("convertRect:toView:"), Bounds(id), parent)
}
func IntrinsicSize(id ID) Point { return getSize(id, S("intrinsicContentSize")) }
func FitsInside(id, parent ID) bool {
	Send(parent, "layoutSubtreeIfNeeded")
	r, b := RectInView(id, parent), Bounds(parent)
	return r.W > 0 && r.H > 0 && r.X >= -1 && r.Y >= -1 && r.X+r.W <= b.W+1 && r.Y+r.H <= b.H+1
}

// These are AppKit layout operations, not another view/state framework. AppKit
// owns intrinsic sizes, keyboard navigation, appearance and accessibility.
func Auto(id ID) ID { Send(id, "setTranslatesAutoresizingMaskIntoConstraints:", false); return id }
func Equal(a ID, anchor string, b ID, other string, offset float64) {
	c := Send(Send(a, anchor), "constraintEqualToAnchor:constant:", Send(b, other), offset)
	Send(c, "setActive:", true)
}
func Fixed(id ID, axis string, value float64) {
	Send(Send(Send(id, axis+"Anchor"), "constraintEqualToConstant:", value), "setActive:", true)
}
func Minimum(id ID, axis string, value float64) {
	Send(Send(Send(id, axis+"Anchor"), "constraintGreaterThanOrEqualToConstant:", value), "setActive:", true)
}
func Edges(id, parent ID, inset float64) {
	Auto(id)
	Equal(id, "leadingAnchor", parent, "leadingAnchor", inset)
	Equal(id, "trailingAnchor", parent, "trailingAnchor", -inset)
	Equal(id, "topAnchor", parent, "topAnchor", inset)
	Equal(id, "bottomAnchor", parent, "bottomAnchor", -inset)
}
func Stack(parent ID, vertical bool, spacing float64) ID {
	s := Auto(View("NSStackView", R(0, 0, 1, 1)))
	orientation, alignment := int64(0), int64(10) // horizontal / centerY
	if vertical {
		orientation, alignment = 1, 5
	} // vertical / leading
	Send(s, "setOrientation:", orientation)
	Send(s, "setAlignment:", alignment)
	Send(s, "setDistribution:", int64(0)) // NSStackViewDistributionFill
	Send(s, "setSpacing:", spacing)
	Send(s, "setDetachesHiddenViews:", true)
	Add(parent, s)
	Release(s)
	return s
}
func Arrange(stack, child ID, fill bool) ID {
	Auto(child)
	Send(stack, "addArrangedSubview:", child)
	if fill {
		Equal(child, "widthAnchor", stack, "widthAnchor", 0)
	}
	return child
}
func Gap(stack ID, height float64) {
	v := Auto(View("NSView", R(0, 0, 1, height)))
	Arrange(stack, v, false)
	Fixed(v, "height", height)
	Release(v)
}
func Flex(stack ID) ID {
	v := Auto(View("NSView", R(0, 0, 1, 1)))
	Arrange(stack, v, false)
	Send(v, "setContentHuggingPriority:forOrientation:", float32(1), int64(0))
	Release(v)
	return v
}
func FillSurface(handle uintptr) ID {
	container := Send(ID(handle), "contentView")
	v := View("NSBox", Bounds(container))
	Send(v, "setBoxType:", int64(4))
	Send(v, "setTitlePosition:", int64(0))
	Send(v, "setBorderWidth:", float64(0))
	Send(v, "setCornerRadius:", float64(0))
	Send(v, "setFillColor:", CanvasColor())
	Send(v, "setAutoresizingMask:", uint64(18))
	Add(container, v)
	Release(v)
	return v
}
func GroupStack(parent ID, spacing float64) (ID, ID) {
	box := Auto(View("NSBox", R(0, 0, 1, 1)))
	Send(box, "setBoxType:", int64(4))
	Send(box, "setTitlePosition:", int64(0))
	Send(box, "setBorderWidth:", float64(0))
	Send(box, "setCornerRadius:", GroupRadius)
	Send(box, "setFillColor:", CanvasColor())
	Send(box, "setBorderColor:", Send(Class("NSColor"), "separatorColor"))
	Send(box, "setBorderWidth:", float64(.5))
	Add(parent, box)
	stack := Stack(box, true, spacing)
	Edges(stack, box, 14)
	Release(box)
	return box, stack
}

// A flipped document makes new/short content start at the top. Its height is
// derived from the stack, and its width follows the clip view when resized.
func ScrollStack(parent ID) (ID, ID) {
	if objc.GetClass("FocusFlippedDocument") == 0 {
		_, err := objc.RegisterClass("FocusFlippedDocument", objc.GetClass("NSView"), nil, nil, []objc.MethodDef{{Cmd: S("isFlipped"), Fn: func(ID, objc.SEL) bool { return true }}})
		if err != nil {
			panic(err)
		}
	}
	scroll := Auto(View("NSScrollView", R(0, 0, 1, 1)))
	Send(scroll, "setHasVerticalScroller:", true)
	Send(scroll, "setDrawsBackground:", false)
	Send(scroll, "setAutohidesScrollers:", true)
	doc := Auto(View("FocusFlippedDocument", R(0, 0, 1, 1)))
	Send(scroll, "setDocumentView:", doc)
	clip := Send(scroll, "contentView")
	Equal(doc, "leadingAnchor", clip, "leadingAnchor", 0)
	Equal(doc, "topAnchor", clip, "topAnchor", 0)
	Equal(doc, "widthAnchor", clip, "widthAnchor", 0)
	stack := Stack(doc, true, 8)
	// Anchor constants depend on the coordinate orientation of the container.
	Equal(stack, "leadingAnchor", doc, "leadingAnchor", 24)
	Equal(stack, "trailingAnchor", doc, "trailingAnchor", -24)
	Equal(stack, "topAnchor", doc, "topAnchor", 20)
	Equal(stack, "bottomAnchor", doc, "bottomAnchor", -20)
	Add(parent, scroll)
	Release(doc)
	Release(scroll)
	return scroll, stack
}
func Switch(parent ID, name string, on bool, change func(bool)) ID {
	id := View("NSSwitch", R(0, 0, 42, 26))
	SetChecked(id, on)
	Send(id, "setAccessibilityLabel:", NS(name))
	Bind(id, func() { change(Checked(id)) })
	Add(parent, id)
	Release(id)
	return id
}
func SetChecked(id ID, on bool) {
	state := int64(0)
	if on {
		state = 1
	}
	Send(id, "setState:", state)
}

type AppList struct {
	View, Table, delegate ID
	apps                  func() []store.App
	selected              func(int)
	cells                 map[ID]appCell
	height                ID
	visibleRows           int
	presentedIDs          []string
	remove                func(string)
}

const AppRowHeight, AppVisibleRows = 44.0, 6

func TableRowRect(table ID, row int) Rect {
	return indexedRect(table, S("rectOfRow:"), int64(row))
}

// Keep AppKit's selection and focus drawing, clipping its actual row surface.
func clipTableRow(row ID) {
	Send(row, "setWantsLayer:", true)
	layer := Send(row, "layer")
	Send(layer, "setCornerRadius:", GroupRadius)
	Send(layer, "setMasksToBounds:", true)
}

type appCell struct{ root, icon, name, remove ID }

// RecordList keeps the list native and allocates controls only for visible rows.
// Content and commands stay with the caller; this adapter owns reusable views.
type RecordRow struct{ ID, Title, Detail, Duration string }
type recordCell struct{ root, title, detail, duration, reuse, remove, inspect ID }
type RecordList struct {
	View, Table, delegate ID
	count                 func() int
	row                   func(int) RecordRow
	reuse, remove         func(string)
	inspect               func(string)
	cells                 map[ID]recordCell
}

var recordLists = map[ID]*RecordList{}

func NewRecordList(parent ID, count func() int, row func(int) RecordRow, reuse, remove func(string)) *RecordList {
	if objc.GetClass("FocusRecordList") == 0 {
		_, err := objc.RegisterClass("FocusRecordList", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: S("tableView:didAddRowView:forRow:"), Fn: func(_ ID, _ objc.SEL, table, row ID, index int64) {
				clipTableRow(row)
			}},
			{Cmd: S("numberOfRowsInTableView:"), Fn: func(_ ID, _ objc.SEL, table ID) int64 {
				if l := recordLists[table]; l != nil {
					return int64(l.count())
				}
				return 0
			}},
			{Cmd: S("tableView:viewForTableColumn:row:"), Fn: func(_ ID, _ objc.SEL, table, column ID, index int64) ID {
				l := recordLists[table]
				if l == nil || index < 0 || int(index) >= l.count() {
					return 0
				}
				view := Send(table, "makeViewWithIdentifier:owner:", NS("FocusRecordRow"), ID(0))
				if view == 0 {
					view = View("NSTableCellView", R(0, 0, 500, 80))
					Send(view, "setIdentifier:", NS("FocusRecordRow"))
					cell := recordCell{root: view}
					cell.title = Auto(Heading(view, "", R(0, 0, 100, 36), 14))
					LimitGoalLabel(cell.title)
					Send(cell.title, "setMaximumNumberOfLines:", int64(1))
					Send(cell.title, "setSelectable:", true)
					cell.detail = Auto(Label(view, "", R(0, 0, 100, 20), 12))
					Secondary(cell.detail)
					Send(cell.detail, "setMaximumNumberOfLines:", int64(1))
					cell.reuse = Auto(Button(view, "复用目标", R(0, 0, 80, 28), nil))
					cell.remove = Auto(Button(view, "删除", R(0, 0, 60, 28), nil))
					cell.inspect = Auto(Button(view, "详情", R(0, 0, 60, 28), nil))
					cell.duration = Auto(Heading(view, "", R(0, 0, 96, 20), 12))
					Send(cell.duration, "setAlignment:", int64(1))
					Send(cell.duration, "setMaximumNumberOfLines:", int64(1))
					for _, button := range []ID{cell.reuse, cell.remove, cell.inspect} {
						FitButtonWidth(button)
						Equal(button, "centerYAnchor", view, "centerYAnchor", 0)
					}
					Equal(cell.remove, "trailingAnchor", view, "trailingAnchor", -8)
					Equal(cell.reuse, "trailingAnchor", cell.remove, "leadingAnchor", -8)
					Equal(cell.inspect, "trailingAnchor", cell.reuse, "leadingAnchor", -8)
					Equal(cell.title, "leadingAnchor", view, "leadingAnchor", 8)
					Equal(cell.duration, "trailingAnchor", cell.inspect, "leadingAnchor", -14)
					Equal(cell.duration, "centerYAnchor", view, "centerYAnchor", 0)
					Fixed(cell.duration, "width", 90)
					Equal(cell.title, "trailingAnchor", cell.duration, "leadingAnchor", -12)
					Equal(cell.title, "topAnchor", view, "topAnchor", 11)
					Equal(cell.detail, "leadingAnchor", cell.title, "leadingAnchor", 0)
					Equal(cell.detail, "trailingAnchor", cell.title, "trailingAnchor", 0)
					Equal(cell.detail, "topAnchor", cell.title, "bottomAnchor", 4)
					bindRow := func(button ID, command func(string)) {
						Bind(button, func() {
							i := int(objc.Send[int64](table, S("rowForView:"), button))
							if i >= 0 && i < l.count() {
								command(l.row(i).ID)
							}
						})
					}
					bindRow(cell.reuse, l.reuse)
					bindRow(cell.remove, l.remove)
					bindRow(cell.inspect, func(id string) {
						if l.inspect != nil {
							l.inspect(id)
						}
					})
					l.cells[view] = cell
				}
				cell := l.cells[view]
				record := l.row(int(index))
				Text(cell.title, record.Title)
				Send(cell.title, "setToolTip:", NS(record.Title))
				Text(cell.detail, record.Detail)
				Text(cell.duration, record.Duration)
				Send(cell.duration, "setToolTip:", NS(record.Duration))
				Send(cell.detail, "setToolTip:", NS(record.Detail))
				Send(cell.reuse, "setAccessibilityLabel:", NS("复用目标："+record.Title))
				Send(cell.remove, "setAccessibilityLabel:", NS("删除记录："+record.Title))
				Hidden(cell.inspect, l.inspect == nil)
				Send(cell.inspect, "setAccessibilityLabel:", NS("查看配置详情："+record.Title))
				return view
			}},
		})
		if err != nil {
			panic(err)
		}
	}
	l := &RecordList{count: count, row: row, reuse: reuse, remove: remove, cells: map[ID]recordCell{}}
	l.View = Auto(View("NSScrollView", R(0, 0, 500, 250)))
	Send(l.View, "setHasVerticalScroller:", true)
	Send(l.View, "setDrawsBackground:", false)
	Send(l.View, "setAutohidesScrollers:", true)
	l.Table = View("NSTableView", R(0, 0, 500, 250))
	Send(l.Table, "setHeaderView:", ID(0))
	Send(l.Table, "setStyle:", int64(2)) // inset: complete rounded selection
	Send(l.Table, "setAutoresizingMask:", uint64(2))
	Send(l.Table, "setRowHeight:", float64(58))
	Send(l.Table, "setUsesAlternatingRowBackgroundColors:", true)
	Send(l.Table, "setColumnAutoresizingStyle:", int64(1))
	Send(l.Table, "setBackgroundColor:", CanvasColor())
	Send(l.Table, "setAccessibilityLabel:", NS("历史记录"))
	column := Send(Send(Class("NSTableColumn"), "alloc"), "initWithIdentifier:", NS("record"))
	Send(column, "setMinWidth:", float64(0))
	Send(column, "setWidth:", float64(0))
	Send(l.Table, "addTableColumn:", column)
	Release(column)
	l.delegate = Send(Class("FocusRecordList"), "new")
	recordLists[l.Table] = l
	Send(l.Table, "setDataSource:", l.delegate)
	Send(l.Table, "setDelegate:", l.delegate)
	Send(l.View, "setDocumentView:", l.Table)
	Send(l.Table, "sizeLastColumnToFit")
	Add(parent, l.View)
	Release(l.View)
	Release(l.Table)
	Send(l.Table, "reloadData")
	return l
}
func (l *RecordList) Select(index int) {
	if index < 0 || index >= l.count() {
		return
	}
	Send(l.Table, "selectRowIndexes:byExtendingSelection:", Send(Class("NSIndexSet"), "indexSetWithIndex:", uint64(index)), false)
	Send(l.Table, "scrollRowToVisible:", int64(index))
}
func (l *RecordList) AllocatedRows() int                { return len(l.cells) }
func (l *RecordList) SetInspectHandler(fn func(string)) { l.inspect = fn; l.Reload() }
func (l *RecordList) Reload()                           { Send(l.Table, "reloadData") }
func (l *RecordList) Close() {
	if l == nil {
		return
	}
	Send(l.Table, "setDataSource:", ID(0))
	Send(l.Table, "setDelegate:", ID(0))
	delete(recordLists, l.Table)
	for _, cell := range l.cells {
		UnbindTree(cell.root)
		Release(cell.root)
	}
	Release(l.delegate)
}

var appLists = map[ID]*AppList{}

func NewAppList(parent ID, apps func() []store.App, selected func(int), visibleRows int) *AppList {
	if objc.GetClass("FocusWorkAppList") == 0 {
		_, err := objc.RegisterClass("FocusWorkAppList", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: S("tableView:didAddRowView:forRow:"), Fn: func(_ ID, _ objc.SEL, table, row ID, index int64) {
				clipTableRow(row)
			}},
			{Cmd: S("numberOfRowsInTableView:"), Fn: func(_ ID, _ objc.SEL, table ID) int64 {
				if l := appLists[table]; l != nil {
					return int64(len(l.apps()))
				}
				return 0
			}},
			{Cmd: S("tableView:viewForTableColumn:row:"), Fn: func(_ ID, _ objc.SEL, table, column ID, row int64) ID {
				l := appLists[table]
				if l == nil || row < 0 || int(row) >= len(l.apps()) {
					return 0
				}
				a := l.apps()[row]
				view := Send(table, "makeViewWithIdentifier:owner:", NS("FocusAppRow"), ID(0))
				if view == 0 {
					view = View("NSTableCellView", R(0, 0, 500, AppRowHeight))
					Send(view, "setIdentifier:", NS("FocusAppRow"))
					icon := Auto(View("NSImageView", R(0, 0, 28, 28)))
					Send(icon, "setImageScaling:", int64(0))
					Add(view, icon)
					Release(icon)
					name := Auto(Label(view, "", R(0, 0, 400, 20), 13))
					Send(name, "setMaximumNumberOfLines:", int64(1))
					Send(Send(name, "cell"), "setLineBreakMode:", int64(4))
					Send(view, "setImageView:", icon)
					Send(view, "setTextField:", name)
					Equal(icon, "leadingAnchor", view, "leadingAnchor", 12)
					Fixed(icon, "width", 28)
					Fixed(icon, "height", 28)
					Equal(icon, "centerYAnchor", view, "centerYAnchor", 0)
					Equal(name, "leadingAnchor", icon, "trailingAnchor", 10)
					remove := Auto(Button(view, "移除", R(0, 0, 60, 28), nil))
					FitButtonWidth(remove)
					Equal(remove, "trailingAnchor", view, "trailingAnchor", -12)
					Equal(remove, "centerYAnchor", view, "centerYAnchor", 0)
					Equal(name, "trailingAnchor", remove, "leadingAnchor", -10)
					Equal(name, "centerYAnchor", view, "centerYAnchor", 0)
					Bind(remove, func() {
						i := int(objc.Send[int64](table, S("rowForView:"), remove))
						if l.remove != nil && i >= 0 && i < len(l.apps()) {
							l.remove(l.apps()[i].ID)
						}
					})
					l.cells[view] = appCell{view, icon, name, remove}
				}
				cell := l.cells[view]
				Send(cell.icon, "setImage:", Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "iconForFile:", NS(a.Path)))
				name := a.Name
				if _, err := os.Stat(a.Path); err != nil {
					name += "（找不到应用）"
				}
				Text(cell.name, name)
				Hidden(cell.remove, l.remove == nil)
				Send(cell.remove, "setAccessibilityLabel:", NS("移除 "+a.Name))
				Send(cell.name, "setToolTip:", NS(name))
				Send(view, "setAccessibilityLabel:", NS(name))
				return view
			}},
			{Cmd: S("tableViewSelectionDidChange:"), Fn: func(_ ID, _ objc.SEL, n ID) {
				table := Send(n, "object")
				if l := appLists[table]; l != nil && l.selected != nil {
					l.selected(l.Selected())
				}
			}},
		})
		if err != nil {
			panic(err)
		}
	}
	l := &AppList{apps: apps, selected: selected, visibleRows: visibleRows, cells: map[ID]appCell{}}
	l.View = Auto(View("NSScrollView", R(0, 0, 520, 160)))
	Send(l.View, "setHasVerticalScroller:", true)
	Send(l.View, "setAutohidesScrollers:", true)
	Send(l.View, "setDrawsBackground:", false)
	l.Table = View("NSTableView", R(0, 0, 500, 160))
	Send(l.Table, "setHeaderView:", ID(0))
	Send(l.Table, "setStyle:", int64(2))
	Send(l.Table, "setAutoresizingMask:", uint64(2))
	Send(l.Table, "setRowHeight:", AppRowHeight)
	setSize(l.Table, S("setIntercellSpacing:"), Point{})
	Send(l.Table, "setBackgroundColor:", CanvasColor())
	Send(l.Table, "setColumnAutoresizingStyle:", int64(1))
	col := Send(Send(Class("NSTableColumn"), "alloc"), "initWithIdentifier:", NS("application"))
	Send(col, "setMinWidth:", float64(0))
	Send(col, "setWidth:", float64(0))
	Send(l.Table, "addTableColumn:", col)
	Release(col)
	l.delegate = Send(Class("FocusWorkAppList"), "new")
	appLists[l.Table] = l
	Send(l.Table, "setDataSource:", l.delegate)
	Send(l.Table, "setDelegate:", l.delegate)
	Send(l.Table, "setAccessibilityLabel:", NS("专注应用"))
	Send(l.View, "setDocumentView:", l.Table)
	Send(l.Table, "sizeLastColumnToFit")
	Add(parent, l.View)
	Release(l.View)
	Release(l.Table)
	l.height = Send(Send(l.View, "heightAnchor"), "constraintEqualToConstant:", AppRowHeight)
	Send(l.height, "setActive:", true)
	l.Reload(-1)
	return l
}
func (l *AppList) Selected() int                    { return int(objc.Send[int64](l.Table, S("selectedRow"))) }
func (l *AppList) SetRemoveHandler(fn func(string)) { l.remove = fn; l.Reload(l.Selected()) }

func (l *AppList) SelectID(id string) bool {
	for i, app := range l.apps() {
		if app.ID == id {
			Send(l.Table, "selectRowIndexes:byExtendingSelection:", Send(Class("NSIndexSet"), "indexSetWithIndex:", uint64(i)), false)
			return true
		}
	}
	return false
}

// RowGeometry measures displayed content rather than the enclosing table cell.
// The cell-based fallback lets the same regression measure the previous layout.
func (l *AppList) RowGeometry(row int) (Rect, Rect, bool) {
	if row < 0 || row >= len(l.apps()) {
		return Rect{}, Rect{}, false
	}
	Send(l.Table, "layoutSubtreeIfNeeded")
	view := Send(l.Table, "viewAtColumn:row:makeIfNecessary:", int64(0), int64(row), true)
	if view != 0 {
		icon, text := Send(view, "imageView"), Send(view, "textField")
		if icon != 0 && text != 0 {
			return RectInView(icon, view), RectInView(text, view), true
		}
	}
	columns := Send(l.Table, "tableColumns")
	if objc.Send[uint64](columns, S("count")) != 2 {
		return Rect{}, Rect{}, false
	}
	height := objc.Send[float64](l.Table, S("rowHeight"))
	icon := Send(l.Table, "preparedCellAtColumn:row:", int64(0), int64(row))
	text := Send(l.Table, "preparedCellAtColumn:row:", int64(1), int64(row))
	iconRect := transformRect(icon, S("imageRectForBounds:"), R(0, 0, 38, height))
	textRect := transformRect(text, S("titleRectForBounds:"), R(0, 0, 460, height))
	textRect.H = getSize(text, S("cellSize")).Y
	return iconRect, textRect, icon != 0 && text != 0
}
func (l *AppList) SelectMultiple() { Send(l.Table, "setAllowsMultipleSelection:", true) }

// RowFitsViewport includes the selection row, whose rounded edges must not be clipped.
func (l *AppList) RowFitsViewport(row int) bool {
	Send(Send(l.View, "superview"), "layoutSubtreeIfNeeded")
	view := Send(l.Table, "rowViewAtRow:makeIfNecessary:", int64(row), true)
	if view == 0 {
		return false
	}
	clip := Send(l.View, "contentView")
	r, b := RectInView(view, clip), Bounds(clip)
	return r.W > 0 && r.X >= b.X && r.X+r.W <= b.X+b.W
}
func (l *AppList) SelectedIndices() []int {
	indexes := Send(l.Table, "selectedRowIndexes")
	var out []int
	for i := objc.Send[uint64](indexes, S("firstIndex")); i < uint64(len(l.apps())); i = objc.Send[uint64](indexes, S("indexGreaterThanIndex:"), i) {
		out = append(out, int(i))
	}
	return out
}
func (l *AppList) Reload(index int) {
	clip := Send(l.View, "contentView")
	position := Bounds(clip)
	var selectedIDs []string
	if index < 0 {
		for _, i := range l.SelectedIndices() {
			if i < len(l.presentedIDs) {
				selectedIDs = append(selectedIDs, l.presentedIDs[i])
			}
		}
	}
	l.presentedIDs = nil
	for _, app := range l.apps() {
		l.presentedIDs = append(l.presentedIDs, app.ID)
	}
	Send(l.Table, "reloadData")
	if n := len(l.apps()); index >= n {
		index = n - 1
	}
	if index >= 0 {
		Send(l.Table, "selectRowIndexes:byExtendingSelection:", Send(Class("NSIndexSet"), "indexSetWithIndex:", uint64(index)), false)
	} else if len(selectedIDs) > 0 {
		Send(l.Table, "deselectAll:", ID(0))
		for i, id := range l.presentedIDs {
			for _, selected := range selectedIDs {
				if id == selected {
					Send(l.Table, "selectRowIndexes:byExtendingSelection:", Send(Class("NSIndexSet"), "indexSetWithIndex:", uint64(i)), true)
				}
			}
		}
	}
	height := AppRowHeight
	if n := min(len(l.apps()), l.visibleRows); n > 0 {
		last := TableRowRect(l.Table, n-1)
		height = max(height, last.Y+last.H)
	}
	Send(l.height, "setConstant:", height)
	RestoreScroll(l.View, Point{position.X, position.Y})
	// Fit after Auto Layout resolves the sheet width, not the constructor frame.
	Send(l.Table, "sizeLastColumnToFit")
	if l.selected != nil {
		l.selected(l.Selected())
	}
}
func (l *AppList) Close() {
	if l == nil {
		return
	}
	Send(l.Table, "setDelegate:", ID(0))
	Send(l.Table, "setDataSource:", ID(0))
	delete(appLists, l.Table)
	for _, cell := range l.cells {
		Release(cell.root)
	}
	Release(l.delegate)
}

func FrameOf(id ID) Rect { return getRect(id, S("frame")) }

// NavigationList is the native single-selection list variant used by the shell.
// The caller owns the selected page; programmatic selection never dispatches it.
type NavigationItem struct {
	Title, Symbol string
	Selectable    bool
}

const NavigationRowHeight = 32.0

type NavigationList struct {
	View, Table, delegate ID
	items                 []NavigationItem
	choose                func(int)
	selecting             bool
	cells                 map[ID]appCell
}

var navigationLists = map[ID]*NavigationList{}

func NewNavigationList(parent ID, items []NavigationItem, choose func(int)) *NavigationList {
	if objc.GetClass("FocusNavigationList") == 0 {
		_, err := objc.RegisterClass("FocusNavigationList", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: S("tableView:didAddRowView:forRow:"), Fn: func(_ ID, _ objc.SEL, table, row ID, index int64) {
				clipTableRow(row)
				if l := navigationLists[table]; l != nil && index >= 0 && int(index) < len(l.items) && !l.items[index].Selectable {
					Send(row, "setAccessibilityElement:", true)
					Send(row, "setAccessibilityRole:", NS("AXGroup"))
					Send(row, "setAccessibilityLabel:", NS(l.items[index].Title))
				}
			}},
			{Cmd: S("numberOfRowsInTableView:"), Fn: func(_ ID, _ objc.SEL, table ID) int64 {
				if l := navigationLists[table]; l != nil {
					return int64(len(l.items))
				}
				return 0
			}},
			{Cmd: S("tableView:shouldSelectRow:"), Fn: func(_ ID, _ objc.SEL, table ID, index int64) bool {
				l := navigationLists[table]
				return l != nil && index >= 0 && int(index) < len(l.items) && l.items[index].Selectable && objc.Send[bool](table, S("isEnabled"))
			}},
			{Cmd: S("tableView:isGroupRow:"), Fn: func(_ ID, _ objc.SEL, table ID, index int64) bool {
				l := navigationLists[table]
				return l != nil && index >= 0 && int(index) < len(l.items) && !l.items[index].Selectable
			}},
			{Cmd: S("tableView:viewForTableColumn:row:"), Fn: func(_ ID, _ objc.SEL, table, column ID, index int64) ID {
				l := navigationLists[table]
				if l == nil || index < 0 || int(index) >= len(l.items) {
					return 0
				}
				item := l.items[index]
				identifier := "FocusNavigationRow"
				if !item.Selectable {
					identifier = "FocusNavigationGroup"
				}
				view := Send(table, "makeViewWithIdentifier:owner:", NS(identifier), ID(0))
				if view == 0 {
					view = View("NSTableCellView", R(0, 0, 152, NavigationRowHeight))
					Send(view, "setIdentifier:", NS(identifier))
					var icon ID
					name := Auto(Label(view, "", R(0, 0, 110, 20), 13))
					Send(name, "setMaximumNumberOfLines:", int64(1))
					Send(Send(name, "cell"), "setLineBreakMode:", int64(4))
					Send(view, "setTextField:", name)
					if item.Selectable {
						icon = Auto(View("NSImageView", R(0, 0, 18, 18)))
						Add(view, icon)
						Release(icon)
						Send(view, "setImageView:", icon)
						Equal(icon, "leadingAnchor", view, "leadingAnchor", 8)
						Fixed(icon, "width", 18)
						Fixed(icon, "height", 18)
						Equal(icon, "centerYAnchor", view, "centerYAnchor", 0)
						Equal(name, "leadingAnchor", icon, "trailingAnchor", 8)
					} else {
						Equal(name, "leadingAnchor", view, "leadingAnchor", 8)
					}
					Equal(name, "trailingAnchor", view, "trailingAnchor", -8)
					Equal(name, "centerYAnchor", view, "centerYAnchor", 0)
					l.cells[view] = appCell{view, icon, name, 0}
				}
				cell := l.cells[view]
				Text(cell.name, item.Title)
				Hidden(cell.icon, !item.Selectable)
				color, font := Send(Class("NSColor"), "labelColor"), float64(13)
				if !item.Selectable {
					color = Send(Class("NSColor"), "secondaryLabelColor")
					font = 11
				}
				Send(cell.name, "setTextColor:", color)
				Send(cell.name, "setFont:", Send(Class("NSFont"), "systemFontOfSize:", font))
				Send(cell.icon, "setImage:", Send(Class("NSImage"), "imageWithSystemSymbolName:accessibilityDescription:", NS(item.Symbol), NS(item.Title)))
				return view
			}},
			{Cmd: S("tableViewSelectionDidChange:"), Fn: func(_ ID, _ objc.SEL, n ID) {
				table := Send(n, "object")
				if l := navigationLists[table]; l != nil && !l.selecting && l.choose != nil {
					i := int(objc.Send[int64](table, S("selectedRow")))
					if i >= 0 && i < len(l.items) && l.items[i].Selectable {
						l.choose(i)
					}
				}
			}},
		})
		if err != nil {
			panic(err)
		}
	}
	l := &NavigationList{items: items, choose: choose, cells: map[ID]appCell{}, selecting: true}
	l.View = Auto(View("NSScrollView", R(0, 0, 152, 400)))
	Send(l.View, "setDrawsBackground:", false)
	Send(l.View, "setAutohidesScrollers:", true)
	l.Table = View("NSTableView", R(0, 0, 152, 400))
	Send(l.Table, "setHeaderView:", ID(0))
	Send(l.Table, "setStyle:", int64(2))
	Send(l.Table, "setAutoresizingMask:", uint64(2))
	Send(l.Table, "setFloatsGroupRows:", false)
	Send(l.Table, "setRowHeight:", NavigationRowHeight)
	Send(l.Table, "setBackgroundColor:", Send(Class("NSColor"), "windowBackgroundColor"))
	Send(l.Table, "setColumnAutoresizingStyle:", int64(1))
	Send(l.Table, "setAllowsEmptySelection:", false)
	Send(l.Table, "setAccessibilityLabel:", NS("主页面导航"))
	column := Send(Send(Class("NSTableColumn"), "alloc"), "initWithIdentifier:", NS("destination"))
	Send(column, "setMinWidth:", float64(0))
	Send(column, "setWidth:", float64(0))
	Send(l.Table, "addTableColumn:", column)
	Release(column)
	l.delegate = Send(Class("FocusNavigationList"), "new")
	navigationLists[l.Table] = l
	Send(l.Table, "setDelegate:", l.delegate)
	Send(l.Table, "setDataSource:", l.delegate)
	Send(l.View, "setDocumentView:", l.Table)
	Send(l.Table, "sizeLastColumnToFit")
	Add(parent, l.View)
	Release(l.View)
	Release(l.Table)
	Send(l.Table, "reloadData")
	l.selecting = false
	return l
}
func (l *NavigationList) Select(index int) {
	l.selecting = true
	defer func() { l.selecting = false }()
	Send(l.Table, "selectRowIndexes:byExtendingSelection:", Send(Class("NSIndexSet"), "indexSetWithIndex:", uint64(index)), false)
}
func (l *NavigationList) Close() {
	if l == nil {
		return
	}
	Send(l.Table, "setDelegate:", ID(0))
	Send(l.Table, "setDataSource:", ID(0))
	delete(navigationLists, l.Table)
	for _, cell := range l.cells {
		Release(cell.root)
	}
	Release(l.delegate)
}

// ClearContent keeps a page's container and its shell constraints alive.
func ClearContent(parent ID) {
	children := Send(Send(parent, "subviews"), "copy")
	for i := uint64(0); i < objc.Send[uint64](children, S("count")); i++ {
		view := Send(children, "objectAtIndex:", i)
		UnbindTree(view)
		Send(view, "removeFromSuperview")
	}
	Release(children)
}
func BeginSheet(parent, child uintptr) bool {
	if parent == 0 || child == 0 || Send(ID(parent), "attachedSheet") != 0 {
		return false
	}
	Send(ID(parent), "beginSheet:completionHandler:", ID(child), ID(0))
	return true
}
func EndSheet(child uintptr) {
	if child == 0 {
		return
	}
	parent := Send(ID(child), "sheetParent")
	if parent != 0 {
		Send(parent, "endSheet:", ID(child))
		Send(ID(child), "orderOut:", ID(0))
	}
}
func FitButtonWidth(id ID) {
	// NSCell measures title/font/bezel independently of a stack's fitting pass.
	width := getSize(Send(id, "cell"), S("cellSize")).X
	if width < 32 {
		width = 32
	}
	constraints := Send(id, "constraints")
	for i := uint64(0); i < objc.Send[uint64](constraints, S("count")); i++ {
		c := Send(constraints, "objectAtIndex:", i)
		if Str(Send(c, "identifier")) == "FocusButtonWidth" {
			Send(c, "setConstant:", width)
			return
		}
	}
	c := Send(Send(id, "widthAnchor"), "constraintEqualToConstant:", width)
	Send(c, "setIdentifier:", NS("FocusButtonWidth"))
	Send(c, "setActive:", true)
}

func CroppedImage(parent ID, path string, r Rect) ID {
	v := View("NSView", r)
	Send(v, "setWantsLayer:", true)
	layer := Send(v, "layer")
	Send(layer, "setCornerRadius:", GroupRadius)
	Send(layer, "setMasksToBounds:", true)
	Send(layer, "setContentsGravity:", NS("resizeAspectFill"))
	Send(v, "setAccessibilityElement:", true)
	Send(v, "setAccessibilityRole:", NS("AXImage"))
	Send(v, "setAccessibilityLabel:", NS("自然场景预览"))
	SetImagePath(v, path)
	Add(parent, v)
	Release(v)
	return v
}
func SetImagePath(view ID, path string) bool {
	img := Send(Send(Class("NSImage"), "alloc"), "initWithContentsOfFile:", NS(path))
	if img != 0 {
		cg := Send(img, "CGImageForProposedRect:context:hints:", uintptr(0), ID(0), ID(0))
		Send(Send(view, "layer"), "setContents:", cg)
		Release(img)
		return cg != 0
	}
	Send(Send(view, "layer"), "setContents:", ID(0))
	return false
}

// AppKit button frames include bezel outsets; constraints and column widths use
// alignment rectangles. Never subtract guessed border/padding values.
func AlignmentFrame(id ID) Rect { return transformRect(id, S("alignmentRectForFrame:"), FrameOf(id)) }

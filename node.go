package caution

import (
	"sort"

	"github.com/nullentropy/caution/wire"
)

type Node struct {
	id     int
	typ    wire.Type
	props  map[string]any
	kids   []*Node
	parent *Node
	sess   *Session
	born   int

	onClick     func()
	onToggle    func(bool)
	onInput     func(string)
	onCommit    func(string)
	onSubmit    func(string)
	onTab       func(shift bool, value string, caret int) bool
	onNav       func(dir int)
	onSpanClick func(action string)

	onSelect      func(index int)
	onDismiss     func()
	onSplitResize func(pos float64)

	rowsFn           func(start, end int) [][]string
	onSort           func(key string, asc bool)
	onRowSelect      func(row int)
	onRowSelectKey   func(key string, row int)
	onRowActivate    func(row int)
	onRowActivateKey func(key string, row int)

	treeItems    []TreeItem
	treeOpen     map[string]bool
	treeFlat     []flatRow
	onTreeToggle func(key string, expanded bool)

	onCellActivate func(row int, key, col, value string)

	onSlide    func(v float64)
	onSlideEnd func(v float64)

	onPick   func(x, y float64, target *Node)
	onDragTo func(x, y float64)
	onDrop   func(x, y float64)
	onWheel  func(x, y, dx, dy float64)
	onRPick  func(x, y float64)
	onRDrag  func(x, y float64)
	onRDrop  func(x, y float64)

	onColResize func(key string, width float64)
	lastStart   int
	lastEnd     int
	hasRange    bool

	wantsFocus bool

	term *termHandlers

	contextHandlers   map[int]func()
	selectionHandlers map[int]func(start, end int)
}

func newNode(typ wire.Type) *Node {
	return &Node{typ: typ, props: map[string]any{}}
}

func Panel() *Node              { return newNode(wire.Panel) }
func Label(text string) *Node   { return newNode(wire.Label).set("text", text) }
func Button(label string) *Node { return newNode(wire.Button).set("label", label) }
func VStack() *Node             { return newNode(wire.VStack) }
func HStack() *Node             { return newNode(wire.HStack) }
func DockPanel() *Node          { return newNode(wire.Dock) }
func Scroll() *Node             { return newNode(wire.Scroll) }
func Checkbox(label string, checked bool) *Node {
	return newNode(wire.Checkbox).set("label", label).set("checked", checked)
}
func TextField(value string) *Node { return newNode(wire.TextField).set("value", value) }

func Textarea(value string) *Node { return newNode(wire.TextArea).set("value", value) }

func Image(src string) *Node { return newNode(wire.Image).set("src", src) }

func Select(options []string, selected int) *Node {
	return newNode(wire.Select).set("options", options).set("selected", selected)
}

func Progress(value float64) *Node { return newNode(wire.Progress).set("value", value) }

func Slider(min, max, value float64) *Node {
	return newNode(wire.Slider).set("min", min).set("max", max).set("value", value)
}

func Radio(options []string, selected int) *Node {
	return newNode(wire.Radio).set("options", options).set("selected", selected)
}

func TabBar(options []string, selected int) *Node {
	return newNode(wire.Tabs).set("options", options).set("selected", selected)
}

func (n *Node) Step(s float64) *Node { return n.set("step", s) }

func (n *Node) SetProgress(v float64) *Node { return n.set("value", v) }

func (n *Node) OnSlide(fn func(v float64)) *Node {
	n.onSlide = fn
	return n.subscribe(wire.EvInput)
}

func (n *Node) OnSlideEnd(fn func(v float64)) *Node {
	n.onSlideEnd = fn
	return n.subscribe(wire.EvCommit)
}

func Dialog(title string) *Node {
	return newNode(wire.Dialog).set("title", title).
		Anchor(A{Left: Px(0), Right: Px(0), Top: Px(0), Bottom: Px(0)})
}

func HSplit() *Node { return newNode(wire.Split).set("axis", "h") }
func VSplit() *Node { return newNode(wire.Split).set("axis", "v") }

type FX struct {
	Frag     string
	Animate  bool
	Uniforms map[string]float64
}

func fxProps(fx FX) map[string]any {
	m := map[string]any{"frag": fx.Frag}
	if fx.Animate {
		m["animate"] = true
	}
	if len(fx.Uniforms) > 0 {
		m["uniforms"] = fx.Uniforms
	}
	return m
}

func (n *Node) Effect(fx FX) *Node { return n.set("effect", fxProps(fx)) }

func Shader(fx FX) *Node {
	sh := newNode(wire.Shader).set("frag", fx.Frag)
	if fx.Animate {
		sh.set("animate", true)
	}
	if len(fx.Uniforms) > 0 {
		sh.set("uniforms", fx.Uniforms)
	}
	return sh
}

func Glass() *Node { return newNode(wire.Glass) }

func (n *Node) HoverFill(c string) *Node { return n.set("hoverFill", c) }

func (n *Node) OnPick(fn func(x, y float64, target *Node)) *Node {
	n.onPick = fn
	return n.subscribe(wire.EvPick)
}

func (n *Node) OnDragTo(fn func(x, y float64)) *Node {
	n.onDragTo = fn
	return n.subscribe(wire.EvDrag)
}

func (n *Node) OnWheel(fn func(x, y, dx, dy float64)) *Node {
	n.onWheel = fn
	return n.subscribe(wire.EvWheel)
}

func (n *Node) OnRightPick(fn func(x, y float64)) *Node {
	n.onRPick = fn
	return n.subscribe(wire.EvRPick)
}

func (n *Node) OnRightDragTo(fn func(x, y float64)) *Node {
	n.onRDrag = fn
	return n.subscribe(wire.EvRDrag)
}

func (n *Node) OnRightDrop(fn func(x, y float64)) *Node {
	n.onRDrop = fn
	return n.subscribe(wire.EvRDrop)
}

func (n *Node) OnDrop(fn func(x, y float64)) *Node {
	n.onDrop = fn
	return n.subscribe(wire.EvDrop)
}

func (n *Node) SetUniforms(u map[string]float64) *Node {
	n.props["uniforms"] = mergedUniforms(n.props["uniforms"], u)
	if n.sess != nil {
		n.sess.queueSet(n.id, "uniforms", u)
	}
	return n
}

func (n *Node) SetUniform(k string, v float64) *Node {
	return n.SetUniforms(map[string]float64{k: v})
}

func mergedUniforms(old any, add map[string]float64) map[string]float64 {
	prev, _ := old.(map[string]float64)
	merged := make(map[string]float64, len(prev)+len(add))
	for k, v := range prev {
		merged[k] = v
	}
	for k, v := range add {
		merged[k] = v
	}
	return merged
}

type Align string

const (
	AlignStart   Align = "start"
	AlignCenter  Align = "center"
	AlignEnd     Align = "end"
	AlignStretch Align = "stretch"
)

type GridCol struct {
	Px     float64
	Weight float64
	Align  Align
}

func Grid(cols []GridCol) *Node {
	arr := make([]map[string]any, 0, len(cols))
	for _, c := range cols {
		m := map[string]any{}
		switch {
		case c.Px > 0:
			m["kind"] = "fixed"
			m["px"] = c.Px
		case c.Weight > 0:
			m["kind"] = "fill"
			m["weight"] = c.Weight
		default:
			m["kind"] = "content"
		}
		if c.Align != "" {
			m["align"] = string(c.Align)
		}
		arr = append(arr, m)
	}
	return newNode(wire.Grid).set("columns", arr)
}

type ColKind string

const (
	ColText     ColKind = ""
	ColButton   ColKind = "button"
	ColCheckbox ColKind = "checkbox"
	ColProgress ColKind = "progress"
)

type Col struct {
	Key, Title    string
	Weight, Width float64
	Kind          ColKind
}

func Table(cols []Col, rowCount int) *Node {
	n := newNode(wire.Table).set("columns", colSpecs(cols)).set("rowCount", rowCount)
	return n.subscribe(wire.EvVisibleRange)
}

func colSpecs(cols []Col) []map[string]any {
	arr := make([]map[string]any, 0, len(cols))
	for _, c := range cols {
		m := map[string]any{"key": c.Key, "title": c.Title}
		if c.Width > 0 {
			m["width"] = c.Width
		} else if c.Weight > 0 {
			m["weight"] = c.Weight
		}
		if c.Kind != "" {
			m["kind"] = string(c.Kind)
		}
		arr = append(arr, m)
	}
	return arr
}

func (n *Node) Add(child *Node) *Node {
	if child.parent != nil {
		child.Remove()
	}
	child.parent = n
	n.kids = append(n.kids, child)
	if n.sess != nil {
		n.sess.attach(child)
		n.sess.queueInsert(n.id, len(n.kids)-1, child)
	}
	return child
}

func (n *Node) Kids(kids ...*Node) *Node {
	for _, k := range kids {
		n.Add(k)
	}
	return n
}

func (n *Node) Remove() {
	if n.parent != nil {
		p := n.parent
		for i, k := range p.kids {
			if k == n {
				p.kids = append(p.kids[:i], p.kids[i+1:]...)
				break
			}
		}
		n.parent = nil
	}
	if n.sess != nil {
		n.sess.queueRemove(n.id)
		n.sess.detach(n)
	}
}

func (n *Node) set(k string, v any) *Node {
	n.props[k] = v
	if n.sess != nil {
		n.sess.queueSet(n.id, k, v)
	}
	return n
}

func (n *Node) echo(k string, v any) { n.props[k] = v }

func (n *Node) Type() wire.Type { return n.typ }

func (n *Node) Parent() *Node { return n.parent }

func (n *Node) Children() []*Node { return append([]*Node(nil), n.kids...) }

func (n *Node) Prop(k string) any { return n.props[k] }

func (n *Node) PropKeys() []string {
	keys := make([]string, 0, len(n.props))
	for k := range n.props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (n *Node) SetProp(k string, v any) *Node { return n.set(k, v) }

func (n *Node) Clear() *Node {
	for _, k := range n.Children() {
		k.Remove()
	}
	return n
}

type Span struct {
	Text   string `json:"text"`
	Bold   bool   `json:"bold,omitempty"`
	Italic bool   `json:"italic,omitempty"`
	Mono   bool   `json:"mono,omitempty"`
	Color  string `json:"color,omitempty"`
	Bg     string `json:"bg,omitempty"`
	Action string `json:"action,omitempty"`
	Font   string `json:"font,omitempty"`
}

func RichText(spans ...Span) *Node { return newNode(wire.RichText).set("spans", spans) }

func (n *Node) OnClick(fn func()) *Node { n.onClick = fn; return n.subscribe(wire.EvClick) }

func (n *Node) OnSpanClick(fn func(action string)) *Node {
	n.onSpanClick = fn
	return n.subscribe(wire.EvSpanClick)
}
func (n *Node) OnToggle(fn func(bool)) *Node {
	n.onToggle = fn
	return n.subscribe(wire.EvToggle)
}

func (n *Node) OnInput(fn func(string)) *Node {
	n.onInput = fn
	return n.subscribe(wire.EvInput)
}

func (n *Node) OnCommit(fn func(string)) *Node {
	n.onCommit = fn
	return n.subscribe(wire.EvCommit)
}

func (n *Node) OnSubmit(fn func(string)) *Node {
	n.onSubmit = fn
	return n.subscribe(wire.EvSubmit)
}

func (n *Node) OnNav(fn func(dir int)) *Node {
	n.onNav = fn
	return n.subscribe(wire.EvNav)
}

func (n *Node) OnTab(fn func(shift bool, value string, caret int) bool) *Node {
	n.onTab = fn
	return n.subscribe(wire.EvTab)
}

func (n *Node) OnSelect(fn func(index int)) *Node {
	n.onSelect = fn
	return n.subscribe(wire.EvSelect)
}

func (n *Node) OnDismiss(fn func()) *Node {
	n.onDismiss = fn
	return n.subscribe(wire.EvDismiss)
}

func (n *Node) CardSize(w, h float64) *Node {
	return n.set("cardWidth", w).set("cardHeight", h)
}

func (n *Node) SplitPos(px float64) *Node { return n.set("pos", px) }

func (n *Node) SplitPinEnd() *Node { return n.set("pinEnd", true) }

func (n *Node) SplitMin(first, second float64) *Node {
	return n.set("minA", first).set("minB", second)
}

func (n *Node) OnSplitResize(fn func(pos float64)) *Node {
	n.onSplitResize = fn
	return n.subscribe(wire.EvSplitResize)
}

func (n *Node) subscribe(ev wire.Event) *Node {
	on, _ := n.props["on"].([]wire.Event)
	for _, e := range on {
		if e == ev {
			return n
		}
	}
	return n.set("on", append(on, ev))
}

type A struct {
	Left, Right, Top, Bottom, CenterX, CenterY *float64
}

func Px(v float64) *float64 { return &v }

func (n *Node) Anchor(a A) *Node {
	m := map[string]any{}
	put := func(k string, v *float64) {
		if v != nil {
			m[k] = *v
		}
	}
	put("left", a.Left)
	put("right", a.Right)
	put("top", a.Top)
	put("bottom", a.Bottom)
	put("centerX", a.CenterX)
	put("centerY", a.CenterY)
	return n.set("anchors", m)
}

func (n *Node) Frame(x, y, w, h float64) *Node { return n.set("frame", []float64{x, y, w, h}) }
func (n *Node) Dock(side string) *Node         { return n.set("dock", side) }

func (n *Node) WindowDrag() *Node { return n.set("windowDrag", true) }
func (n *Node) W(w float64) *Node { return n.set("width", w) }
func (n *Node) H(h float64) *Node { return n.set("height", h) }
func (n *Node) Fill(weight float64) *Node {
	return n.set("stack", map[string]any{"kind": "fill", "weight": weight})
}
func (n *Node) Fixed(px float64) *Node {
	return n.set("stack", map[string]any{"kind": "fixed", "px": px})
}
func (n *Node) Clips() *Node { return n.set("clips", true) }

func (n *Node) NoFade() *Node { return n.set("noFade", true) }

func (n *Node) Pad(p float64) *Node         { return n.set("padding", p) }
func (n *Node) Gap(g float64) *Node         { return n.set("spacing", g) }
func (n *Node) DockGap(g float64) *Node     { return n.set("gap", g) }
func (n *Node) Align(a Align) *Node         { return n.set("align", string(a)) }
func (n *Node) InsetBottom(v float64) *Node { return n.set("insetBottom", v) }
func (n *Node) ColGap(g float64) *Node      { return n.set("colGap", g) }
func (n *Node) RowGap(g float64) *Node      { return n.set("rowGap", g) }

func (n *Node) StickBottom() *Node { return n.set("stickBottom", true) }

func (n *Node) Span(cols int) *Node { return n.set("span", cols) }

func (n *Node) Bg(c string) *Node    { return n.set("bg", c) }
func (n *Node) Color(c string) *Node { return n.set("color", c) }
func (n *Node) Border(c string, width float64) *Node {
	return n.set("borderColor", c).set("borderWidth", width)
}
func (n *Node) Radius(r float64) *Node { return n.set("radius", r) }
func (n *Node) RadiusCorners(tl, tr, br, bl float64) *Node {
	return n.set("radius", map[string]any{"tl": tl, "tr": tr, "br": br, "bl": bl})
}
func (n *Node) Shadow(blur, dx, dy float64, c string) *Node {
	return n.set("shadow", map[string]any{"blur": blur, "dx": dx, "dy": dy, "color": c})
}

func (n *Node) FontSize(s float64) *Node { return n.set("size", s) }
func (n *Node) Weight(w int) *Node       { return n.set("weight", w) }
func (n *Node) Italic() *Node            { return n.set("italic", true) }
func (n *Node) Mono() *Node              { return n.set("mono", true) }

func (n *Node) Font(family string) *Node { return n.set("font", family) }
func (n *Node) Sensitive() *Node         { return n.set("sensitive", true) }
func (n *Node) Selectable() *Node        { return n.set("selectable", true) }
func (n *Node) Primary() *Node           { return n.set("primary", true) }

func (n *Node) Wrap() *Node { return n.set("wrap", true) }

func (n *Node) Tip(s string) *Node { return n.set("tip", s) }

func (n *Node) Sound(token string) *Node { return n.set("sound", token) }

type ContextItem struct {
	Title  string
	Sep    bool
	OnPick func()
}

func (n *Node) Context(items ...ContextItem) *Node {
	specs := make([]any, 0, len(items))
	n.contextHandlers = map[int]func(){}
	id := 0
	for _, it := range items {
		if it.Sep {
			specs = append(specs, map[string]any{"sep": true})
			continue
		}
		id++
		specs = append(specs, map[string]any{"id": id, "title": it.Title})
		n.contextHandlers[id] = it.OnPick
	}
	n.set("context", specs)
	return n.subscribe(wire.EvContext)
}

type SelectionItem struct {
	Title  string
	Sep    bool
	OnPick func(start, end int)
}

func (n *Node) SelectionContext(items ...SelectionItem) *Node {
	specs := make([]any, 0, len(items))
	n.selectionHandlers = map[int]func(int, int){}
	id := 0
	for _, it := range items {
		if it.Sep {
			specs = append(specs, map[string]any{"sep": true})
			continue
		}
		id++
		specs = append(specs, map[string]any{"id": id, "title": it.Title})
		n.selectionHandlers[id] = it.OnPick
	}
	n.set("selectionContext", specs)
	return n.subscribe(wire.EvSelContext)
}

func (n *Node) Rows(lines int) *Node { return n.set("rows", lines) }
func (n *Node) EnterSends() *Node    { return n.set("enterSends", true) }

func (n *Node) SetText(s string) *Node       { return n.set("text", s) }
func (n *Node) SetSpans(spans ...Span) *Node { return n.set("spans", spans) }
func (n *Node) SetLabel(s string) *Node      { return n.set("label", s) }
func (n *Node) SetChecked(b bool) *Node      { return n.set("checked", b) }
func (n *Node) SetColor(c string) *Node      { return n.set("color", c) }
func (n *Node) Placeholder(s string) *Node   { return n.set("placeholder", s) }

func (n *Node) SetValue(s string) *Node { return n.set("value", s) }

type Fit string

const (
	FitContain Fit = "contain"
	FitCover   Fit = "cover"
	FitFill    Fit = "fill"
)

func (n *Node) Fit(mode Fit) *Node { return n.set("fit", string(mode)) }

func (n *Node) Alt(s string) *Node { return n.set("alt", s) }

func (n *Node) SetSrc(s string) *Node { return n.set("src", s) }

func (n *Node) Focus() *Node {
	if n.sess == nil {
		n.wantsFocus = true
		return n
	}
	n.sess.focus = n
	n.sess.focusPending = true
	return n
}

func (n *Node) Reveal() *Node {
	if n.sess != nil {
		n.sess.queueCmd(n.id, wire.CmdReveal, nil)
	}
	return n
}

func (n *Node) ClearValue() *Node {
	n.props["value"] = ""
	if n.sess != nil {
		n.sess.queueCmd(n.id, wire.CmdClear, nil)
	}
	return n
}

func (n *Node) SetValueNow(v string) *Node {
	n.props["value"] = v
	if n.sess != nil {
		n.sess.queueCmd(n.id, wire.CmdValue, v)
	}
	return n
}

func (n *Node) RowsFunc(fn func(start, end int) [][]string) *Node {
	n.rowsFn = fn
	return n
}

func (n *Node) OnSort(fn func(key string, asc bool)) *Node {
	n.onSort = fn
	return n.subscribe(wire.EvSort)
}

func (n *Node) OnRowSelect(fn func(row int)) *Node {
	n.onRowSelect = fn
	return n.subscribe(wire.EvRowSelect)
}

func (n *Node) RowKey(col int) *Node { return n.set("rowKey", col) }

func (n *Node) OnRowSelectKey(fn func(key string, row int)) *Node {
	n.onRowSelectKey = fn
	return n.subscribe(wire.EvRowSelect)
}

func (n *Node) OnRowActivate(fn func(row int)) *Node {
	n.onRowActivate = fn
	return n.subscribe(wire.EvRowActivate)
}

func (n *Node) OnRowActivateKey(fn func(key string, row int)) *Node {
	n.onRowActivateKey = fn
	return n.subscribe(wire.EvRowActivate)
}

func (n *Node) OnColResize(fn func(key string, width float64)) *Node {
	n.onColResize = fn
	return n.subscribe(wire.EvColResize)
}

func (n *Node) RowHeight(h float64) *Node { return n.set("rowHeight", h) }

func (n *Node) SetRowCount(c int) *Node {
	n.set("rowCount", c)
	return n.RefreshRows()
}

func (n *Node) SetSelected(row int) *Node { return n.set("selected", row) }

func (n *Node) SetSelectedKey(key string) *Node { return n.set("selectedKey", key) }

func (n *Node) RefreshRows() *Node {
	if n.sess != nil && n.hasRange {
		if rows, meta := n.rowsWindow(n.lastStart, n.lastEnd); rows != nil {
			n.sess.queueRows(n.id, n.lastStart, rows, meta, true)
		}
	}
	return n
}

func (n *Node) rowsWindow(start, end int) ([][]string, []map[string]any) {
	if n.typ == wire.Tree {
		return n.treeRowsWindow(start, end)
	}
	if n.rowsFn == nil {
		return nil, nil
	}
	return n.rowsFn(start, end), nil
}

type TreeItem struct {
	Key   string
	Cells []string
	Kids  []TreeItem
}

type flatRow struct {
	key        string
	cells      []string
	depth      int
	expandable bool
	expanded   bool
}

func Tree(cols []Col, items []TreeItem) *Node {
	n := newNode(wire.Tree).set("columns", colSpecs(cols))
	n.treeOpen = map[string]bool{}
	n.SetTreeItems(items)
	n.subscribe(wire.EvVisibleRange)
	return n.subscribe(wire.EvToggle)
}

func (n *Node) SetTreeItems(items []TreeItem) *Node {
	n.treeItems = items
	n.reflatten()
	return n.RefreshRows()
}

func (n *Node) OnRowToggle(fn func(key string, expanded bool)) *Node {
	n.onTreeToggle = fn
	return n
}

func (n *Node) OnCellActivate(fn func(row int, key, col, value string)) *Node {
	n.onCellActivate = fn
	return n.subscribe(wire.EvCellActivate)
}

func (n *Node) Expand(keys ...string) *Node {
	for _, k := range keys {
		n.treeOpen[k] = true
	}
	n.reflatten()
	return n.RefreshRows()
}

func (n *Node) Collapse(keys ...string) *Node {
	for _, k := range keys {
		delete(n.treeOpen, k)
	}
	n.reflatten()
	return n.RefreshRows()
}

func (n *Node) toggleTreeRow(key string) {
	if n.treeOpen[key] {
		delete(n.treeOpen, key)
	} else {
		n.treeOpen[key] = true
	}
	n.reflatten()
	n.RefreshRows()
	if n.onTreeToggle != nil {
		n.onTreeToggle(key, n.treeOpen[key])
	}
}

func (n *Node) reflatten() {
	n.treeFlat = n.treeFlat[:0]
	var walk func(items []TreeItem, depth int)
	walk = func(items []TreeItem, depth int) {
		for _, it := range items {
			open := n.treeOpen[it.Key] && len(it.Kids) > 0
			n.treeFlat = append(n.treeFlat, flatRow{
				key: it.Key, cells: it.Cells, depth: depth,
				expandable: len(it.Kids) > 0, expanded: open,
			})
			if open {
				walk(it.Kids, depth+1)
			}
		}
	}
	walk(n.treeItems, 0)
	n.set("rowCount", len(n.treeFlat))
}

func (n *Node) treeRowsWindow(start, end int) ([][]string, []map[string]any) {
	if start < 0 {
		start = 0
	}
	rows := make([][]string, 0, max(0, end-start+1))
	meta := make([]map[string]any, 0, cap(rows))
	for i := start; i <= end && i < len(n.treeFlat); i++ {
		f := n.treeFlat[i]
		rows = append(rows, f.cells)
		m := map[string]any{"key": f.key, "d": f.depth}
		if f.expandable {
			m["k"] = true
			if f.expanded {
				m["x"] = true
			}
		}
		meta = append(meta, m)
	}
	return rows, meta
}

type nodeJSON struct {
	ID   int            `json:"id"`
	Type wire.Type      `json:"type"`
	P    map[string]any `json:"p,omitempty"`
	Kids []nodeJSON     `json:"kids,omitempty"`
}

func (n *Node) toJSON() nodeJSON {
	j := nodeJSON{ID: n.id, Type: n.typ, P: n.props}
	for _, k := range n.kids {
		j.Kids = append(j.Kids, k.toJSON())
	}
	return j
}

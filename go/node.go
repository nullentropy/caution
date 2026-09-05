package caution

import "sort"

// Node is one widget in the tree. The zero id means "not yet attached";
// ids are assigned when a subtree joins a session.
type Node struct {
	id     int
	typ    string
	props  map[string]any
	kids   []*Node
	parent *Node
	sess   *Session
	// born is the seq of the message that introduced this id to the client.
	// Events carrying an older seq were fired before the client could have
	// seen this node (its id was recycled by a remount) and are dropped.
	born int

	onClick  func()
	onToggle func(bool)
	onInput  func(string)
	onCommit func(string)

	onSelect      func(index int)
	onDismiss     func()
	onSplitResize func(pos float64)

	// table state
	rowsFn           func(start, end int) [][]string
	onSort           func(key string, asc bool)
	onRowSelect      func(row int)
	onRowSelectKey   func(key string, row int)
	onRowActivate    func(row int)
	onRowActivateKey func(key string, row int)

	// tree state (type "tree"): items are data, the node owns expansion,
	// and the flattened visible slice ships as virtualized rows + meta.
	treeItems    []TreeItem
	treeOpen     map[string]bool
	treeFlat     []flatRow
	onTreeToggle func(key string, expanded bool)

	// in-cell widget columns (Col.Kind)
	onCellActivate func(row int, key, col, value string)

	// slider handlers (numeric input/commit)
	onSlide    func(v float64)
	onSlideEnd func(v float64)

	// glass handlers (raw pointer reports; see Glass)
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

	// monotonic counters for one-shot client commands carried as props
	focusSeq    int
	resetSeq    int
	overrideSeq int
	revealSeq   int

	contextHandlers map[int]func()
}

func newNode(typ string) *Node {
	return &Node{typ: typ, props: map[string]any{}}
}

// -- constructors -------------------------------------------------------------

func Panel() *Node              { return newNode("panel") }
func Label(text string) *Node   { return newNode("label").set("text", text) }
func Button(label string) *Node { return newNode("button").set("label", label) }
func VStack() *Node             { return newNode("vstack") }
func HStack() *Node             { return newNode("hstack") }
func DockPanel() *Node          { return newNode("dock") }
func Scroll() *Node             { return newNode("scroll") }
func Checkbox(label string, checked bool) *Node {
	return newNode("checkbox").set("label", label).set("checked", checked)
}
func TextField(value string) *Node { return newNode("textfield").set("value", value) }

// Textarea is the multi-line text editor. Enter inserts a newline and never
// commits. Commit fires on blur, and natively on Cmd+Enter.
// Rows sets its visible height.
func Textarea(value string) *Node { return newNode("textarea").set("value", value) }

// Image displays a bitmap from a URL or data: URI. The client loads the
// texture asynchronously (placeholder until ready). Size it with W/H or
// anchors. Fit controls aspect handling, Radius rounds corners, Alt is what
// screen readers hear.
func Image(src string) *Node { return newNode("image").set("src", src) }

// Select is a dropdown. The open list is client-local and picking emits `select`.
func Select(options []string, selected int) *Node {
	return newNode("select").set("options", options).set("selected", selected)
}

// Progress is a determinate progress bar. The value is 0..1. Display-only.
func Progress(value float64) *Node { return newNode("progress").set("value", value) }

// Slider is a horizontal slider over [min, max]. Dragging emits `input`
// (debounced). Release or a key press emits `commit`. Step(0) means continuous.
func Slider(min, max, value float64) *Node {
	return newNode("slider").set("min", min).set("max", max).set("value", value)
}

// Radio is a vertical radio group. Picking emits `select` with the index.
func Radio(options []string, selected int) *Node {
	return newNode("radio").set("options", options).set("selected", selected)
}

// TabBar is a row of tabs. Picking emits `select` with the index. What the
// tabs switch is app policy (swap a panel's children in the handler).
func TabBar(options []string, selected int) *Node {
	return newNode("tabs").set("options", options).set("selected", selected)
}

// Step quantizes a slider (0 = continuous).
func (n *Node) Step(s float64) *Node { return n.set("step", s) }

// SetProgress updates a progress bar or slider value (0..1 for progress).
func (n *Node) SetProgress(v float64) *Node { return n.set("value", v) }

// OnSlide fires as a slider drags (client-debounced ~100ms).
func (n *Node) OnSlide(fn func(v float64)) *Node {
	n.onSlide = fn
	return n.subscribe("input")
}

// OnSlideEnd fires when a slider drag releases (or a key press adjusts it).
func (n *Node) OnSlideEnd(fn func(v float64)) *Node {
	n.onSlideEnd = fn
	return n.subscribe("commit")
}

// Dialog is a modal: a full-viewport scrim and a centered card whose children lay
// out inside the card's content area. Append it last so it paints on top.
// Scrim clicks and Escape emit `dismiss`. Removing the node is app policy.
func Dialog(title string) *Node {
	return newNode("dialog").set("title", title).
		Anchor(A{Left: Px(0), Right: Px(0), Top: Px(0), Bottom: Px(0)})
}

// HSplit / VSplit are two-pane resizable splits (nest them for more panes).
// The first two children are the panes. The divider drags client-side and
// commits `split-resize` on release.
func HSplit() *Node { return newNode("split").set("axis", "h") }
func VSplit() *Node { return newNode("split").set("axis", "v") }

// FX is an app-supplied fragment shader. Frag must define
// `vec4 effect(vec2 uv)` (uv origin top-left) and may use u_time (seconds),
// u_res (logical px), any Uniforms (floats), and, for Effect() only,
// `src(uv)`: the widget subtree rendered to a texture. Animate repaints
// continuously while visible. Leave it false for static effects so the
// damage-driven idle is preserved.
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

// Effect renders this widget's subtree offscreen and composites it back
// through the shader: CRT looks, glow, blur, transitions. Visual only, so
// hit-testing sees the undistorted widgets.
func (n *Node) Effect(fx FX) *Node { return n.set("effect", fxProps(fx)) }

// Shader is a pane painted entirely by the fragment shader: procedural
// backdrops, visualizations, raymarched scenes.
func Shader(fx FX) *Node {
	sh := newNode("shader").set("frag", fx.Frag)
	if fx.Animate {
		sh.set("animate", true)
	}
	if len(fx.Uniforms) > 0 {
		sh.set("uniforms", fx.Uniforms)
	}
	return sh
}

// Glass is a transparent pointer-capture surface for custom interaction.
// It paints nothing. While OnPick is subscribed it swallows presses over its
// bounds and reports them with the position (glass-relative, logical px) and
// the deepest node under the point. Widgets underneath keep animating and
// patching, but receive no input.
func Glass() *Node { return newNode("glass") }

// OnPick fires on press. target is the deepest session node under the
// point (resolved client-side, where layout lives), or nil over dead space.
func (n *Node) OnPick(fn func(x, y float64, target *Node)) *Node {
	n.onPick = fn
	return n.subscribe("pick")
}

// OnDragTo fires while dragging after a pick, coalesced client-side
// (~30/s); the final position always arrives via OnDrop.
func (n *Node) OnDragTo(fn func(x, y float64)) *Node {
	n.onDragTo = fn
	return n.subscribe("drag")
}

// OnWheel fires for scroll-wheel input over the glass: glass-relative
// position plus deltas, coalesced client-side like drags (deltas accumulate
// between sends, so no distance is lost). While subscribed, the glass owns
// the wheel over its bounds, so nothing beneath it scrolls.
func (n *Node) OnWheel(fn func(x, y, dx, dy float64)) *Node {
	n.onWheel = fn
	return n.subscribe("wheel")
}

// OnRightPick fires on a right-button press over the glass. Subscribing
// claims the right button for the app: over this glass it starts a captured
// right-drag gesture (orbit, measure, lasso) INSTEAD of opening a context
// menu. Positions are glass-relative, like every glass report.
func (n *Node) OnRightPick(fn func(x, y float64)) *Node {
	n.onRPick = fn
	return n.subscribe("rpick")
}

// OnRightDragTo fires while right-dragging after a right pick, coalesced
// client-side (~30/s); the final position always arrives via OnRightDrop.
func (n *Node) OnRightDragTo(fn func(x, y float64)) *Node {
	n.onRDrag = fn
	return n.subscribe("rdrag")
}

// OnRightDrop fires on right release after a right pick. It is always the true
// endpoint, since a quick gesture can coalesce down to rpick+rdrop alone.
func (n *Node) OnRightDrop(fn func(x, y float64)) *Node {
	n.onRDrop = fn
	return n.subscribe("rdrop")
}

// OnDrop fires on release after a pick.
func (n *Node) OnDrop(fn func(x, y float64)) *Node {
	n.onDrop = fn
	return n.subscribe("drop")
}

// SetUniforms live-updates a Shader pane's custom uniforms. Uniform maps are
// merge-only: the wire carries just the keys given here and the client merges
// them into what it has, and the node's stored map stays complete so a
// remount ships everything. Nothing un-sets a key (overwrite it instead).
func (n *Node) SetUniforms(u map[string]float64) *Node {
	n.props["uniforms"] = mergedUniforms(n.props["uniforms"], u)
	if n.sess != nil {
		n.sess.queueSet(n.id, "uniforms", u)
	}
	return n
}

// SetUniform live-updates one uniform. Only that key travels. See
// SetUniforms for the merge contract.
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

// GridCol describes one grid column: Px pins it, Weight shares leftover
// space, neither means content-sized. Align places cells within the column
// ("start" default, "center", "end", "stretch").
type GridCol struct {
	Px     float64
	Weight float64
	Align  string
}

// Grid is an NSGridView-style rows x columns container for forms and
// inspectors. Children flow row-major, Span(n) spans columns, and rows auto-size
// and vertically center their cells.
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
			m["align"] = c.Align
		}
		arr = append(arr, m)
	}
	return newNode("grid").set("columns", arr)
}

// Col describes a table column. Width pins it in px, otherwise Weight shares
// the remaining space (default weight 1). Kind makes it an in-cell widget
// column. Cells stay strings and the column says how to render them:
//
//	""         plain text (default)
//	"button"   the cell is the label, and a click emits cell-activate
//	"checkbox" the cell is truthy ("true"/"1"); a click flips it locally and
//	           emits cell-activate with the proposed value, and the app updates
//	           its data (or refreshes to veto)
//	"progress" the cell is a 0..1 float, display-only
//
// Cells are always strings, never node subtrees.
type Col struct {
	Key, Title    string
	Weight, Width float64
	Kind          string
}

// Table creates a virtualized table over rowCount rows. Rows are data, not
// nodes. Provide them on demand with RowsFunc, and the client requests windows as
// the user scrolls.
func Table(cols []Col, rowCount int) *Node {
	n := newNode("table").set("columns", colSpecs(cols)).set("rowCount", rowCount)
	return n.subscribe("visible-range")
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
			m["kind"] = c.Kind
		}
		arr = append(arr, m)
	}
	return arr
}

// -- tree mutation ------------------------------------------------------------

// Add appends child and returns it. If this subtree is live, the client
// receives an insert op carrying the fully-built child subtree.
func (n *Node) Add(child *Node) *Node {
	child.parent = n
	n.kids = append(n.kids, child)
	if n.sess != nil {
		n.sess.attach(child)
		n.sess.queueInsert(n.id, len(n.kids)-1, child)
	}
	return child
}

// Kids appends children and returns n, for declarative tree building.
func (n *Node) Kids(kids ...*Node) *Node {
	for _, k := range kids {
		n.Add(k)
	}
	return n
}

// Remove detaches n from its parent (and from the client).
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

// set records a prop and, when live, streams it as a patch op.
func (n *Node) set(k string, v any) *Node {
	n.props[k] = v
	if n.sess != nil {
		n.sess.queueSet(n.id, k, v)
	}
	return n
}

// setMulti records several props and streams them as ONE set op, for
// commands whose meaning depends on the props arriving together
// (SetValueNow's value + overrideSeq).
func (n *Node) setMulti(props map[string]any) *Node {
	for k, v := range props {
		n.props[k] = v
	}
	if n.sess != nil {
		n.sess.queueSetMulti(n.id, props)
	}
	return n
}

// -- reflection ---------------------------------------------------------------

// Type returns the node's widget type ("panel", "label", ...).
func (n *Node) Type() string { return n.typ }

// Parent returns the node's parent, or nil for a root.
func (n *Node) Parent() *Node { return n.parent }

// Children returns the node's children as a copy. Mutate via Add/Remove.
func (n *Node) Children() []*Node { return append([]*Node(nil), n.kids...) }

// Prop returns a prop value as stored
func (n *Node) Prop(k string) any { return n.props[k] }

// PropKeys returns the node's prop names, sorted.
func (n *Node) PropKeys() []string {
	keys := make([]string, 0, len(n.props))
	for k := range n.props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SetProp sets any prop by its wire name, for tooling that needs props the
// typed helpers do not cover.
func (n *Node) SetProp(k string, v any) *Node { return n.set(k, v) }

// Clear removes all children.
func (n *Node) Clear() *Node {
	for _, k := range n.Children() {
		k.Remove()
	}
	return n
}

// -- events -------------------------------------------------------------------

func (n *Node) OnClick(fn func()) *Node { n.onClick = fn; return n.subscribe("click") }
func (n *Node) OnToggle(fn func(bool)) *Node {
	n.onToggle = fn
	return n.subscribe("toggle")
}

// OnInput fires as the user types (debounced client-side, ~150ms).
func (n *Node) OnInput(fn func(string)) *Node {
	n.onInput = fn
	return n.subscribe("input")
}

// OnCommit fires on Enter or blur.
func (n *Node) OnCommit(fn func(string)) *Node {
	n.onCommit = fn
	return n.subscribe("commit")
}

// OnSelect fires when a dropdown option is picked.
func (n *Node) OnSelect(fn func(index int)) *Node {
	n.onSelect = fn
	return n.subscribe("select")
}

// OnDismiss fires on dialog scrim click / Escape.
func (n *Node) OnDismiss(fn func()) *Node {
	n.onDismiss = fn
	return n.subscribe("dismiss")
}

// CardSize sets a dialog's card dimensions.
func (n *Node) CardSize(w, h float64) *Node {
	return n.set("cardWidth", w).set("cardHeight", h)
}

// SplitPos sets the first pane's size in px (clamped by SplitMin at layout).
func (n *Node) SplitPos(px float64) *Node { return n.set("pos", px) }

// SplitMin sets the minimum size of each pane.
func (n *Node) SplitMin(first, second float64) *Node {
	return n.set("minA", first).set("minB", second)
}

// OnSplitResize fires when the user releases the divider.
func (n *Node) OnSplitResize(fn func(pos float64)) *Node {
	n.onSplitResize = fn
	return n.subscribe("split-resize")
}

func (n *Node) subscribe(ev string) *Node {
	on, _ := n.props["on"].([]string)
	for _, e := range on {
		if e == ev {
			return n
		}
	}
	return n.set("on", append(on, ev))
}

// -- layout props -------------------------------------------------------------

// A is a springs-and-struts anchor spec. Nil fields are unset. Use Px.
type A struct {
	Left, Right, Top, Bottom, CenterX, CenterY *float64
}

// Px wraps a literal for use in A.
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

// WindowDrag marks this subtree as a window-move region, for an app-drawn
// titlebar (terminal/native Options.CustomTitlebar). A press here that no
// interactive widget claims moves the window, and a double-click zooms.
// Interactive widgets inside the region still work; give them an explicit
// H(), since the control.height metric (32) overflows a typical 38px strip.
// Ignored by the browser terminal.
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

// -- container props ----------------------------------------------------------

func (n *Node) Pad(p float64) *Node         { return n.set("padding", p) }
func (n *Node) Gap(g float64) *Node         { return n.set("spacing", g) }
func (n *Node) DockGap(g float64) *Node     { return n.set("gap", g) }
func (n *Node) Align(a string) *Node        { return n.set("align", a) }
func (n *Node) InsetBottom(v float64) *Node { return n.set("insetBottom", v) }
func (n *Node) ColGap(g float64) *Node      { return n.set("colGap", g) }
func (n *Node) RowGap(g float64) *Node      { return n.set("rowGap", g) }

// Span makes this child cover n columns of its parent Grid.
func (n *Node) Span(cols int) *Node { return n.set("span", cols) }

// -- visual props -------------------------------------------------------------

// Colors are "#rrggbb"/"#rrggbbaa" or theme tokens like "$accent".
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

// -- text props ---------------------------------------------------------------

func (n *Node) FontSize(s float64) *Node { return n.set("size", s) }
func (n *Node) Weight(w int) *Node       { return n.set("weight", w) }
func (n *Node) Italic() *Node            { return n.set("italic", true) }
func (n *Node) Mono() *Node              { return n.set("mono", true) }
func (n *Node) Selectable() *Node        { return n.set("selectable", true) }
func (n *Node) Primary() *Node           { return n.set("primary", true) }

// Wrap makes a label break across lines at its laid-out width instead of
// truncating; its intrinsic height follows the wrapped line count.
func (n *Node) Wrap() *Node { return n.set("wrap", true) }

// Tip attaches tooltip text to any widget: client-local, shown after a
// short hover idle, never an event.
func (n *Node) Tip(s string) *Node { return n.set("tip", s) }

// Sound picks the gesture token this widget fires instead of its own, or "none"
// to silence it. See Session.SetSounds.
func (n *Node) Sound(token string) *Node { return n.set("sound", token) }

// ContextItem is one entry in a node's right-click context menu.
type ContextItem struct {
	Title  string
	Sep    bool
	OnPick func()
}

// Context attaches a right-click menu to this widget's subtree. The
// deepest carrier under the pointer wins. The menu opens client-locally;
// a pick comes back as one `context` event and runs its OnPick on the
// session goroutine, like any other handler.
func (n *Node) Context(items ...ContextItem) *Node {
	wire := make([]any, 0, len(items))
	n.contextHandlers = map[int]func(){}
	id := 0
	for _, it := range items {
		if it.Sep {
			wire = append(wire, map[string]any{"sep": true})
			continue
		}
		id++
		wire = append(wire, map[string]any{"id": id, "title": it.Title})
		n.contextHandlers[id] = it.OnPick
	}
	n.set("context", wire)
	return n.subscribe("context")
}

// Rows sets a textarea's visible line count (its intrinsic height).
func (n *Node) Rows(lines int) *Node { return n.set("rows", lines) }

// -- live updates -------------------------------------------------------------

func (n *Node) SetText(s string) *Node     { return n.set("text", s) }
func (n *Node) SetLabel(s string) *Node    { return n.set("label", s) }
func (n *Node) SetChecked(b bool) *Node    { return n.set("checked", b) }
func (n *Node) SetColor(c string) *Node    { return n.set("color", c) }
func (n *Node) Placeholder(s string) *Node { return n.set("placeholder", s) }

// SetValue pushes a value to a text field. Note the local-echo rule: a field
// the user is currently editing ignores this until it blurs.
func (n *Node) SetValue(s string) *Node { return n.set("value", s) }

// Fit sets image aspect handling: "contain" (default), "cover", or "fill".
func (n *Node) Fit(mode string) *Node { return n.set("fit", mode) }

// Alt sets an image's screen-reader description.
func (n *Node) Alt(s string) *Node { return n.set("alt", s) }

// SetSrc swaps an image's source. A placeholder shows until the new texture
// finishes loading.
func (n *Node) SetSrc(s string) *Node { return n.set("src", s) }

// Focus asks the client to give this widget keyboard focus. Any focusable
// widget, not just text fields (which additionally rebind their input
// funnel). Carried as a monotonic prop so repeated calls each take effect.
func (n *Node) Focus() *Node {
	n.focusSeq++
	return n.set("focusSeq", n.focusSeq)
}

// Reveal scrolls the client's enclosing scrollers to bring this widget into
// view. Focus does not change.
func (n *Node) Reveal() *Node {
	n.revealSeq++
	return n.set("revealSeq", n.revealSeq)
}

// ClearValue empties a text field even while it is focused, unlike SetValue,
// for submit-and-keep-typing flows like a terminal or a chat input.
func (n *Node) ClearValue() *Node {
	n.props["value"] = ""
	n.resetSeq++
	return n.set("resetSeq", n.resetSeq)
}

// SetValueNow pushes a value into a text field even while the user is editing
// it, for validation and formatting. The pushed value becomes the field's new
// baseline: Escape reverts to it, and no commit fires unless the user changes
// it again. The value and the override marker travel in one op, so they can
// never apply separately.
func (n *Node) SetValueNow(v string) *Node {
	n.overrideSeq++
	return n.setMulti(map[string]any{"value": v, "overrideSeq": n.overrideSeq})
}

// -- table --------------------------------------------------------------------

// RowsFunc supplies row cells for the inclusive window [start, end].
func (n *Node) RowsFunc(fn func(start, end int) [][]string) *Node {
	n.rowsFn = fn
	return n
}

// OnSort fires when a column header is clicked. The handler should reorder
// its data and call RefreshRows.
func (n *Node) OnSort(fn func(key string, asc bool)) *Node {
	n.onSort = fn
	return n.subscribe("sort")
}

func (n *Node) OnRowSelect(fn func(row int)) *Node {
	n.onRowSelect = fn
	return n.subscribe("row-select")
}

// RowKey declares the column whose cells are stable row keys. With keys,
// the client selects by *key* instead of index, so selection survives sorts
// and data changes. Row-select events carry {row, key} and echo into the
// selectedKey prop. Keys should be unique (an id column).
func (n *Node) RowKey(col int) *Node { return n.set("rowKey", col) }

// OnRowSelectKey is OnRowSelect for keyed tables: the handler gets the
// stable key plus the row's index at click time.
func (n *Node) OnRowSelectKey(fn func(key string, row int)) *Node {
	n.onRowSelectKey = fn
	return n.subscribe("row-select")
}

// OnRowActivate fires when a row is double-clicked or Enter is pressed on
// it, the "open" gesture. Activation implies selection, so the row-select
// event (and its local echo) always precedes it.
func (n *Node) OnRowActivate(fn func(row int)) *Node {
	n.onRowActivate = fn
	return n.subscribe("row-activate")
}

// OnRowActivateKey is OnRowActivate for keyed tables and trees: the handler
// gets the stable key plus the row's index at activation time.
func (n *Node) OnRowActivateKey(fn func(key string, row int)) *Node {
	n.onRowActivateKey = fn
	return n.subscribe("row-activate")
}

// OnColResize fires when the user drags a column divider. Widths are
// client-local, so persist them and re-apply via Col.Width if resizes should
// survive a remount.
func (n *Node) OnColResize(fn func(key string, width float64)) *Node {
	n.onColResize = fn
	return n.subscribe("col-resize")
}

func (n *Node) RowHeight(h float64) *Node { return n.set("rowHeight", h) }

func (n *Node) SetRowCount(c int) *Node {
	n.set("rowCount", c)
	return n.RefreshRows()
}

func (n *Node) SetSelected(row int) *Node { return n.set("selected", row) }

// SetSelectedKey selects a row by key on a keyed table ("" clears).
func (n *Node) SetSelectedKey(key string) *Node { return n.set("selectedKey", key) }

// RefreshRows re-sends the client's last reported window with the reset flag
// (the row cache is stale because sort order, data, or tree expansion changed).
func (n *Node) RefreshRows() *Node {
	if n.sess != nil && n.hasRange {
		if rows, meta := n.rowsWindow(n.lastStart, n.lastEnd); rows != nil {
			n.sess.queueRows(n.id, n.lastStart, rows, meta, true)
		}
	}
	return n
}

// rowsWindow produces the [start, end] slice for the rows op: a table's from
// its RowsFunc (no meta), a tree's from the flattened visible items.
func (n *Node) rowsWindow(start, end int) ([][]string, []map[string]any) {
	if n.typ == "tree" {
		return n.treeRowsWindow(start, end)
	}
	if n.rowsFn == nil {
		return nil, nil
	}
	return n.rowsFn(start, end), nil
}

// -- tree ---------------------------------------------------------------------

// TreeItem is one node of a Tree's data: a stable key, its row cells, and
// its children. Items are data like table rows, not Nodes, so the wire
// carries only the expanded, visible slice the client is looking at.
type TreeItem struct {
	Key   string
	Cells []string
	Kids  []TreeItem
}

// flatRow is one visible row of the flattened tree.
type flatRow struct {
	key        string
	cells      []string
	depth      int
	expandable bool
	expanded   bool
}

// Tree creates a virtualized outline view: hierarchical rows over the same
// column model and rows protocol as Table. The node owns expansion state. A
// disclosure click (or Left/Right on the cursor row) emits `toggle`, the tree
// reflattens, and the client's window refreshes. Selection is keyed by
// TreeItem.Key (`selectedKey` / OnRowSelectKey), so it survives expansion
// changes above it. Everything starts collapsed. See Expand.
func Tree(cols []Col, items []TreeItem) *Node {
	n := newNode("tree").set("columns", colSpecs(cols))
	n.treeOpen = map[string]bool{}
	n.SetTreeItems(items)
	n.subscribe("visible-range")
	return n.subscribe("toggle")
}

// SetTreeItems replaces the tree's data (expansion state is kept for keys
// that still exist) and refreshes the client's window.
func (n *Node) SetTreeItems(items []TreeItem) *Node {
	n.treeItems = items
	n.reflatten()
	return n.RefreshRows()
}

// OnRowToggle fires after a row is expanded or collapsed (the tree has
// already reflattened and refreshed the client).
func (n *Node) OnRowToggle(fn func(key string, expanded bool)) *Node {
	n.onTreeToggle = fn
	return n
}

// OnCellActivate fires when an in-cell widget column is used: a button
// cell's click, or a checkbox cell's flip (value carries the proposed
// "true"/"false". The client has already echoed it locally, so update the
// row data to keep it, or RefreshRows to veto). row is the index at click
// time. Key is the row's selection identity ("" for keyless tables), col
// is the column key.
func (n *Node) OnCellActivate(fn func(row int, key, col, value string)) *Node {
	n.onCellActivate = fn
	return n.subscribe("cell-activate")
}

// Expand opens the given keys (parents are not opened implicitly: a row is
// visible only while its ancestors are expanded).
func (n *Node) Expand(keys ...string) *Node {
	for _, k := range keys {
		n.treeOpen[k] = true
	}
	n.reflatten()
	return n.RefreshRows()
}

// Collapse closes the given keys.
func (n *Node) Collapse(keys ...string) *Node {
	for _, k := range keys {
		delete(n.treeOpen, k)
	}
	n.reflatten()
	return n.RefreshRows()
}

// toggleTreeRow flips one key from a client toggle event.
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

// reflatten rebuilds the visible-row slice and keeps rowCount in sync.
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

// -- wire format --------------------------------------------------------------

type nodeJSON struct {
	ID   int            `json:"id"`
	Type string         `json:"type"`
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

package proto

import (
	"time"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"

	"github.com/rs/zerolog/log"
)

type props = map[string]any

type nodeJSON struct {
	ID   int        `json:"id"`
	Type wire.Type  `json:"type"`
	P    props      `json:"p"`
	Kids []nodeJSON `json:"kids"`
}

type EventSink interface {
	Event(id int, ev wire.Event, value any)
	Debounce(id int, ev wire.Event, value any, d time.Duration)
}

func num(v any) (float32, bool) {
	f, ok := v.(float64) // encoding/json numbers
	return float32(f), ok
}

func numP(v any) *float32 {
	if f, ok := num(v); ok {
		return &f
	}
	return nil
}

func str(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func boolean(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

// a set op carries only the props that changed, so a missing key leaves the
// field alone. a present key of the wrong type, null included, resets it to
// the zero value
func set[T propType](p props, key string, dst *T) {
	var zero T
	setOr(p, key, dst, zero)
}

// set with def in place of the zero value
func setOr[T propType](p props, key string, dst *T, def T) {
	v, ok := p[key]
	if !ok {
		return
	}
	*dst = def
	f, isNum := num(v)
	switch d := any(dst).(type) {
	case *string:
		if s, ok := str(v); ok {
			*d = s
		}
	case *bool:
		if b, ok := boolean(v); ok {
			*d = b
		}
	case *float32:
		if isNum {
			*d = f
		}
	case **float32:
		if isNum {
			*d = &f
		}
	case *int:
		if isNum {
			*d = int(f)
		}
	}
}

type propType interface {
	string | bool | float32 | *float32 | int
}

// choice props (radio, tabs, select) replace the whole list
func setOptions(p props, dst *[]string) {
	v, ok := p["options"].([]any)
	if !ok {
		return
	}
	*dst = (*dst)[:0]
	for _, o := range v {
		s, _ := str(o)
		*dst = append(*dst, s)
	}
}

func color(v any) *gfx.Color {
	s, ok := str(v)
	if !ok || s == "" {
		return nil
	}
	if s[0] == '$' {
		return ui.Tok(s[1:]) // nil for unknown tokens, like the browser
	}
	c := gfx.Hex(s)
	return &c
}

func radius(v any) (gfx.Corners, bool) {
	if f, ok := num(v); ok {
		return gfx.CornerRadius(f), true
	}
	if o, ok := v.(props); ok {
		g := func(k string) float32 {
			f, _ := num(o[k])
			return f
		}
		return gfx.Corners{TL: g("tl"), TR: g("tr"), BR: g("br"), BL: g("bl")}, true
	}
	return gfx.Corners{}, false
}

func anchors(v any) *ui.Anchors {
	o, ok := v.(props)
	if !ok {
		return nil
	}
	return &ui.Anchors{
		Left: numP(o["left"]), Right: numP(o["right"]),
		Top: numP(o["top"]), Bottom: numP(o["bottom"]),
		CenterX: numP(o["centerX"]), CenterY: numP(o["centerY"]),
	}
}

func frame(v any) *gfx.Rect {
	arr, ok := v.([]any)
	if !ok || len(arr) != 4 {
		return nil
	}
	var f [4]float32
	for i, n := range arr {
		val, ok := num(n)
		if !ok {
			return nil
		}
		f[i] = val
	}
	r := gfx.R(f[0], f[1], f[2], f[3])
	return &r
}

func floatMap(v any) map[string]float32 {
	out := map[string]float32{}
	if o, ok := v.(props); ok {
		for k, val := range o {
			if f, ok := num(val); ok {
				out[k] = f
			}
		}
	}
	return out
}

func effectSpec(v any) *ui.Effect {
	o, ok := v.(props)
	if !ok {
		return nil
	}
	frag, ok := str(o["frag"])
	if !ok || frag == "" {
		return nil
	}
	animate, _ := boolean(o["animate"])
	return &ui.Effect{Frag: frag, Uniforms: floatMap(o["uniforms"]), Animate: animate}
}

func stackSize(v any) (ui.StackSize, bool) {
	o, ok := v.(props)
	if !ok {
		return ui.StackSize{}, false
	}
	kind, _ := str(o["kind"])
	switch kind {
	case "fixed":
		if px, ok := num(o["px"]); ok {
			return ui.StackSize{Kind: "fixed", Px: px}, true
		}
	case "fill":
		w, ok := num(o["weight"])
		if !ok {
			w = 1
		}
		return ui.StackSize{Kind: "fill", Weight: w}, true
	case "content":
		return ui.StackSize{Kind: "content"}, true
	}
	return ui.StackSize{}, false
}

func applyCommon(w ui.Widget, p props) {
	b := w.Base()
	if v, ok := p["frame"]; ok {
		b.Frame = frame(v)
	}
	if v, ok := p["anchors"]; ok {
		b.Anchors = anchors(v)
	}
	if v, ok := p["dock"]; ok {
		s, _ := str(v)
		b.Dock = s
	}
	if v, ok := p["stack"]; ok {
		ss, valid := stackSize(v)
		if !valid {
			ss = ui.StackSize{Kind: "content"}
		}
		b.StackSize = ss
	}
	if v, ok := p["effect"]; ok {
		b.Effect = effectSpec(v)
	}
	set(p, "windowDrag", &b.WindowDrag)
	setOr(p, "span", &b.GridSpan, 1)
	set(p, "width", &b.Width)
	set(p, "height", &b.Height)
	set(p, "clips", &b.Clips)
	set(p, "noFade", &b.NoFade)
	set(p, "tip", &b.Tip)
	set(p, "sound", &b.SoundToken)
}

func contextItems(v any) []ui.ContextItem {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]ui.ContextItem, 0, len(arr))
	for _, iv := range arr {
		o, _ := iv.(props)
		var it ui.ContextItem
		if f, ok := num(o["id"]); ok {
			it.ID = int(f)
		}
		it.Title, _ = str(o["title"])
		it.Sep, _ = boolean(o["sep"])
		out = append(out, it)
	}
	return out
}

func spans(v any) []ui.Span {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]ui.Span, 0, len(arr))
	for _, iv := range arr {
		o, _ := iv.(props)
		var s ui.Span
		s.Text, _ = str(o["text"])
		s.Bold, _ = boolean(o["bold"])
		s.Italic, _ = boolean(o["italic"])
		s.Mono, _ = boolean(o["mono"])
		s.Color = color(o["color"])
		s.Bg = color(o["bg"])
		s.Action, _ = str(o["action"])
		s.Font, _ = str(o["font"])
		out = append(out, s)
	}
	return out
}

func applyLabelFont(l *ui.Label, p props) {
	prev := l.Font
	size := prev.Size
	if f, ok := num(p["size"]); ok {
		size = f
	}
	weight := prev.Weight
	if f, ok := num(p["weight"]); ok {
		weight = int(f)
	}
	italic := prev.Italic
	if b, ok := boolean(p["italic"]); ok {
		italic = b
	}
	mono := prev.Mono
	if b, ok := boolean(p["mono"]); ok {
		mono = b
	}
	family := prev.Family
	if v, ok := str(p["font"]); ok {
		family = v
	}
	l.Font = gfx.NewFont(size, gfx.FontOpts{Weight: weight, Italic: italic, Mono: mono, Family: family})
}

// construct by type, apply props, wire subscribed events to the sink
// owns the id->widget map patches use
type NodeStore struct {
	ByID map[int]ui.Widget
	idOf map[ui.Widget]int
	sink EventSink
}

func NewNodeStore(sink EventSink) *NodeStore {
	return &NodeStore{ByID: map[int]ui.Widget{}, idOf: map[ui.Widget]int{}, sink: sink}
}

// forgets every node for a fresh mount
func (ns *NodeStore) Reset() {
	clear(ns.ByID)
	clear(ns.idOf)
}

func (ns *NodeStore) Build(json nodeJSON) ui.Widget {
	w := construct(json.Type)
	ns.ByID[json.ID] = w
	ns.idOf[w] = json.ID
	ns.Apply(w, json.ID, json.P)
	for _, kid := range json.Kids {
		c := ns.Build(kid)
		c.Base().Parent = w
		w.Base().Kids = append(w.Base().Kids, c)
	}
	return w
}

func subscribed(p props, ev wire.Event) bool {
	arr, ok := p["on"].([]any)
	if !ok {
		return false
	}
	for _, v := range arr {
		if s, _ := str(v); wire.Event(s) == ev {
			return true
		}
	}
	return false
}

func (ns *NodeStore) Apply(w ui.Widget, id int, p props) {
	if p == nil {
		return
	}
	applyCommon(w, p)
	// context menus are type-agnostic like tips, but the pick needs the
	// node id so they wire here and not in applyCommon
	if v, ok := p["context"]; ok {
		w.Base().ContextItems = contextItems(v)
	}
	if subscribed(p, wire.EvContext) {
		w.Base().OnContextPick = func(cid int) { ns.sink.Event(id, wire.EvContext, cid) }
	}
	switch t := w.(type) {
	case *ui.Panel:
		if v, ok := p["bg"]; ok {
			t.Bg = color(v)
		}
		if v, ok := p["borderColor"]; ok {
			t.BorderColor = color(v)
		}
		setOr(p, "borderWidth", &t.BorderWidth, 1)
		if v, ok := p["radius"]; ok {
			t.Radius, _ = radius(v)
		}
		if v, ok := p["shadow"]; ok {
			t.DropShadow = nil
			if o, ok := v.(props); ok {
				blur := float32(10)
				if f, ok := num(o["blur"]); ok {
					blur = f
				}
				col := gfx.Hex("#00000080")
				if c := color(o["color"]); c != nil {
					col = *c
				}
				dx, _ := num(o["dx"])
				dy, _ := num(o["dy"])
				t.DropShadow = &ui.Shadow{Blur: blur, Color: col, Dx: dx, Dy: dy}
			}
		}
	case *ui.Label:
		set(p, "text", &t.Text)
		applyLabelFont(t, p)
		if v, ok := p["color"]; ok {
			if c := color(v); c != nil {
				t.Color = c
			} else {
				t.Color = ui.Tok("ink")
			}
		}
		set(p, "selectable", &t.Selectable)
		set(p, "wrap", &t.Wrap)
	case *ui.RichText:
		if v, ok := p["spans"]; ok {
			t.SetSpans(spans(v))
		}
		size, family := t.Font.Size, t.Font.Family
		if f, ok := num(p["size"]); ok {
			size = f
		}
		if v, ok := str(p["font"]); ok {
			family = v
		}
		t.Font = gfx.NewFont(size, gfx.FontOpts{Family: family})
		if v, ok := p["color"]; ok {
			if c := color(v); c != nil {
				t.Color = c
			} else {
				t.Color = ui.Tok("ink")
			}
		}
		set(p, "selectable", &t.Selectable)
		if subscribed(p, wire.EvSpanClick) {
			t.OnSpanClick = func(action string) { ns.sink.Event(id, wire.EvSpanClick, action) }
		}
	case *ui.Button:
		set(p, "label", &t.Label)
		set(p, "primary", &t.Primary)
		set(p, "radius", &t.Radius)
		if subscribed(p, wire.EvClick) {
			t.OnClick = func() { ns.sink.Event(id, wire.EvClick, nil) }
		}
	case *ui.Checkbox:
		set(p, "label", &t.Label)
		set(p, "checked", &t.Checked)
		if subscribed(p, wire.EvToggle) {
			t.OnToggle = func(checked bool) { ns.sink.Event(id, wire.EvToggle, checked) }
		}
	case *ui.TextField:
		ns.applyText(t, id, p)
	case *ui.TextArea:
		ns.applyText(&t.TextField, id, p)
		setOr(p, "rows", &t.Rows, 4)
	case *ui.Stack:
		set(p, "padding", &t.Padding)
		setOr(p, "spacing", &t.Spacing, 8)
		set(p, "align", &t.Align)
	case *ui.Dock:
		set(p, "padding", &t.Padding)
		set(p, "gap", &t.Gap)
	case *ui.ScrollView:
		set(p, "insetBottom", &t.ContentInsetBottom)
		set(p, "stickBottom", &t.StickBottom)
	case *ui.TableView:
		if v, ok := p["columns"].([]any); ok {
			t.Columns = t.Columns[:0]
			for _, cv := range v {
				o, _ := cv.(props)
				col := ui.TableColumn{}
				col.Key, _ = str(o["key"])
				col.Title, _ = str(o["title"])
				col.Weight, _ = num(o["weight"])
				col.Width, _ = num(o["width"])
				col.Kind, _ = str(o["kind"])
				t.Columns = append(t.Columns, col)
			}
		}
		set(p, "rowCount", &t.RowCount)
		set(p, "rowHeight", &t.RowHeight) // 0 defers to the row.height token
		set(p, "sortKey", &t.SortKey)
		if v, ok := p["sortDir"]; ok {
			s, _ := str(v)
			t.SortDir = "asc"
			if s == "desc" {
				t.SortDir = "desc"
			}
		}
		setOr(p, "selected", &t.Selected, -1)
		setOr(p, "rowKey", &t.KeyCol, -1)
		set(p, "selectedKey", &t.SelectedKey)
		if subscribed(p, wire.EvVisibleRange) {
			t.OnVisibleRange = func(start, end int) {
				ns.sink.Event(id, wire.EvVisibleRange, map[string]any{"start": start, "end": end})
			}
		}
		if subscribed(p, wire.EvSort) {
			t.OnSort = func(key string, asc bool) {
				ns.sink.Event(id, wire.EvSort, map[string]any{"key": key, "asc": asc})
			}
		}
		if subscribed(p, wire.EvRowSelect) {
			t.OnRowSelect = func(row int, key string) {
				if key != "" {
					ns.sink.Event(id, wire.EvRowSelect, map[string]any{"row": row, "key": key})
				} else {
					ns.sink.Event(id, wire.EvRowSelect, row)
				}
			}
		}
		if subscribed(p, wire.EvRowActivate) {
			t.OnRowActivate = func(row int, key string) {
				if key != "" {
					ns.sink.Event(id, wire.EvRowActivate, map[string]any{"row": row, "key": key})
				} else {
					ns.sink.Event(id, wire.EvRowActivate, row)
				}
			}
		}
		if subscribed(p, wire.EvToggle) {
			t.OnToggle = func(row int, key string) {
				ns.sink.Event(id, wire.EvToggle, map[string]any{"row": row, "key": key})
			}
		}
		if subscribed(p, wire.EvCellActivate) {
			t.OnCellActivate = func(row int, key, col, value string) {
				ns.sink.Event(id, wire.EvCellActivate,
					map[string]any{"row": row, "key": key, "col": col, "value": value})
			}
		}
		if subscribed(p, wire.EvColResize) {
			t.OnColResize = func(key string, width float32) {
				ns.sink.Event(id, wire.EvColResize, map[string]any{"key": key, "width": width})
			}
		}
	case *ui.Progress:
		set(p, "value", &t.Value)
	case *ui.Slider:
		set(p, "min", &t.Min)
		setOr(p, "max", &t.Max, 1)
		set(p, "step", &t.Step)
		// local echo, mid-drag the client's value wins
		if !t.Dragging {
			set(p, "value", &t.Value)
		}
		if subscribed(p, wire.EvInput) {
			t.OnInput = func(v float32) { ns.sink.Debounce(id, wire.EvInput, v, 100*time.Millisecond) }
		}
		if subscribed(p, wire.EvCommit) {
			t.OnCommit = func(v float32) { ns.sink.Event(id, wire.EvCommit, v) }
		}
	case *ui.RadioGroup:
		setOptions(p, &t.Options)
		setOr(p, "selected", &t.Selected, -1)
		if subscribed(p, wire.EvSelect) {
			t.OnSelect = func(i int) { ns.sink.Event(id, wire.EvSelect, i) }
		}
	case *ui.Tabs:
		setOptions(p, &t.Options)
		set(p, "selected", &t.Selected)
		if subscribed(p, wire.EvSelect) {
			t.OnSelect = func(i int) { ns.sink.Event(id, wire.EvSelect, i) }
		}
	case *ui.Select:
		setOptions(p, &t.Options)
		setOr(p, "selected", &t.Selected, -1)
		if subscribed(p, wire.EvSelect) {
			t.OnSelect = func(i int) { ns.sink.Event(id, wire.EvSelect, i) }
		}
	case *ui.Dialog:
		set(p, "title", &t.Title)
		setOr(p, "cardWidth", &t.CardW, 420)
		setOr(p, "cardHeight", &t.CardH, 200)
		if subscribed(p, wire.EvDismiss) {
			t.OnDismiss = func() { ns.sink.Event(id, wire.EvDismiss, nil) }
		}
	case *ui.SplitView:
		if v, ok := p["axis"]; ok {
			s, _ := str(v)
			t.Axis = "h"
			if s == "v" {
				t.Axis = "v"
			}
		}
		setOr(p, "pos", &t.Pos, 300)
		set(p, "pinEnd", &t.PinEnd)
		setOr(p, "minA", &t.MinA, 80)
		setOr(p, "minB", &t.MinB, 80)
		if subscribed(p, wire.EvSplitResize) {
			t.OnResize = func(pos float32) { ns.sink.Event(id, wire.EvSplitResize, pos) }
		}
	case *ui.ImageView:
		set(p, "src", &t.Src)
		if v, ok := p["fit"]; ok {
			s, _ := str(v)
			t.Fit = "contain"
			if s == "cover" || s == "fill" {
				t.Fit = s
			}
		}
		if v, ok := p["radius"]; ok {
			t.Radius, _ = radius(v)
		}
		set(p, "alt", &t.Alt)
	case *ui.ShaderPane:
		set(p, "frag", &t.Frag)
		// merge only, a set op carries just the changed keys (SetUniform) so
		// tweening one float never re-ships the whole uniform map
		if v, ok := p["uniforms"]; ok {
			if t.Uniforms == nil {
				t.Uniforms = map[string]float32{}
			}
			for k, f := range floatMap(v) {
				t.Uniforms[k] = f
			}
		}
		set(p, "animate", &t.Animate)
	case *ui.Glass:
		// the glass resolves pick targets through the store's id map
		// (deepest widget -> nearest server-known ancestor)
		t.IdOf = func(w ui.Widget) int { return ns.idOf[w] }
		if subscribed(p, wire.EvPick) {
			t.OnPick = func(x, y float32, target int) {
				ns.sink.Event(id, wire.EvPick, map[string]any{"x": x, "y": y, "target": target})
			}
		}
		if subscribed(p, wire.EvDrag) {
			t.OnDragTo = func(x, y float32) {
				ns.sink.Event(id, wire.EvDrag, map[string]any{"x": x, "y": y})
			}
		}
		if subscribed(p, wire.EvDrop) {
			t.OnDropAt = func(x, y float32) {
				ns.sink.Event(id, wire.EvDrop, map[string]any{"x": x, "y": y})
			}
		}
		if subscribed(p, wire.EvWheel) {
			t.OnWheelAt = func(x, y, dx, dy float32) {
				ns.sink.Event(id, wire.EvWheel, map[string]any{"x": x, "y": y, "dx": dx, "dy": dy})
			}
		}
		if subscribed(p, wire.EvRPick) {
			t.OnRPick = func(x, y float32) {
				ns.sink.Event(id, wire.EvRPick, map[string]any{"x": x, "y": y})
			}
		}
		if subscribed(p, wire.EvRDrag) {
			t.OnRDragTo = func(x, y float32) {
				ns.sink.Event(id, wire.EvRDrag, map[string]any{"x": x, "y": y})
			}
		}
		if subscribed(p, wire.EvRDrop) {
			t.OnRDropAt = func(x, y float32) {
				ns.sink.Event(id, wire.EvRDrop, map[string]any{"x": x, "y": y})
			}
		}
	case *ui.Grid:
		if v, ok := p["columns"].([]any); ok {
			t.Columns = t.Columns[:0]
			for _, cv := range v {
				o, _ := cv.(props)
				track := ui.GridTrack{Kind: "content"}
				if k, _ := str(o["kind"]); k == "fixed" || k == "fill" {
					track.Kind = k
				}
				track.Px, _ = num(o["px"])
				track.Weight, _ = num(o["weight"])
				track.Align, _ = str(o["align"])
				t.Columns = append(t.Columns, track)
			}
		}
		setOr(p, "colGap", &t.ColGap, 12)
		setOr(p, "rowGap", &t.RowGap, 10)
	}
}

// TextArea embeds TextField and shares its whole prop contract
func (ns *NodeStore) applyText(t *ui.TextField, id int, p props) {
	set(p, "radius", &t.Radius)
	// local echo, while the user types a focused field ignores server sets
	if !t.Focused {
		set(p, "value", &t.Value)
	}
	set(p, "placeholder", &t.Placeholder)
	setOr(p, "size", &t.FontSize, 14)
	set(p, "mono", &t.Mono)
	set(p, "font", &t.Family)
	set(p, "sensitive", &t.Sensitive)
	if subscribed(p, wire.EvInput) {
		t.OnInput = func(v string) { ns.sink.Debounce(id, wire.EvInput, v, 150*time.Millisecond) }
	}
	if subscribed(p, wire.EvCommit) {
		t.OnCommit = func(v string) { ns.sink.Event(id, wire.EvCommit, v) }
	}
	if subscribed(p, wire.EvSubmit) {
		t.OnSubmit = func(v string) { ns.sink.Event(id, wire.EvSubmit, v) }
	}
	if subscribed(p, wire.EvTab) {
		t.OnTab = func(shift bool, v string, caret int) {
			ns.sink.Event(id, wire.EvTab, map[string]any{"shift": shift, "value": v, "caret": caret})
		}
	}
}

func (ns *NodeStore) Remove(id int) {
	w, ok := ns.ByID[id]
	if !ok {
		return
	}
	var prune func(n ui.Widget)
	prune = func(n ui.Widget) {
		if nid, ok := ns.idOf[n]; ok {
			if ns.ByID[nid] == n {
				delete(ns.ByID, nid)
			}
			delete(ns.idOf, n)
		}
		for _, c := range n.Base().Kids {
			prune(c)
		}
	}
	prune(w)
	if parent := w.Base().Parent; parent != nil {
		pk := parent.Base()
		for i, c := range pk.Kids {
			if c == w {
				pk.Kids = append(pk.Kids[:i], pk.Kids[i+1:]...)
				break
			}
		}
		w.Base().Parent = nil
	}
}

// one constructor per protocol node type
var widgets = map[wire.Type]func() ui.Widget{
	wire.Panel:     func() ui.Widget { return ui.NewPanel() },
	wire.Label:     func() ui.Widget { return ui.NewLabel("", gfx.NewFont(13, gfx.FontOpts{}), nil) },
	wire.RichText:  func() ui.Widget { return ui.NewRichText(gfx.NewFont(13, gfx.FontOpts{}), nil) },
	wire.Button:    func() ui.Widget { return ui.NewButton("") },
	wire.Checkbox:  func() ui.Widget { return ui.NewCheckbox("") },
	wire.TextField: func() ui.Widget { return ui.NewTextField() },
	wire.TextArea:  func() ui.Widget { return ui.NewTextArea() },
	wire.VStack:    func() ui.Widget { return ui.NewVStack() },
	wire.HStack:    func() ui.Widget { return ui.NewHStack() },
	wire.Dock:      func() ui.Widget { return ui.NewDock() },
	wire.Scroll:    func() ui.Widget { return ui.NewScrollView() },
	wire.Grid:      func() ui.Widget { return ui.NewGrid() },
	wire.Split:     func() ui.Widget { return ui.NewSplitView() },
	wire.Table:     func() ui.Widget { return ui.NewTableView() },
	wire.Tree: func() ui.Widget {
		// tree is the table in tree mode: same columns and rows protocol,
		// plus per-row hierarchy meta and toggle round-trips
		t := ui.NewTableView()
		t.Tree = true
		return t
	},
	wire.Select:   func() ui.Widget { return ui.NewSelect() },
	wire.Progress: func() ui.Widget { return ui.NewProgress() },
	wire.Slider:   func() ui.Widget { return ui.NewSlider() },
	wire.Radio:    func() ui.Widget { return ui.NewRadioGroup() },
	wire.Tabs:     func() ui.Widget { return ui.NewTabs() },
	wire.Dialog:   func() ui.Widget { return ui.NewDialog() },
	wire.Image:    func() ui.Widget { return ui.NewImageView() },
	wire.Shader:   func() ui.Widget { return ui.NewShaderPane() },
	wire.Glass:    func() ui.Widget { return ui.NewGlass() },
}

func construct(typ wire.Type) ui.Widget {
	if ctor, ok := widgets[typ]; ok {
		return ctor()
	}
	// server newer than this terminal, paint the hole instead of dropping the
	// subtree so the rest of the tree still works
	log.Error().Msgf("caution: unknown node type %q, using panel", typ)
	return ui.NewPanel()
}

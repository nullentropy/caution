package proto

import (
	"time"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/ui"

	"github.com/rs/zerolog/log"
)

type props = map[string]any

type nodeJSON struct {
	ID   int        `json:"id"`
	Type string     `json:"type"`
	P    props      `json:"p"`
	Kids []nodeJSON `json:"kids"`
}

// EventSink receives subscribed semantic events for shipping upstream.
type EventSink interface {
	Event(id int, ev string, value any)
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
	a := &ui.Anchors{
		Left: numP(o["left"]), Right: numP(o["right"]),
		Top: numP(o["top"]), Bottom: numP(o["bottom"]),
		CenterX: numP(o["centerX"]), CenterY: numP(o["centerY"]),
	}
	return a
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
	if v, ok := p["windowDrag"]; ok {
		b.WindowDrag, _ = v.(bool)
	}
	if v, ok := p["stack"]; ok {
		ss, valid := stackSize(v)
		if !valid {
			ss = ui.StackSize{Kind: "content"}
		}
		b.StackSize = ss
	}
	if v, ok := p["span"]; ok {
		if f, ok := num(v); ok {
			b.GridSpan = int(f)
		} else {
			b.GridSpan = 1
		}
	}
	if v, ok := p["width"]; ok {
		b.Width = numP(v)
	}
	if v, ok := p["height"]; ok {
		b.Height = numP(v)
	}
	if v, ok := p["clips"]; ok {
		b.Clips, _ = boolean(v)
	}
	if v, ok := p["effect"]; ok {
		b.Effect = effectSpec(v)
	}
	if v, ok := p["outline"]; ok {
		b.Outline, _ = boolean(v)
	}
	if v, ok := p["tip"]; ok {
		b.Tip, _ = str(v)
	}
	if v, ok := p["sound"]; ok {
		b.SoundToken, _ = str(v)
	}
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
	l.Font = gfx.NewFont(size, gfx.FontOpts{Weight: weight, Italic: italic, Mono: mono})
}

// NodeStore is the registry of the widget vocabulary: construct by type,
// apply props, wire subscribed events to the sink. Maintains the id->widget
// map used by patches.
type NodeStore struct {
	ByID map[int]ui.Widget
	sink EventSink
}

func NewNodeStore(sink EventSink) *NodeStore {
	return &NodeStore{ByID: map[int]ui.Widget{}, sink: sink}
}

func (ns *NodeStore) Build(json nodeJSON) ui.Widget {
	w := construct(json.Type)
	ns.ByID[json.ID] = w
	ns.Apply(w, json.ID, json.P)
	for _, kid := range json.Kids {
		c := ns.Build(kid)
		c.Base().Parent = w
		w.Base().Kids = append(w.Base().Kids, c)
	}
	return w
}

func subscribed(p props, ev string) bool {
	arr, ok := p["on"].([]any)
	if !ok {
		return false
	}
	for _, v := range arr {
		if s, _ := str(v); s == ev {
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
	// Context menus are type-agnostic, like tips - but their pick needs the
	// node id, so they wire here rather than in applyCommon.
	if v, ok := p["context"]; ok {
		w.Base().ContextItems = contextItems(v)
	}
	if subscribed(p, "context") {
		w.Base().OnContextPick = func(cid int) { ns.sink.Event(id, "context", cid) }
	}
	// Universal one-shot commands, monotonic so they replay safely on
	// remount; the requests park until the next paint.
	if v, ok := p["focusSeq"]; ok {
		if f, _ := num(v); f != w.Base().FocusSeq {
			w.Base().FocusSeq = f
			w.Base().RequestFocus()
		}
	}
	if v, ok := p["revealSeq"]; ok {
		if f, _ := num(v); f != w.Base().RevealSeq {
			w.Base().RevealSeq = f
			w.Base().RequestReveal()
		}
	}
	switch t := w.(type) {
	case *ui.Panel:
		if v, ok := p["bg"]; ok {
			t.Bg = color(v)
		}
		if v, ok := p["borderColor"]; ok {
			t.BorderColor = color(v)
		}
		if v, ok := p["borderWidth"]; ok {
			t.BorderWidth = 1
			if f, ok := num(v); ok {
				t.BorderWidth = f
			}
		}
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
		if v, ok := p["text"]; ok {
			t.Text, _ = str(v)
		}
		applyLabelFont(t, p)
		if v, ok := p["color"]; ok {
			if c := color(v); c != nil {
				t.Color = c
			} else {
				t.Color = ui.Tok("ink")
			}
		}
		if v, ok := p["selectable"]; ok {
			t.Selectable, _ = boolean(v)
		}
		if v, ok := p["wrap"]; ok {
			t.Wrap, _ = boolean(v)
		}
	case *ui.Button:
		if v, ok := p["label"]; ok {
			t.Label, _ = str(v)
		}
		if v, ok := p["primary"]; ok {
			t.Primary, _ = boolean(v)
		}
		if v, ok := p["radius"]; ok {
			t.Radius = nil
			if f, ok := num(v); ok {
				r := float32(f)
				t.Radius = &r
			}
		}
		if subscribed(p, "click") {
			t.OnClick = func() { ns.sink.Event(id, "click", nil) }
		}
	case *ui.Checkbox:
		if v, ok := p["label"]; ok {
			t.Label, _ = str(v)
		}
		if v, ok := p["checked"]; ok {
			t.Checked, _ = boolean(v)
		}
		if subscribed(p, "toggle") {
			t.OnToggle = func(checked bool) { ns.sink.Event(id, "toggle", checked) }
		}
	case *ui.TextField:
		if v, ok := p["radius"]; ok {
			t.Radius = nil
			if f, ok := num(v); ok {
				r := float32(f)
				t.Radius = &r
			}
		}
		// Local echo: while the user is typing, the client's value is
		// authoritative - a focused field ignores server sets.
		forced := false
		if v, ok := p["overrideSeq"]; ok {
			if f, _ := num(v); f != t.OverrideSeq {
				t.OverrideSeq = f
				forced = true
			}
		}
		if v, ok := p["value"]; ok {
			if s, _ := str(v); forced {
				t.ForceValue(s)
			} else if !t.Focused {
				t.Value = s
			}
		}
		if v, ok := p["placeholder"]; ok {
			t.Placeholder, _ = str(v)
		}
		if v, ok := p["size"]; ok {
			t.FontSize = 14
			if f, ok := num(v); ok {
				t.FontSize = f
			}
		}
		if v, ok := p["mono"]; ok {
			t.Mono, _ = boolean(v)
		}
		if v, ok := p["sensitive"]; ok {
			t.Sensitive, _ = boolean(v)
		}
		// One-shot commands ride as monotonic props so they replay safely on
		// remount: resetSeq force-clears (even focused), focusSeq takes focus.
		if v, ok := p["resetSeq"]; ok {
			if f, _ := num(v); f != t.ResetSeq {
				t.ResetSeq = f
				t.ForceClear()
			}
		}
		if subscribed(p, "input") {
			t.OnInput = debounceString(150*time.Millisecond, func(v string) {
				ns.sink.Event(id, "input", v)
			})
		}
		if subscribed(p, "commit") {
			t.OnCommit = func(v string) { ns.sink.Event(id, "commit", v) }
		}
	case *ui.TextArea:
		// Same contract as TextField (it embeds the editing core), plus rows.
		forced := false
		if v, ok := p["overrideSeq"]; ok {
			if f, _ := num(v); f != t.OverrideSeq {
				t.OverrideSeq = f
				forced = true
			}
		}
		if v, ok := p["value"]; ok {
			if s, _ := str(v); forced {
				t.ForceValue(s)
			} else if !t.Focused {
				t.Value = s
			}
		}
		if v, ok := p["placeholder"]; ok {
			t.Placeholder, _ = str(v)
		}
		if v, ok := p["size"]; ok {
			t.FontSize = 14
			if f, ok := num(v); ok {
				t.FontSize = f
			}
		}
		if v, ok := p["mono"]; ok {
			t.Mono, _ = boolean(v)
		}
		if v, ok := p["rows"]; ok {
			t.Rows = 4
			if f, ok := num(v); ok {
				t.Rows = int(f)
			}
		}
		if v, ok := p["resetSeq"]; ok {
			if f, _ := num(v); f != t.ResetSeq {
				t.ResetSeq = f
				t.ForceClear()
			}
		}
		if subscribed(p, "input") {
			t.OnInput = debounceString(150*time.Millisecond, func(v string) {
				ns.sink.Event(id, "input", v)
			})
		}
		if subscribed(p, "commit") {
			t.OnCommit = func(v string) { ns.sink.Event(id, "commit", v) }
		}
	case *ui.Stack:
		if v, ok := p["padding"]; ok {
			t.Padding, _ = num(v)
		}
		if v, ok := p["spacing"]; ok {
			t.Spacing = 8
			if f, ok := num(v); ok {
				t.Spacing = f
			}
		}
		if v, ok := p["align"]; ok {
			t.Align, _ = str(v)
		}
	case *ui.Dock:
		if v, ok := p["padding"]; ok {
			t.Padding, _ = num(v)
		}
		if v, ok := p["gap"]; ok {
			t.Gap, _ = num(v)
		}
	case *ui.ScrollView:
		if v, ok := p["insetBottom"]; ok {
			t.ContentInsetBottom, _ = num(v)
		}
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
		if v, ok := p["rowCount"]; ok {
			f, _ := num(v)
			t.RowCount = int(f)
		}
		if v, ok := p["rowHeight"]; ok {
			t.RowHeight = 0 // unparsable prop: defer to the row.height token
			if f, ok := num(v); ok {
				t.RowHeight = f
			}
		}
		if v, ok := p["sortKey"]; ok {
			t.SortKey, _ = str(v)
		}
		if v, ok := p["sortDir"]; ok {
			s, _ := str(v)
			t.SortDir = "asc"
			if s == "desc" {
				t.SortDir = "desc"
			}
		}
		if v, ok := p["selected"]; ok {
			t.Selected = -1
			if f, ok := num(v); ok {
				t.Selected = int(f)
			}
		}
		if v, ok := p["rowKey"]; ok {
			t.KeyCol = -1
			if f, ok := num(v); ok {
				t.KeyCol = int(f)
			}
		}
		if v, ok := p["selectedKey"]; ok {
			t.SelectedKey, _ = str(v)
		}
		if subscribed(p, "visible-range") {
			t.OnVisibleRange = func(start, end int) {
				ns.sink.Event(id, "visible-range", map[string]any{"start": start, "end": end})
			}
		}
		if subscribed(p, "sort") {
			t.OnSort = func(key string, asc bool) {
				ns.sink.Event(id, "sort", map[string]any{"key": key, "asc": asc})
			}
		}
		if subscribed(p, "row-select") {
			t.OnRowSelect = func(row int, key string) {
				if key != "" {
					ns.sink.Event(id, "row-select", map[string]any{"row": row, "key": key})
				} else {
					ns.sink.Event(id, "row-select", row)
				}
			}
		}
		if subscribed(p, "row-activate") {
			t.OnRowActivate = func(row int, key string) {
				if key != "" {
					ns.sink.Event(id, "row-activate", map[string]any{"row": row, "key": key})
				} else {
					ns.sink.Event(id, "row-activate", row)
				}
			}
		}
		if subscribed(p, "toggle") {
			t.OnToggle = func(row int, key string) {
				ns.sink.Event(id, "toggle", map[string]any{"row": row, "key": key})
			}
		}
		if subscribed(p, "cell-activate") {
			t.OnCellActivate = func(row int, key, col, value string) {
				ns.sink.Event(id, "cell-activate",
					map[string]any{"row": row, "key": key, "col": col, "value": value})
			}
		}
		if subscribed(p, "col-resize") {
			t.OnColResize = func(key string, width float32) {
				ns.sink.Event(id, "col-resize", map[string]any{"key": key, "width": width})
			}
		}
	case *ui.Progress:
		if v, ok := p["value"]; ok {
			t.Value, _ = num(v)
		}
	case *ui.Slider:
		if v, ok := p["min"]; ok {
			t.Min, _ = num(v)
		}
		if v, ok := p["max"]; ok {
			t.Max = 1
			if f, ok := num(v); ok {
				t.Max = f
			}
		}
		if v, ok := p["step"]; ok {
			t.Step, _ = num(v)
		}
		// Local echo: mid-drag the client's value is authoritative.
		if v, ok := p["value"]; ok && !t.Dragging {
			t.Value, _ = num(v)
		}
		if subscribed(p, "input") {
			t.OnInput = debounceFloat(100*time.Millisecond, func(v float32) {
				ns.sink.Event(id, "input", v)
			})
		}
		if subscribed(p, "commit") {
			t.OnCommit = func(v float32) { ns.sink.Event(id, "commit", v) }
		}
	case *ui.RadioGroup:
		if v, ok := p["options"].([]any); ok {
			t.Options = t.Options[:0]
			for _, o := range v {
				s, _ := str(o)
				t.Options = append(t.Options, s)
			}
		}
		if v, ok := p["selected"]; ok {
			t.Selected = -1
			if f, ok := num(v); ok {
				t.Selected = int(f)
			}
		}
		if subscribed(p, "select") {
			t.OnSelect = func(i int) { ns.sink.Event(id, "select", i) }
		}
	case *ui.Tabs:
		if v, ok := p["options"].([]any); ok {
			t.Options = t.Options[:0]
			for _, o := range v {
				s, _ := str(o)
				t.Options = append(t.Options, s)
			}
		}
		if v, ok := p["selected"]; ok {
			t.Selected = 0
			if f, ok := num(v); ok {
				t.Selected = int(f)
			}
		}
		if subscribed(p, "select") {
			t.OnSelect = func(i int) { ns.sink.Event(id, "select", i) }
		}
	case *ui.Select:
		if v, ok := p["options"].([]any); ok {
			t.Options = t.Options[:0]
			for _, o := range v {
				s, _ := str(o)
				t.Options = append(t.Options, s)
			}
		}
		if v, ok := p["selected"]; ok {
			t.Selected = -1
			if f, ok := num(v); ok {
				t.Selected = int(f)
			}
		}
		if subscribed(p, "select") {
			t.OnSelect = func(i int) { ns.sink.Event(id, "select", i) }
		}
	case *ui.Dialog:
		if v, ok := p["title"]; ok {
			t.Title, _ = str(v)
		}
		if v, ok := p["cardWidth"]; ok {
			t.CardW = 420
			if f, ok := num(v); ok {
				t.CardW = f
			}
		}
		if v, ok := p["cardHeight"]; ok {
			t.CardH = 200
			if f, ok := num(v); ok {
				t.CardH = f
			}
		}
		if subscribed(p, "dismiss") {
			t.OnDismiss = func() { ns.sink.Event(id, "dismiss", nil) }
		}
	case *ui.SplitView:
		if v, ok := p["axis"]; ok {
			s, _ := str(v)
			t.Axis = "h"
			if s == "v" {
				t.Axis = "v"
			}
		}
		if v, ok := p["pos"]; ok {
			t.Pos = 300
			if f, ok := num(v); ok {
				t.Pos = f
			}
		}
		if v, ok := p["minA"]; ok {
			t.MinA = 80
			if f, ok := num(v); ok {
				t.MinA = f
			}
		}
		if v, ok := p["minB"]; ok {
			t.MinB = 80
			if f, ok := num(v); ok {
				t.MinB = f
			}
		}
		if subscribed(p, "split-resize") {
			t.OnResize = func(pos float32) { ns.sink.Event(id, "split-resize", pos) }
		}
	case *ui.ImageView:
		if v, ok := p["src"]; ok {
			t.Src, _ = str(v)
		}
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
		if v, ok := p["alt"]; ok {
			t.Alt, _ = str(v)
		}
	case *ui.ShaderPane:
		if v, ok := p["frag"]; ok {
			t.Frag, _ = str(v)
		}
		// Merge-only: a set op carries just the changed keys (SetUniform), so
		// tweening one float never re-ships a pane's whole uniform map.
		if v, ok := p["uniforms"]; ok {
			if t.Uniforms == nil {
				t.Uniforms = map[string]float32{}
			}
			for k, f := range floatMap(v) {
				t.Uniforms[k] = f
			}
		}
		if v, ok := p["animate"]; ok {
			t.Animate, _ = boolean(v)
		}
	case *ui.Glass:
		// The store owns the id map; the glass resolves pick targets
		// through it (deepest widget -> nearest server-known ancestor).
		t.IdOf = func(w ui.Widget) int {
			for wid, ww := range ns.ByID {
				if ww == w {
					return wid
				}
			}
			return 0
		}
		if subscribed(p, "pick") {
			t.OnPick = func(x, y float32, target int) {
				ns.sink.Event(id, "pick", map[string]any{"x": x, "y": y, "target": target})
			}
		}
		if subscribed(p, "drag") {
			t.OnDragTo = func(x, y float32) {
				ns.sink.Event(id, "drag", map[string]any{"x": x, "y": y})
			}
		}
		if subscribed(p, "drop") {
			t.OnDropAt = func(x, y float32) {
				ns.sink.Event(id, "drop", map[string]any{"x": x, "y": y})
			}
		}
		if subscribed(p, "wheel") {
			t.OnWheelAt = func(x, y, dx, dy float32) {
				ns.sink.Event(id, "wheel", map[string]any{"x": x, "y": y, "dx": dx, "dy": dy})
			}
		}
		if subscribed(p, "rpick") {
			t.OnRPick = func(x, y float32) {
				ns.sink.Event(id, "rpick", map[string]any{"x": x, "y": y})
			}
		}
		if subscribed(p, "rdrag") {
			t.OnRDragTo = func(x, y float32) {
				ns.sink.Event(id, "rdrag", map[string]any{"x": x, "y": y})
			}
		}
		if subscribed(p, "rdrop") {
			t.OnRDropAt = func(x, y float32) {
				ns.sink.Event(id, "rdrop", map[string]any{"x": x, "y": y})
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
		if v, ok := p["colGap"]; ok {
			t.ColGap = 12
			if f, ok := num(v); ok {
				t.ColGap = f
			}
		}
		if v, ok := p["rowGap"]; ok {
			t.RowGap = 10
			if f, ok := num(v); ok {
				t.RowGap = f
			}
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
		for nid, nw := range ns.ByID {
			if nw == n {
				delete(ns.ByID, nid)
			}
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

func construct(typ string) ui.Widget {
	switch typ {
	case "panel":
		return ui.NewPanel()
	case "label":
		return ui.NewLabel("", gfx.NewFont(13, gfx.FontOpts{}), nil)
	case "button":
		return ui.NewButton("")
	case "checkbox":
		return ui.NewCheckbox("")
	case "textfield":
		return ui.NewTextField()
	case "textarea":
		return ui.NewTextArea()
	case "vstack":
		return ui.NewVStack()
	case "hstack":
		return ui.NewHStack()
	case "dock":
		return ui.NewDock()
	case "scroll":
		return ui.NewScrollView()
	case "table":
		return ui.NewTableView()
	case "tree":
		// A tree is the table widget in tree mode: same columns, same rows
		// protocol, plus per-row hierarchy meta and toggle round-trips.
		t := ui.NewTableView()
		t.Tree = true
		return t
	case "select":
		return ui.NewSelect()
	case "progress":
		return ui.NewProgress()
	case "slider":
		return ui.NewSlider()
	case "radio":
		return ui.NewRadioGroup()
	case "tabs":
		return ui.NewTabs()
	case "dialog":
		return ui.NewDialog()
	case "split":
		return ui.NewSplitView()
	case "grid":
		return ui.NewGrid()
	case "shader":
		return ui.NewShaderPane()
	case "image":
		return ui.NewImageView()
	case "glass":
		return ui.NewGlass()
	default:
		log.Error().Msgf("caution: unknown node type %q, using panel", typ)
		return ui.NewPanel()
	}
}

// debounceString trails calls by d; the timer goroutine only fires the last
// value. Used for the input event, mirroring the browser's 150ms debounce.
func debounceString(d time.Duration, fn func(string)) func(string) {
	var timer *time.Timer
	return func(v string) {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(d, func() { fn(v) })
	}
}

func debounceFloat(d time.Duration, fn func(float32)) func(float32) {
	var timer *time.Timer
	return func(v float32) {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(d, func() { fn(v) })
	}
}

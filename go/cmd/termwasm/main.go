//go:build js && wasm

// caution termwasm: the terminal compiled to WebAssembly, built by cmd/bundle
// and never run on the host. The protocol session, the widget tree, layout,
// text, and the renderer all run here, drawing straight into the page's
// WebGL2 context. The page around it (src/terminal.ts) owns the canvas, the
// WebSocket, input events, the hidden textarea that collects typing, audio,
// and the accessibility mirror.
//
// Exports land on globalThis.__cautionTerm, all synchronous. The page
// supplies globalThis.__cautionHost before starting the module: the WebGL2
// context, the socket url, the session id to resume, and the callbacks the
// terminal drives.
package main

import (
	"encoding/json"
	"syscall/js"
	"time"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/glx"
	"github.com/nullentropy/caution/go/terminal/proto"
	"github.com/nullentropy/caution/go/terminal/render"
	"github.com/nullentropy/caution/go/terminal/text"
	"github.com/nullentropy/caution/go/terminal/ui"
	"github.com/nullentropy/caution/go/wire"
)

var (
	u    *ui.Ui
	r    *render.Renderer
	sess *proto.Session

	dpr          float32 = 1
	lastW, lastH float32
	dirty        = true // the tree changed since the last full frame
	animating    bool   // the last full frame asked for another
	lastSem      string
	semNodes     []ui.SemanticNode
)

func host() js.Value { return js.Global().Get("__cautionHost") }

func call(method string, args ...any) js.Value { return host().Call(method, args...) }

func main() {
	glx.SetContext(host().Get("gl"))
	if err := glx.Init(); err != nil {
		println("caution:", err.Error())
		return
	}
	var err error
	if r, err = render.New(); err != nil {
		println("caution: renderer:", err.Error())
		return
	}

	u = ui.New()
	u.HostText = true
	u.Measure = func(f gfx.Font, s string) *text.Run { return r.Shaper.Shape(f, dpr, s) }
	u.OnInvalidate = func() {
		dirty = true
		call("invalidate")
	}
	u.SetCursor = func(name string) { call("setCursor", name) }
	u.WriteClipboard = func(s string) { call("writeClipboard", s) }
	r.Images.OnLoad = func() { u.Invalidate() }

	sid := ""
	if v := host().Get("sid"); v.Type() == js.TypeString {
		sid = v.String()
	}
	sess = proto.NewSession(u, proto.Config{
		URL:  host().Get("url").String(),
		Dial: dial,
		SID:  sid,
		Wake: func() { call("invalidate") },
		ViewSize: func() (int, int) {
			if lastW == 0 {
				v := call("viewSize")
				return v.Index(0).Int(), v.Index(1).Int()
			}
			return int(lastW), int(lastH)
		},
		SetMenu: func(menus []proto.MenuSpec) {
			u.SetMenubar(proto.MenubarSpec(menus), func(id int) { sess.Event(0, wire.EvMenu, id) })
		},
		SetTitle:     func(title string) { call("setTitle", title) },
		Preload:      r.Images.Preload,
		PreloadSound: func(src string) { call("preloadSound", src) },
		Play:         func(src string, loop bool) { call("play", src, loop) },
		Stop:         func(src string) { call("stop", src) },
		OnMount:      func(sid string) { call("mounted", sid) },
	})

	js.Global().Set("__cautionTerm", map[string]any{
		"paint":        js.FuncOf(paint),
		"tickIn":       js.FuncOf(tickIn),
		"tick":         js.FuncOf(tick),
		"pointerDown":  js.FuncOf(pointerDown),
		"pointerMove":  js.FuncOf(pointerMove),
		"pointerUp":    js.FuncOf(pointerUp),
		"rightDown":    js.FuncOf(rightDown),
		"rightUp":      js.FuncOf(rightUp),
		"contextClick": js.FuncOf(contextClick),
		"aux":          js.FuncOf(aux),
		"wheel":        js.FuncOf(wheel),
		"keyDown":      js.FuncOf(keyDown),
		"textSync":     js.FuncOf(textSync),
		"textState":    js.FuncOf(textState),
		"blur":         js.FuncOf(blur),
		"semantics":    js.FuncOf(semantics),
		"activate":     js.FuncOf(activate),
		"fullscreen":   js.FuncOf(fullscreen),
	})
	select {}
}

// Frame kinds paint reports, for the page's diagnostics counters.
const (
	frameSkipped = iota
	frameFull
	frameDamage
	framePartial
)

// paint services one animation frame the page scheduled: logical size, device
// size, and the clock in seconds. The page owns scheduling; the choice of a
// full rebuild against replaying the retained list's animation damage is the
// same one run.go's loop makes. Returns [animating, kind].
func paint(_ js.Value, a []js.Value) any {
	w, h := float32(a[0].Float()), float32(a[1].Float())
	devW, devH := int32(a[2].Int()), int32(a[3].Int())
	now := float32(a[4].Float())
	if w == 0 || h == 0 {
		return []any{0, frameSkipped}
	}
	if d := float32(devW) / w; d != dpr {
		dpr = d
		u.NoteDPR(d)
	}
	if w != lastW || h != lastH {
		if lastW != 0 {
			sess.NoteResize(int(w), int(h))
		}
		lastW, lastH = w, h
	}
	sess.Pump()
	fr := render.Frame{ViewW: w, ViewH: h, DevW: devW, DevH: devH, Time: now}
	kind := frameSkipped
	full := func() {
		dl := &gfx.DisplayList{}
		u.BuildFrame(dl, w, h)
		animating = dl.WantsAnimation()
		before := r.Frames.Damage
		r.Render(dl, *ui.Tok("bg"), fr)
		kind = frameFull
		if r.Frames.Damage != before {
			kind = frameDamage
		}
	}
	if dirty {
		dirty = false
		full()
	} else if animating && !r.AnimationIsInvisible() {
		if r.RenderPartial(*ui.Tok("bg"), fr) {
			kind = framePartial
		} else {
			full()
		}
	}
	on := 0
	if animating {
		on = 1
	}
	return []any{on, kind}
}

func tickIn(js.Value, []js.Value) any {
	return float64(u.TickIn() / time.Millisecond)
}

func tick(js.Value, []js.Value) any {
	u.NoteTick()
	dirty = true
	return nil
}

func xy(a []js.Value) (float32, float32) { return float32(a[0].Float()), float32(a[1].Float()) }

func pointerDown(_ js.Value, a []js.Value) any { u.PointerDown(xy(a)); return nil }
func pointerMove(_ js.Value, a []js.Value) any { u.PointerMove(xy(a)); return nil }
func pointerUp(_ js.Value, a []js.Value) any   { u.PointerUp(xy(a)); return nil }
func rightUp(_ js.Value, a []js.Value) any     { u.RightUp(xy(a)); return nil }
func contextClick(_ js.Value, a []js.Value) any {
	u.ContextClick(xy(a))
	return nil
}

func rightDown(_ js.Value, a []js.Value) any {
	x, y := xy(a)
	return u.RightDown(x, y)
}

func aux(_ js.Value, a []js.Value) any {
	u.AuxDown(a[0].Int())
	return nil
}

func wheel(_ js.Value, a []js.Value) any {
	u.Wheel(float32(a[0].Float()), float32(a[1].Float()), float32(a[2].Float()), float32(a[3].Float()))
	return nil
}

func keyDown(_ js.Value, a []js.Value) any {
	return u.KeyDown(ui.Key{
		Name: a[0].String(), Meta: a[1].Bool(), Ctrl: a[2].Bool(), Shift: a[3].Bool(), Alt: a[4].Bool(),
	})
}

func textSync(_ js.Value, a []js.Value) any {
	u.SyncText(a[0].String(), a[1].Int(), a[2].Int(), a[3].Bool())
	return nil
}

func textState(js.Value, []js.Value) any {
	st, ok := u.FocusedText()
	if !ok {
		return nil
	}
	return []any{
		st.Value, st.SelStart, st.SelEnd, st.Multiline, st.Placeholder,
		st.Bounds.X, st.Bounds.Y, st.Bounds.W, st.Bounds.H,
	}
}

func blur(js.Value, []js.Value) any {
	u.FocusWidget(nil)
	return nil
}

func fullscreen(_ js.Value, a []js.Value) any {
	sess.NoteFullscreen(a[0].Bool())
	return nil
}

type semJSON struct {
	ID       int          `json:"id"`
	Role     string       `json:"role"`
	Label    string       `json:"label"`
	Checked  bool         `json:"checked,omitempty"`
	Value    string       `json:"value,omitempty"`
	RowCount int          `json:"rowCount,omitempty"`
	Focused  bool         `json:"focused,omitempty"`
	Bounds   [4]float32   `json:"bounds"`
	Rows     []semRowJSON `json:"rows,omitempty"`
}

type semRowJSON struct {
	Index    int        `json:"index"`
	Label    string     `json:"label"`
	Selected bool       `json:"selected"`
	Bounds   [4]float32 `json:"bounds"`
}

func rectJSON(r gfx.Rect) [4]float32 { return [4]float32{r.X, r.Y, r.W, r.H} }

// semantics returns the flattened tree as JSON, or "" when nothing changed
// since the last call.
func semantics(js.Value, []js.Value) any {
	semNodes = u.Semantics()
	out := make([]semJSON, 0, len(semNodes))
	for _, n := range semNodes {
		j := semJSON{
			ID: n.ID, Role: n.Role, Label: n.Label, Checked: n.Checked, Value: n.Value,
			RowCount: n.RowCount, Focused: n.Focused, Bounds: rectJSON(n.Bounds),
		}
		for _, r := range n.Rows {
			j.Rows = append(j.Rows, semRowJSON{Index: r.Index, Label: r.Label, Selected: r.Selected, Bounds: rectJSON(r.Bounds)})
		}
		out = append(out, j)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	if s := string(b); s != lastSem {
		lastSem = s
		return s
	}
	return ""
}

func activate(_ js.Value, a []js.Value) any {
	id, row := a[0].Int(), a[1].Int()
	for _, n := range semNodes {
		if n.ID == id {
			u.Activate(n.Widget, row)
			break
		}
	}
	return nil
}

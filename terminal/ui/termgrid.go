package ui

import (
	"math"
	"time"

	"github.com/nullentropy/caution/terminal/gfx"
)

type TermRun struct {
	X                           int
	Text                        string
	Fg, Bg                      *gfx.Color
	Bold, Italic, Under, Strike bool
}

type TermCursor struct {
	X, Y    int
	Visible bool
	Blink   bool
	Shape   string
	Ch      string
	Fg, Bg  *gfx.Color
}

type TermGrid struct {
	Core
	FontSize float32
	Family   string
	Pad      float32
	Fg, Bg   gfx.Color
	Lines    map[int][]TermRun
	Caret    TermCursor

	OnInput    func(v map[string]any)
	OnMouse    func(v map[string]any)
	OnGridSize func(cols, rows int)

	focused       bool
	cols, rows    int
	swallowUntil  time.Time
	lastActivity  time.Time
	hoverX, hover float32
	wheelAcc      float32
	downCell      [2]int
	held          int
}

func NewTermGrid() *TermGrid {
	t := &TermGrid{FontSize: 14, Fg: gfx.Hex("#d0d0d0"), Bg: gfx.Hex("#1c1c1c"), Lines: map[int][]TermRun{}}
	t.self = t
	return t
}

func (t *TermGrid) font(bold, italic bool) gfx.Font {
	w := 400
	if bold {
		w = 700
	}
	return gfx.NewFont(t.FontSize, gfx.FontOpts{Mono: true, Weight: w, Italic: italic, Family: t.Family})
}

func (t *TermGrid) cell() (w, h float32) {
	if t.UI == nil || t.UI.Measure == nil {
		return t.FontSize * 0.6, t.FontSize * 1.2
	}
	r := t.UI.Measure(t.font(false, false), "MMMMMMMMMM")
	return r.Width / 10, ceil32(r.Ascent + r.Descent)
}

func (t *TermGrid) LayoutChildren() {
	t.Core.LayoutChildren()
	cw, ch := t.cell()
	if cw <= 0 || ch <= 0 {
		return
	}
	cols := int((t.Bounds.W - 2*t.Pad) / cw)
	rows := int((t.Bounds.H - 2*t.Pad) / ch)
	if cols < 1 || rows < 1 || (cols == t.cols && rows == t.rows) {
		return
	}
	t.cols, t.rows = cols, rows
	if t.OnGridSize != nil {
		t.OnGridSize(cols, rows)
	}
}

func (t *TermGrid) PaintSelf(dl *gfx.DisplayList) {
	dl.Fill(t.Bounds, t.Bg, gfx.Corners{})
	cw, ch := t.cell()
	ox, oy := t.Bounds.X+t.Pad, t.Bounds.Y+t.Pad
	colX := func(c int) float32 { return float32(math.Round(float64(ox + float32(c)*cw))) }
	dl.PushClip(t.Bounds)
	defer dl.PopClip()
	for y := 0; y < t.rows; y++ {
		runs := t.Lines[y]
		top := oy + float32(y)*ch
		for _, r := range runs {
			if r.Bg == nil {
				continue
			}
			n := cellCount(r.Text)
			dl.Fill(gfx.R(colX(r.X), top, colX(r.X+n)-colX(r.X), ch), *r.Bg, gfx.Corners{})
		}
		for _, r := range runs {
			fg := t.Fg
			if r.Fg != nil {
				fg = *r.Fg
			}
			x := colX(r.X)
			if r.Text != "" && !isBlank(r.Text) {
				dl.Text(r.Text, x, top, t.font(r.Bold, r.Italic), fg)
			}
			n := float32(cellCount(r.Text))
			if r.Under {
				dl.Fill(gfx.R(x, top+ch-2, n*cw, 1), fg, gfx.Corners{})
			}
			if r.Strike {
				dl.Fill(gfx.R(x, top+ch/2, n*cw, 1), fg, gfx.Corners{})
			}
		}
	}
	t.paintCursor(dl, colX, oy, cw, ch)
}

func (t *TermGrid) paintCursor(dl *gfx.DisplayList, colX func(int) float32, oy, cw, ch float32) {
	c := t.Caret
	if !c.Visible || c.Y >= t.rows || c.X >= t.cols || !t.caretOn() {
		return
	}
	x, y := colX(c.X), oy+float32(c.Y)*ch
	w := colX(c.X+1) - x
	ink := t.Fg
	if c.Fg != nil {
		ink = *c.Fg
	}
	paper := t.Bg
	if c.Bg != nil {
		paper = *c.Bg
	}
	box := gfx.R(x, y, w, ch)
	switch {
	case !t.focused || t.UI.windowInactive:
		dl.Rect(box, gfx.Transparent, gfx.RectOpts{BorderWidth: 1, BorderColor: &ink})
	case c.Shape == "bar":
		dl.Fill(gfx.R(x, y, 2, ch), ink, gfx.Corners{})
	case c.Shape == "underline":
		dl.Fill(gfx.R(x, y+ch-2, w, 2), ink, gfx.Corners{})
	default:
		dl.Fill(box, ink, gfx.Corners{})
		if c.Ch != "" && !isBlank(c.Ch) {
			dl.Text(c.Ch, x, y, t.font(false, false), paper)
		}
	}
}

func cellCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func isBlank(s string) bool {
	for _, r := range s {
		if r != ' ' {
			return false
		}
	}
	return true
}

func (t *TermGrid) cellAt(x, y float32) (int, int) {
	cw, ch := t.cell()
	c := int((x - t.Bounds.X - t.Pad) / cw)
	r := int((y - t.Bounds.Y - t.Pad) / ch)
	return clampInt(c, 0, max(0, t.cols-1)), clampInt(r, 0, max(0, t.rows-1))
}

func (t *TermGrid) Interactive() bool { return true }
func (t *TermGrid) Focusable() bool   { return true }
func (t *TermGrid) RawKeys() bool     { return true }

func (t *TermGrid) OnFocusChange(f bool) {
	t.focused = f
	t.Touch()
	t.Invalidate()
}

func (t *TermGrid) OnKey(k Key) bool {
	t.Touch()
	if t.OnInput == nil {
		return false
	}
	if k.Meta {
		if k.Name == "v" && t.UI.ReadClipboard != nil {
			if s := t.UI.ReadClipboard(); s != "" {
				t.OnInput(map[string]any{"t": s, "paste": true})
			}
			return true
		}

		return false
	}

	if k.Name == " " && !k.Ctrl && !k.Alt {
		return false
	}
	if k.Ctrl || k.Alt {

		t.swallowUntil = time.Now().Add(60 * time.Millisecond)
	}
	t.OnInput(map[string]any{"k": k.Name, "ctrl": k.Ctrl, "alt": k.Alt, "shift": k.Shift})
	return true
}

func (t *TermGrid) OnChar(r rune) bool {
	t.Touch()
	if time.Now().Before(t.swallowUntil) {
		t.swallowUntil = time.Time{}
		return true
	}
	if t.OnInput != nil {
		t.OnInput(map[string]any{"t": string(r)})
	}
	return true
}

func (t *TermGrid) mouse(kind string, x, y float32, extra map[string]any) {
	if t.OnMouse == nil {
		return
	}
	c, r := t.cellAt(x, y)
	v := map[string]any{"kind": kind, "x": c, "y": r, "button": 1}
	for k, val := range extra {
		v[k] = val
	}
	t.OnMouse(v)
}

func (t *TermGrid) OnPointerDown(x, y float32, count int) {
	c, r := t.cellAt(x, y)
	t.downCell = [2]int{c, r}
	t.held = 1
	t.mouse("press", x, y, map[string]any{"count": count})
}

func (t *TermGrid) OnButton(button int, down bool, x, y float32, k Key) bool {
	if t.OnMouse == nil {
		return false
	}
	extra := map[string]any{"button": button, "ctrl": k.Ctrl, "shift": k.Shift || k.Alt}
	if !down {
		t.held = 0
		t.mouse("release", x, y, extra)
		return true
	}
	c, r := t.cellAt(x, y)
	t.downCell = [2]int{c, r}
	t.held = button
	t.mouse("press", x, y, extra)
	return true
}

func (t *TermGrid) OnPointerDrag(x, y float32) {
	c, r := t.cellAt(x, y)
	if [2]int{c, r} == t.downCell {
		return
	}
	t.downCell = [2]int{c, r}
	t.mouse("drag", x, y, map[string]any{"button": max(t.held, 1)})
}

func (t *TermGrid) OnPointerUp(x, y float32) {
	t.held = 0
	t.mouse("release", x, y, nil)
}

func (t *TermGrid) OnPointerHover(x, y float32) { t.hoverX, t.hover = x, y }

func (t *TermGrid) Scrollable() bool { return t.OnMouse != nil }

func (t *TermGrid) ScrollBy(dy float32) {
	_, ch := t.cell()
	t.wheelAcc += dy
	notches := int(t.wheelAcc / ch)
	if notches == 0 {
		return
	}
	t.wheelAcc -= float32(notches) * ch
	t.mouse("wheel", t.hoverX, t.hover, map[string]any{"dy": notches})
}

func (t *TermGrid) Touch() { t.lastActivity = time.Now() }

func (t *TermGrid) blinks() bool {
	return t.Caret.Blink && t.Caret.Visible && t.focused && t.UI != nil && !t.UI.windowInactive
}

func (t *TermGrid) caretOn() bool {
	if !t.blinks() {
		return true
	}
	since := time.Since(t.lastActivity)
	return since >= tfBlinkFor || (since/tfBlink)%2 == 0
}

func (t *TermGrid) blinkTick() time.Duration {
	if !t.blinks() {
		return 0
	}
	since := time.Since(t.lastActivity)
	if since >= tfBlinkFor {
		return 0
	}
	return min(tfBlink-since%tfBlink, tfBlinkFor-since)
}

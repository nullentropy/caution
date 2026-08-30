package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/text"
)

const taPadY = 8

// TextArea is the multi-line editor: TextField's editing core (rune-boundary
// selection, word ops, clipboard, blink) under wrapped-line geometry. Enter
// inserts a newline - commit fires on blur (or Cmd+Enter). Up/Down move by
// visual lines with a goal column; the wheel scrolls it like any Scroller.
type TextArea struct {
	TextField
	// Rows is the visible line count for the intrinsic height (`rows` prop).
	Rows int

	scrollY float32
	goalX   *float32
	cache   *taCache
}

type taCache struct {
	value   string
	fontKey string
	w       float32
	lines   []text.Line
	// rstart/rend are the rune-offset twins of lines' byte offsets - the
	// selection is rune-based (it shares TextField's editing core).
	rstart, rend []int
}

func NewTextArea() *TextArea {
	a := &TextArea{TextField: TextField{FontSize: 14}, Rows: 4}
	a.self = a
	return a
}

func (a *TextArea) metrics() (lineH, advance float32) {
	m := a.UI.Measure(a.font(), "Mg")
	lineH = m.Ascent + m.Descent
	return lineH, lineH + float32(int(lineH*0.25+0.5))
}

func (a *TextArea) IntrinsicSize() Size {
	lineH, adv := a.metrics()
	rows := max(1, a.Rows)
	return Size{280, ceil32(taPadY*2 + lineH + float32(rows-1)*adv)}
}

func (a *TextArea) ForceClear() {
	a.TextField.ForceClear()
	a.scrollY = 0
	a.goalX = nil
}

// -- wrapped geometry ---------------------------------------------------------

func (a *TextArea) lines() *taCache {
	w := max(1, a.Bounds.W-tfPadX()*2)
	f := a.font()
	if c := a.cache; c != nil && c.value == a.Value && c.fontKey == f.Key && abs32(c.w-w) < 0.5 {
		return c
	}
	ls := text.Wrap(func(s string) float32 { return a.UI.Measure(f, s).Width }, a.Value, w)
	c := &taCache{value: a.Value, fontKey: f.Key, w: w, lines: ls,
		rstart: make([]int, len(ls)), rend: make([]int, len(ls))}
	// One ordered walk converts byte offsets to rune offsets.
	bo, ro := 0, 0
	advanceTo := func(target int) int {
		for bo < target {
			_, sz := utf8.DecodeRuneInString(a.Value[bo:])
			bo += sz
			ro++
		}
		return ro
	}
	for i, ln := range ls {
		c.rstart[i] = advanceTo(ln.Start)
		c.rend[i] = advanceTo(ln.End)
	}
	a.cache = c
	return c
}

// lineOf is the line whose span contains rune boundary r (gaps - the eaten
// separators - belong to the line before them).
func (a *TextArea) lineOf(c *taCache, r int) int {
	li := 0
	for i := range c.rstart {
		if c.rstart[i] <= r {
			li = i
		} else {
			break
		}
	}
	return li
}

func (a *TextArea) lineRun(c *taCache, i int) ([]float32, string) {
	lineText := a.Value[c.lines[i].Start:c.lines[i].End]
	run := a.UI.Measure(a.font(), lineText)
	return runeBoundaryXs(run, c.rend[i]-c.rstart[i]), lineText
}

// caretPos maps a rune boundary to (line, x).
func (a *TextArea) caretPos(c *taCache, r int) (li int, x float32) {
	if len(c.lines) == 0 {
		return 0, 0
	}
	li = a.lineOf(c, r)
	xs, _ := a.lineRun(c, li)
	local := clampInt(r-c.rstart[li], 0, len(xs)-1)
	return li, xs[local]
}

// runeAtPoint maps a point to the nearest rune boundary.
func (a *TextArea) runeAtPoint(x, y float32) int {
	c := a.lines()
	if len(c.lines) == 0 {
		return 0
	}
	_, adv := a.metrics()
	localY := y - (a.Bounds.Y + taPadY) + a.scrollY
	li := clampInt(int(localY/adv), 0, len(c.lines)-1)
	xs, _ := a.lineRun(c, li)
	lx := x - (a.Bounds.X + tfPadX())
	best, bestD := 0, float32(1e30)
	for i, bx := range xs {
		if d := abs32(lx - bx); d < bestD {
			bestD, best = d, i
		}
	}
	return c.rstart[li] + best
}

// -- line-aware editing -----------------------------------------------------------

func (a *TextArea) lineStart(r int) int {
	c := a.lines()
	if len(c.lines) == 0 {
		return 0
	}
	return c.rstart[a.lineOf(c, r)]
}

func (a *TextArea) lineEnd(r int) int {
	c := a.lines()
	if len(c.lines) == 0 {
		return 0
	}
	return c.rend[a.lineOf(c, r)]
}

func (a *TextArea) verticalMove(dir int, extend bool) {
	c := a.lines()
	li, x := a.caretPos(c, a.selEnd)
	if a.goalX != nil {
		x = *a.goalX
	}
	gx := x
	a.goalX = &gx
	var to int
	target := li + dir
	switch {
	case target < 0:
		to = 0
	case target >= len(c.lines):
		to = len([]rune(a.Value))
	default:
		xs, _ := a.lineRun(c, target)
		best, bestD := 0, float32(1e30)
		for i, bx := range xs {
			if d := abs32(x - bx); d < bestD {
				bestD, best = d, i
			}
		}
		to = c.rstart[target] + best
	}
	a.moveCaret(to, extend)
}

func (a *TextArea) OnKey(k Key) bool {
	n := len([]rune(a.Value))
	s, e := a.selRange()
	cmdish := k.Meta || k.Ctrl
	vertical := k.Name == "Up" || k.Name == "Down"
	if !vertical {
		a.goalX = nil
	}
	switch k.Name {
	case "Enter":
		if cmdish {
			a.Commit() // Cmd+Enter commits without leaving the field
			return true
		}
		a.edit(s, e, "\n")
		return true
	case "Backspace":
		switch {
		case s != e:
			a.edit(s, e, "")
		case k.Meta:
			a.edit(a.lineStart(a.selEnd), a.selEnd, "") // Cmd+Backspace deletes to visual line start
		case k.Alt:
			a.edit(a.wordLeft(a.selEnd), a.selEnd, "")
		case a.selEnd > 0:
			a.edit(a.selEnd-1, a.selEnd, "")
		}
		return true
	case "Delete":
		if s != e {
			a.edit(s, e, "")
		} else if a.selEnd < n {
			a.edit(a.selEnd, a.selEnd+1, "")
		}
		return true
	case "Left":
		to := a.selEnd - 1
		switch {
		case k.Meta:
			to = a.lineStart(a.selEnd) // Cmd+Left is visual line start
		case k.Alt:
			to = a.wordLeft(a.selEnd)
		case s != e && !k.Shift:
			to = s
		}
		a.moveCaret(to, k.Shift)
		return true
	case "Right":
		to := a.selEnd + 1
		switch {
		case k.Meta:
			to = a.lineEnd(a.selEnd) // Cmd+Right is visual line end
		case k.Alt:
			to = a.wordRight(a.selEnd)
		case s != e && !k.Shift:
			to = e
		}
		a.moveCaret(to, k.Shift)
		return true
	case "Up":
		if k.Meta {
			a.moveCaret(0, k.Shift)
		} else {
			a.verticalMove(-1, k.Shift)
		}
		return true
	case "Down":
		if k.Meta {
			a.moveCaret(n, k.Shift)
		} else {
			a.verticalMove(1, k.Shift)
		}
		return true
	case "Home":
		a.moveCaret(a.lineStart(a.selEnd), k.Shift)
		return true
	case "End":
		a.moveCaret(a.lineEnd(a.selEnd), k.Shift)
		return true
	case "a":
		if cmdish {
			a.SelectAll()
			return true
		}
	case "c":
		if cmdish && s != e {
			a.writeClipboard(s, e)
			return true
		}
	case "x":
		if cmdish {
			if s != e {
				a.writeClipboard(s, e)
				a.edit(s, e, "")
			}
			return true
		}
	case "v":
		if cmdish {
			a.pasteMultiline()
			return true
		}
	case "z":
		if cmdish {
			if k.Shift {
				a.redoEdit()
			} else {
				a.undoEdit()
			}
			return true
		}
	case "Escape":
		return a.revert() // clean field: let Escape bubble
	}
	return false
}

// pasteMultiline keeps newlines (unlike the single-line field's paste).
func (a *TextArea) pasteMultiline() {
	if a.UI.ReadClipboard == nil {
		return
	}
	clip := strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, a.UI.ReadClipboard())
	if clip == "" {
		return
	}
	s, e := a.selRange()
	a.edit(s, e, clip)
}

// -- scrolling (wheel routes here via ScrollTarget) ---------------------------------

func (a *TextArea) contentH() float32 {
	lineH, adv := a.metrics()
	n := max(1, len(a.lines().lines))
	return lineH + float32(n-1)*adv
}

func (a *TextArea) Scrollable() bool {
	return a.UI != nil && a.contentH() > a.Bounds.H-taPadY*2
}

func (a *TextArea) ScrollBy(dy float32) {
	maxS := max(0, a.contentH()-(a.Bounds.H-taPadY*2))
	next := min(max(a.scrollY+dy, 0), maxS)
	if next != a.scrollY {
		a.scrollY = next
		a.Invalidate()
	}
}

// -- painting -----------------------------------------------------------------------

func (a *TextArea) PaintSelf(dl *gfx.DisplayList) {
	b := a.Bounds
	if a.UI.ShowFocusRing(a) {
		PaintFocusRing(dl, b, Metrics.RadiusControl)
	}
	border := Theme["controlEdge"]
	bw := Metrics.BorderWidth
	if a.Focused {
		border = Theme["accent"]
		bw = Metrics.BorderWidth * 1.5
	}
	dl.Rect(b, *Theme["panelInset"], gfx.RectOpts{
		Radius: gfx.CornerRadius(Metrics.RadiusControl), BorderWidth: bw, BorderColor: border,
	})

	lineH, adv := a.metrics()
	c := a.lines()
	innerH := b.H - taPadY*2

	// Caret-follow: keep the caret's line inside the viewport.
	if a.Focused && len(c.lines) > 0 {
		li, _ := a.caretPos(c, a.selEnd)
		cy := float32(li) * adv
		if cy-a.scrollY < 0 {
			a.scrollY = cy
		}
		if cy+lineH-a.scrollY > innerH {
			a.scrollY = cy + lineH - innerH
		}
	}
	a.scrollY = min(max(a.scrollY, 0), max(0, a.contentH()-innerH))

	textX := b.X + tfPadX()
	topY := b.Y + taPadY - a.scrollY

	dl.PushClip(gfx.Inset(b, 1.5))
	if a.Value == "" && a.Placeholder != "" {
		dl.Text(a.Placeholder, textX, b.Y+taPadY, a.font(), *Theme["inkFaint"])
	}
	s, e := a.selRange()
	for i, ln := range c.lines {
		y := topY + float32(i)*adv
		if y+lineH < b.Y || y > b.Y+b.H {
			continue // off-viewport line
		}
		lineText := a.Value[ln.Start:ln.End]
		if a.Focused && s != e {
			la := max(s, c.rstart[i])
			lb := min(e, c.rend[i])
			empty := c.rstart[i] == c.rend[i]
			if lb > la || (empty && s <= c.rstart[i] && e > c.rend[i]) {
				xs, _ := a.lineRun(c, i)
				x0 := xs[clampInt(la-c.rstart[i], 0, len(xs)-1)]
				x1 := x0 + 4 // stub marks empty lines inside the range
				if lb > la {
					x1 = xs[clampInt(lb-c.rstart[i], 0, len(xs)-1)]
				}
				dl.Fill(gfx.R(textX+x0-1, y-1, x1-x0+2, lineH+2),
					gfx.WithAlpha(*Theme["accent"], 0.35), gfx.CornerRadius(2))
			}
		}
		dl.Text(lineText, textX, y, a.font(), *Theme["ink"])
	}
	if a.Focused && s == e && a.blinkOn() {
		li, x := a.caretPos(c, a.selEnd)
		dl.Fill(gfx.R(textX+x, topY+float32(li)*adv-1, 1.5, lineH+2),
			*Theme["accent"], gfx.Corners{})
	}
	dl.PopClip()
}

// -- mouse ---------------------------------------------------------------------------

func (a *TextArea) OnPointerDown(x, y float32, detail int) {
	a.goalX = nil
	i := a.runeAtPoint(x, y)
	switch {
	case detail >= 3:
		// Triple-click selects the hard paragraph around the point.
		rs := []rune(a.Value)
		s := clampInt(i, 0, len(rs))
		for s > 0 && rs[s-1] != '\n' {
			s--
		}
		e := clampInt(i, 0, len(rs))
		for e < len(rs) && rs[e] != '\n' {
			e++
		}
		a.selStart, a.selEnd, a.anchor = s, e, s
		a.touch()
		a.Invalidate()
	case detail == 2:
		s, e := a.wordRangeAt(min(i, max(0, len([]rune(a.Value))-1)))
		a.selStart, a.selEnd, a.anchor = s, e, s
		a.touch()
		a.Invalidate()
	default:
		a.selStart, a.selEnd, a.anchor = i, i, i
		a.touch()
		a.Invalidate()
	}
}

func (a *TextArea) OnPointerDrag(x, y float32) {
	a.goalX = nil
	i := a.runeAtPoint(x, y)
	if a.selStart != a.anchor || a.selEnd != i {
		a.selStart, a.selEnd = a.anchor, i
		a.touch()
		a.Invalidate()
	}
}

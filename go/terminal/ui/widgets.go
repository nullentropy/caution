package ui

import (
	"math"
	"unicode"
	"unicode/utf8"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/text"
)

type Panel struct {
	Core
	Bg          *gfx.Color
	BorderColor *gfx.Color
	BorderWidth float32
	Radius      gfx.Corners
	DropShadow  *Shadow
}

type Shadow struct {
	Blur   float32
	Color  gfx.Color
	Dx, Dy float32
}

func NewPanel() *Panel { p := &Panel{BorderWidth: 1}; p.self = p; return p }

func (p *Panel) PaintSelf(dl *gfx.DisplayList) {
	if s := p.DropShadow; s != nil {
		dl.Shadow(p.Bounds, s.Color, s.Blur, p.Radius, s.Dx, s.Dy)
	}
	if p.Bg != nil || p.BorderColor != nil {
		fill := gfx.Transparent
		if p.Bg != nil {
			fill = *p.Bg
		}
		var bw float32
		if p.BorderColor != nil {
			bw = p.BorderWidth
		}
		dl.Rect(p.Bounds, fill, gfx.RectOpts{Radius: p.Radius, BorderWidth: bw, BorderColor: p.BorderColor})
	}
}

// -- label --------------------------------------------------------------------------

type Label struct {
	Core
	Text  string
	Font  gfx.Font
	Color *gfx.Color
	// Selectable enables drag/double-click selection and Cmd+C copy.
	Selectable bool
	// Wrap breaks across lines at the laid-out width instead of truncating;
	// the intrinsic height follows the line count (HeightForWidth). A wrapped
	// label's selection boundaries are byte offsets into the source text
	// (cross-line drags work; copies are exact), where a single-line label's
	// are glyph indices.
	Wrap bool

	lastFit   *labelFit
	lastLines *labelLines
	lastSize  *labelSize
}

// labelSize memoizes the natural size. Layout asks for it on every widget on
// every frame, and each ask costs a shaping-cache lookup.
type labelSize struct {
	text    string
	fontKey string
	epoch   int
	size    Size
}

type labelFit struct {
	text    string
	fontKey string
	epoch   int
	w       float32
	display string
}

type labelLines struct {
	text    string
	fontKey string
	epoch   int
	w       float32
	lines   []text.Line
}

func NewLabel(textStr string, f gfx.Font, color *gfx.Color) *Label {
	if color == nil {
		color = Theme["ink"]
	}
	l := &Label{Text: textStr, Font: f, Color: color}
	l.self = l
	return l
}

// displayText is what actually paints: the full text when it fits the
// laid-out bounds, else the longest prefix + '...' that does (NSTextField-style
// tail truncation). Labels sized by their intrinsic width never truncate;
// ones stretched narrower by anchors/stacks/grids clamp instead of
// overflowing.
func (l *Label) displayText() string {
	avail := l.Bounds.W
	if avail <= 0 {
		return l.Text
	}
	if l.UI.Measure(l.Font, l.Text).Width <= avail {
		return l.Text
	}
	if f := l.lastFit; f != nil && f.text == l.Text && f.fontKey == l.Font.Key &&
		f.epoch == l.UI.MeasureEpoch() && abs32(f.w-avail) < 0.5 {
		return f.display
	}
	cps := []rune(l.Text)
	lo, hi := 0, len(cps)
	for lo < hi {
		mid := (lo + hi + 1) >> 1
		if l.UI.Measure(l.Font, string(cps[:mid])+"…").Width <= avail {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	display := string(cps[:lo]) + "…"
	l.lastFit = &labelFit{text: l.Text, fontKey: l.Font.Key, epoch: l.UI.MeasureEpoch(), w: avail, display: display}
	return display
}

// run is the displayed run, so selection and painting agree on what is visible.
func (l *Label) run() *text.Run {
	return l.UI.Measure(l.Font, l.displayText())
}

func (l *Label) IntrinsicSize() Size {
	if c := l.lastSize; c != nil && c.text == l.Text && c.fontKey == l.Font.Key &&
		c.epoch == l.UI.MeasureEpoch() {
		return c.size
	}
	r := l.UI.Measure(l.Font, l.Text) // natural size = full text
	sz := Size{ceil32(r.Width), ceil32(r.Ascent + r.Descent)}
	l.lastSize = &labelSize{text: l.Text, fontKey: l.Font.Key, epoch: l.UI.MeasureEpoch(), size: sz}
	return sz
}

// -- wrapped layout -------------------------------------------------------------

func (l *Label) wrappedLines(w float32) []text.Line {
	if c := l.lastLines; c != nil && c.text == l.Text && c.fontKey == l.Font.Key &&
		c.epoch == l.UI.MeasureEpoch() && abs32(c.w-w) < 0.5 {
		return c.lines
	}
	lines := text.Wrap(func(s string) float32 { return l.UI.Measure(l.Font, s).Width }, l.Text, max(1, w))
	l.lastLines = &labelLines{text: l.Text, fontKey: l.Font.Key, epoch: l.UI.MeasureEpoch(), w: w, lines: lines}
	return lines
}

// lineMetrics: per-font constants; the advance adds 25% leading between lines.
func (l *Label) lineMetrics() (lineH, advance float32) {
	m := l.UI.Measure(l.Font, "Mg")
	lineH = m.Ascent + m.Descent
	return lineH, lineH + float32(int(lineH*0.25+0.5))
}

func (l *Label) HeightForWidth(w float32) (float32, bool) {
	if !l.Wrap {
		return 0, false
	}
	n := max(1, len(l.wrappedLines(w)))
	lineH, adv := l.lineMetrics()
	return ceil32(lineH + float32(n-1)*adv), true
}

// xInLine is the boundary x at a byte offset within one line's display text.
func (l *Label) xInLine(lineText string, bo int) float32 {
	run := l.UI.Measure(l.Font, lineText)
	ri := utf8.RuneCountInString(lineText[:min(bo, len(lineText))])
	xs := runeBoundaryXs(run, utf8.RuneCountInString(lineText))
	return xs[clampInt(ri, 0, len(xs)-1)]
}

// byteAtPoint is the nearest source-offset boundary to a point (wrapped).
func (l *Label) byteAtPoint(x, y float32) int {
	lines := l.wrappedLines(l.Bounds.W)
	if len(lines) == 0 {
		return 0
	}
	_, adv := l.lineMetrics()
	li := clampInt(int((y-l.Bounds.Y)/adv), 0, len(lines)-1)
	ln := lines[li]
	lineText := l.Text[ln.Start:ln.End]
	run := l.UI.Measure(l.Font, lineText)
	xs := runeBoundaryXs(run, utf8.RuneCountInString(lineText))
	best, bestD := 0, float32(math.Inf(1))
	for i, bx := range xs {
		if d := abs32((x - l.Bounds.X) - bx); d < bestD {
			bestD, best = d, i
		}
	}
	// rune index within the line -> byte offset in the source
	bo := ln.Start
	for i := 0; i < best; i++ {
		_, sz := utf8.DecodeRuneInString(l.Text[bo:])
		bo += sz
	}
	return bo
}

func (l *Label) wordRangeAtByte(bo int) (int, int) {
	if len(l.Text) == 0 {
		return 0, 0
	}
	bo = clampInt(bo, 0, len(l.Text)-1)
	// Snap into the rune that covers bo.
	for bo > 0 && !utf8.RuneStart(l.Text[bo]) {
		bo--
	}
	r, _ := utf8.DecodeRuneInString(l.Text[bo:])
	target := isWordRune(r)
	s, e := bo, bo
	for s > 0 {
		pr, sz := utf8.DecodeLastRuneInString(l.Text[:s])
		if isWordRune(pr) != target {
			break
		}
		s -= sz
	}
	for e < len(l.Text) {
		nr, sz := utf8.DecodeRuneInString(l.Text[e:])
		if isWordRune(nr) != target {
			break
		}
		e += sz
	}
	if e == bo { // include at least the rune under the point
		_, sz := utf8.DecodeRuneInString(l.Text[bo:])
		e = bo + sz
	}
	return s, e
}

func (l *Label) paintWrapped(dl *gfx.DisplayList) {
	lines := l.wrappedLines(l.Bounds.W)
	lineH, adv := l.lineMetrics()
	sel := l.UI.selectionFor(l)
	var s, e int
	if sel != nil {
		s, e = min(sel.Start, sel.End), max(sel.Start, sel.End)
	}
	for i, ln := range lines {
		y := l.Bounds.Y + float32(i)*adv
		t := l.Text[ln.Start:ln.End]
		if sel != nil && e > s {
			a := max(s, ln.Start)
			b := min(e, ln.End)
			if b > a || (ln.Start == ln.End && s <= ln.Start && e > ln.End) {
				x0 := l.xInLine(t, max(0, a-ln.Start))
				x1 := x0 + 4 // stub marks empty lines inside the range
				if b > a {
					x1 = l.xInLine(t, b-ln.Start)
				}
				dl.Fill(gfx.R(l.Bounds.X+x0-1, y-1, x1-x0+2, lineH+2),
					gfx.WithAlpha(*Theme["accent"], 0.35), gfx.CornerRadius(2))
			}
		}
		dl.Text(t, l.Bounds.X, y, l.Font, *l.Color)
	}
}

func (l *Label) Interactive() bool { return l.Selectable }

func (l *Label) Cursor() string {
	if l.Selectable {
		return "text"
	}
	return ""
}

func (l *Label) PaintSelf(dl *gfx.DisplayList) {
	if l.Wrap {
		l.paintWrapped(dl)
		return
	}
	if sel := l.UI.selectionFor(l); sel != nil {
		run := l.run()
		s := min(sel.Start, sel.End)
		e := max(sel.Start, sel.End)
		if e > s {
			x0 := l.boundaryX(s)
			x1 := l.boundaryX(e)
			dl.Fill(gfx.R(l.Bounds.X+x0-1, l.Bounds.Y-1, x1-x0+2, run.Ascent+run.Descent+2),
				gfx.WithAlpha(*Theme["accent"], 0.35), gfx.CornerRadius(2))
		}
	}
	dl.Text(l.displayText(), l.Bounds.X, l.Bounds.Y, l.Font, *l.Color)
}

// boundaryX is the x offset of the boundary before glyph i (or the run end).
func (l *Label) boundaryX(i int) float32 {
	run := l.run()
	if i < len(run.Glyphs) {
		return run.Glyphs[i].X
	}
	return run.Width
}

// BoundaryAt is the nearest glyph boundary to an absolute x coordinate.
func (l *Label) BoundaryAt(absX float32) int {
	local := absX - l.Bounds.X
	n := len(l.run().Glyphs)
	best, bestD := 0, float32(math.Inf(1))
	for i := 0; i <= n; i++ {
		if d := abs32(local - l.boundaryX(i)); d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

// GlyphText maps a glyph-boundary range back to the underlying characters via
// shaping clusters. Wrapped labels use source byte offsets directly.
func (l *Label) GlyphText(start, end int) string {
	s := min(start, end)
	e := max(start, end)
	if l.Wrap {
		return l.Text[clampInt(s, 0, len(l.Text)):clampInt(e, 0, len(l.Text))]
	}
	run := l.run()
	if s >= e || len(run.Glyphs) == 0 {
		return ""
	}
	runes := []rune(l.displayText())
	from := run.Glyphs[min(s, len(run.Glyphs)-1)].Cluster
	to := len(runes)
	if e < len(run.Glyphs) {
		to = run.Glyphs[e].Cluster
	}
	if from > to {
		from, to = to, from
	}
	return string(runes[from:to])
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
}

func (l *Label) wordRangeAt(boundary int) (int, int) {
	run := l.run()
	n := len(run.Glyphs)
	if n == 0 {
		return 0, 0
	}
	runes := []rune(l.displayText())
	at := func(i int) rune {
		c := run.Glyphs[i].Cluster
		if c < len(runes) {
			return runes[c]
		}
		return ' '
	}
	i := min(boundary, n-1)
	target := isWordRune(at(i))
	s, e := i, i+1
	for s > 0 && isWordRune(at(s-1)) == target {
		s--
	}
	for e < n && isWordRune(at(e)) == target {
		e++
	}
	return s, e
}

func (l *Label) OnPointerDown(x, y float32, detail int) {
	if !l.Selectable || l.UI == nil {
		return
	}
	if l.Wrap {
		switch {
		case detail >= 3:
			l.UI.setSelection(l, 0, len(l.Text))
		case detail == 2:
			s, e := l.wordRangeAtByte(l.byteAtPoint(x, y))
			l.UI.setSelection(l, s, e)
		default:
			i := l.byteAtPoint(x, y)
			l.UI.setSelection(l, i, i)
		}
		return
	}
	switch {
	case detail >= 3:
		l.UI.setSelection(l, 0, len(l.run().Glyphs))
	case detail == 2:
		s, e := l.wordRangeAt(l.BoundaryAt(x))
		l.UI.setSelection(l, s, e)
	default:
		i := l.BoundaryAt(x)
		l.UI.setSelection(l, i, i)
	}
}

func (l *Label) OnPointerDrag(x, y float32) {
	if !l.Selectable || l.UI == nil {
		return
	}
	if l.Wrap {
		l.UI.moveSelectionEnd(l, l.byteAtPoint(x, y))
		return
	}
	l.UI.moveSelectionEnd(l, l.BoundaryAt(x))
}

// -- button --------------------------------------------------------------------------

var btnFont = gfx.NewFont(13, gfx.FontOpts{Weight: 600})

type Button struct {
	Core
	Label   string
	Primary bool
	// Radius overrides the radius.control metric for this button (nil = the
	// token): circular chrome controls in a custom titlebar, pill CTAs.
	Radius  *float32
	OnClick func()

	hovered bool
	pressed bool
	// Memoized natural size - layout asks every frame (see labelSize).
	lastSize  Size
	lastLabel string
	lastEpoch int
	sized     bool
}

func NewButton(label string) *Button { b := &Button{Label: label}; b.self = b; return b }

// radiusOr resolves a per-widget radius override against its metric default.
func radiusOr(over *float32, def float32) float32 {
	if over != nil {
		return *over
	}
	return def
}

func (b *Button) IntrinsicSize() Size {
	// Keyed by MeasureEpoch, which a metrics change bumps, so the memo can never
	// outlive the token table it was computed from.
	if b.sized && b.lastLabel == b.Label && b.lastEpoch == b.UI.MeasureEpoch() {
		return b.lastSize
	}
	b.lastSize = Size{ceil32(b.UI.Measure(btnFont, b.Label).Width) + Metrics.ControlPadX*2, Metrics.ControlHeight}
	b.lastLabel, b.lastEpoch, b.sized = b.Label, b.UI.MeasureEpoch(), true
	return b.lastSize
}

func (b *Button) Interactive() bool { return true }
func (b *Button) Focusable() bool   { return true }
func (b *Button) Cursor() string    { return "pointer" }

func (b *Button) OnKey(k Key) bool {
	if k.Name == " " || k.Name == "Enter" {
		b.Activate()
		return true
	}
	return false
}

func (b *Button) Activate() {
	if b.OnClick != nil {
		b.OnClick()
	}
}

func (b *Button) PaintSelf(dl *gfx.DisplayList) {
	r := b.Bounds
	rad := radiusOr(b.Radius, Metrics.RadiusControl)
	if b.UI.ShowFocusRing(b) {
		PaintFocusRing(dl, r, rad)
	}
	var bg gfx.Color
	var border *gfx.Color
	if b.Primary {
		accent := *Theme["accent"]
		switch {
		case b.pressed:
			bg = gfx.Mix(accent, gfx.Black, 0.18)
		case b.hovered:
			bg = gfx.Mix(accent, gfx.White, 0.1)
		default:
			bg = accent
		}
		dl.Shadow(r, gfx.WithAlpha(accent, 0.35), 10, gfx.CornerRadius(rad), 0, 3)
	} else {
		control := *Theme["control"]
		switch {
		case b.pressed:
			bg = gfx.Mix(control, gfx.Black, 0.2)
		case b.hovered:
			bg = gfx.Mix(control, gfx.White, 0.06)
		default:
			bg = control
		}
		border = Theme["controlEdge"]
	}
	var bw float32
	if border != nil {
		bw = Metrics.BorderWidth
	}
	dl.Rect(r, bg, gfx.RectOpts{Radius: gfx.CornerRadius(rad), BorderWidth: bw, BorderColor: border})
	m := b.UI.Measure(btnFont, b.Label)
	ink := *Theme["ink"]
	if b.Primary {
		ink = gfx.White
	}
	dl.Text(b.Label, r.X+(r.W-m.Width)/2, r.Y+(r.H-(m.Ascent+m.Descent))/2, btnFont, ink)
}

func (b *Button) OnPointerDown(_, _ float32, _ int) {
	b.pressed = true
	b.Invalidate()
}

func (b *Button) OnPointerUp(x, y float32) {
	wasInside := Contains(b.Bounds, x, y)
	b.pressed = false
	b.Invalidate()
	if wasInside && b.OnClick != nil {
		b.OnClick()
	}
}

func (b *Button) OnHoverChange(h bool) {
	b.hovered = h
	b.Invalidate()
}

// PaintFocusRing draws the keyboard-focus adorner around a rect. radius is the
// widget's own corner radius, uninflated: pass Metrics.RadiusControl for
// standard controls, or half the size for circular things. The ring offsets
// itself outward by its stroke width plus a 1px gap and rounds to match.
func PaintFocusRing(dl *gfx.DisplayList, around gfx.Rect, radius float32) {
	w := Metrics.FocusRing
	off := w + 1
	ring := gfx.WithAlpha(*Theme["accent"], 0.8)
	dl.Rect(gfx.Inflate(around, off), gfx.Transparent, gfx.RectOpts{
		Radius: gfx.CornerRadius(radius + off), BorderWidth: w, BorderColor: &ring,
	})
}

// -- checkbox ------------------------------------------------------------------------

var (
	cbFont = gfx.NewFont(13, gfx.FontOpts{})
)

type Checkbox struct {
	Core
	Label    string
	Checked  bool
	OnToggle func(checked bool)

	hovered bool
}

func NewCheckbox(label string) *Checkbox { c := &Checkbox{Label: label}; c.self = c; return c }

func (c *Checkbox) IntrinsicSize() Size {
	m := c.UI.Measure(cbFont, c.Label)
	box := Metrics.CheckboxSize
	return Size{box + Metrics.SpaceGap + ceil32(m.Width), max(box, ceil32(m.Ascent+m.Descent))}
}

func (c *Checkbox) Interactive() bool { return true }
func (c *Checkbox) Focusable() bool   { return true }
func (c *Checkbox) Cursor() string    { return "pointer" }

func (c *Checkbox) OnKey(k Key) bool {
	if k.Name == " " {
		c.Activate()
		return true
	}
	return false
}

func (c *Checkbox) Activate() {
	c.Checked = !c.Checked
	if c.OnToggle != nil {
		c.OnToggle(c.Checked)
	}
	c.Invalidate()
}

func (c *Checkbox) PaintSelf(dl *gfx.DisplayList) {
	b := c.Bounds
	cb := Metrics.CheckboxSize
	boxY := b.Y + (b.H-cb)/2
	box := gfx.R(b.X, boxY, cb, cb)
	if c.UI.ShowFocusRing(c) {
		PaintFocusRing(dl, box, Metrics.CheckboxRadius())
	}
	if c.Checked {
		fill := *Theme["accent"]
		if c.hovered {
			fill = gfx.Mix(fill, gfx.White, 0.1)
		}
		dl.Fill(box, fill, gfx.CornerRadius(Metrics.CheckboxRadius()))
		PaintCheck(dl, b.X+cb/2, boxY+cb/2, gfx.White)
	} else {
		edge := *Theme["controlEdge"]
		if c.hovered {
			edge = gfx.Mix(edge, gfx.White, 0.25)
		}
		dl.Rect(box, *Theme["panelInset"], gfx.RectOpts{
			Radius: gfx.CornerRadius(Metrics.CheckboxRadius()), BorderWidth: Metrics.BorderWidth * 1.5, BorderColor: &edge,
		})
	}
	m := c.UI.Measure(cbFont, c.Label)
	ink := Theme["inkDim"]
	if c.Checked {
		ink = Theme["ink"]
	}
	dl.Text(c.Label, b.X+cb+Metrics.SpaceGap, b.Y+(b.H-(m.Ascent+m.Descent))/2, cbFont, *ink)
}

func (c *Checkbox) OnPointerUp(x, y float32) {
	if Contains(c.Bounds, x, y) {
		c.Activate()
	}
}

func (c *Checkbox) OnHoverChange(h bool) {
	c.hovered = h
	c.Invalidate()
}

// -- small helpers -------------------------------------------------------------------

func ceil32(v float32) float32 { return float32(math.Ceil(float64(v))) }
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// PaintCheck draws the checkmark as geometry, stepped diagonal strokes, like
// the select chevron and the tree disclosure. Chrome never depends on a font
// shipping a glyph, and both terminals agree to the pixel.
func PaintCheck(dl *gfx.DisplayList, cx, cy float32, c gfx.Color) {
	cx = float32(math.Floor(float64(cx)))
	cy = float32(math.Floor(float64(cy)))
	for i := 0; i < 8; i++ {
		y := cy - 1 + float32(i)
		if i > 2 {
			y = cy + 3 - float32(i)
		}
		dl.Fill(gfx.R(cx-4+float32(i), y, 1, 3), c, gfx.Corners{})
	}
}

package ui

import (
	"unicode/utf8"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/text"
)

type Span struct {
	Text               string
	Bold, Italic, Mono bool
	// "" takes the widget's family
	Font string
	// nil takes the widget's Color
	Color *gfx.Color
	Bg    *gfx.Color
	// non-empty makes it clickable
	Action string
}

type RichText struct {
	Core
	// every span's face derives from it, so mono never changes the line pitch
	Font        gfx.Font
	Color       *gfx.Color
	Selectable  bool
	OnSpanClick func(action string)

	spans []Span
	// spans' text concatenated, selection boundaries and fragments index it
	text string
	// spans[i] covers text[offs[i]:offs[i+1]]
	offs []int
	// bumped by SetSpans, keys the memos below
	rev int

	lastSize   *richSize
	lastLayout *richLayout
	pressSpan  int
	hoverSpan  int
}

type richSize struct {
	rev, epoch int
	fontKey    string
	size       Size
}

// text of one span on one wrapped line, positioned from the line origin
type fragment struct {
	span       int
	start, end int
	x, w       float32
}

type richLine struct {
	text.Line
	frags []fragment
}

// paint, hit-testing, selection and clicks all read this
type richLayout struct {
	rev, epoch int
	fontKey    string
	w          float32
	lines      []richLine
	fonts      []gfx.Font
	// y offset that puts a span's baseline on the base font's
	shift []float32
}

func NewRichText(f gfx.Font, color *gfx.Color) *RichText {
	if color == nil {
		color = Theme["ink"]
	}
	r := &RichText{Font: f, Color: color, pressSpan: -1, hoverSpan: -1}
	r.self = r
	return r
}

func (r *RichText) SetSpans(spans []Span) {
	r.spans = spans
	r.offs = make([]int, len(spans)+1)
	r.text = ""
	for i, s := range spans {
		r.offs[i] = len(r.text)
		r.text += s.Text
	}
	r.offs[len(spans)] = len(r.text)
	r.rev++
	r.hoverSpan, r.pressSpan = -1, -1
	r.Invalidate()
}

func (r *RichText) Text() string { return r.text }

func (r *RichText) spanFont(s Span) gfx.Font {
	w := r.Font.Weight
	if s.Bold {
		w = 700
	}
	family := s.Font
	if family == "" {
		family = r.Font.Family
	}
	return gfx.NewFont(r.Font.Size, gfx.FontOpts{Weight: w, Italic: s.Italic, Mono: s.Mono, Family: family})
}

// width of text[a:b] with each span's own face
func (r *RichText) measureRange(fonts []gfx.Font, a, b int) float32 {
	var w float32
	for i := range r.spans {
		lo, hi := max(a, r.offs[i]), min(b, r.offs[i+1])
		if hi > lo {
			w += r.UI.Measure(fonts[i], r.text[lo:hi]).Width
		}
	}
	return w
}

func (r *RichText) fragments(fonts []gfx.Font, ln text.Line) []fragment {
	var out []fragment
	var x float32
	for i := range r.spans {
		lo, hi := max(ln.Start, r.offs[i]), min(ln.End, r.offs[i+1])
		if hi > lo {
			w := r.UI.Measure(fonts[i], r.text[lo:hi]).Width
			out = append(out, fragment{span: i, start: lo, end: hi, x: x, w: w})
			x += w
		}
	}
	return out
}

func (r *RichText) layout(w float32) *richLayout {
	if c := r.lastLayout; c != nil && c.rev == r.rev && c.fontKey == r.Font.Key &&
		c.epoch == r.UI.MeasureEpoch() && abs32(c.w-w) < 0.5 {
		return c
	}
	l := &richLayout{rev: r.rev, epoch: r.UI.MeasureEpoch(), fontKey: r.Font.Key, w: w}
	base := r.UI.Measure(r.Font, "Mg").Ascent
	for _, s := range r.spans {
		f := r.spanFont(s)
		l.fonts = append(l.fonts, f)
		l.shift = append(l.shift, base-r.UI.Measure(f, "Mg").Ascent)
	}
	measure := func(a, b int) float32 { return r.measureRange(l.fonts, a, b) }
	for _, ln := range text.WrapRanges(measure, r.text, max(1, w)) {
		l.lines = append(l.lines, richLine{Line: ln, frags: r.fragments(l.fonts, ln)})
	}
	r.lastLayout = l
	return l
}

func (r *RichText) IntrinsicSize() Size {
	if c := r.lastSize; c != nil && c.rev == r.rev && c.fontKey == r.Font.Key && c.epoch == r.UI.MeasureEpoch() {
		return c.size
	}
	fonts := make([]gfx.Font, len(r.spans))
	for i, s := range r.spans {
		fonts[i] = r.spanFont(s)
	}
	lineH, _ := lineMetrics(r.UI, r.Font)
	sz := Size{ceil32(r.measureRange(fonts, 0, len(r.text))), ceil32(lineH)}
	r.lastSize = &richSize{rev: r.rev, epoch: r.UI.MeasureEpoch(), fontKey: r.Font.Key, size: sz}
	return sz
}

func (r *RichText) HeightForWidth(w float32) (float32, bool) {
	return wrappedHeight(r.UI, r.Font, len(r.layout(w).lines)), true
}

// every rune boundary on a line: x from the line origin and the byte offset
// it maps to, ascending
func (r *RichText) lineBoundaries(l *richLayout, ln richLine) (xs []float32, bos []int) {
	for _, f := range ln.frags {
		s := r.text[f.start:f.end]
		n := utf8.RuneCountInString(s)
		rx := runeBoundaryXs(r.UI.Measure(l.fonts[f.span], s), n)
		bo := f.start
		for i := 0; i < n; i++ {
			xs = append(xs, f.x+rx[i])
			bos = append(bos, bo)
			_, sz := utf8.DecodeRuneInString(r.text[bo:])
			bo += sz
		}
	}
	var endX float32
	if n := len(ln.frags); n > 0 {
		endX = ln.frags[n-1].x + ln.frags[n-1].w
	}
	return append(xs, endX), append(bos, ln.End)
}

func (r *RichText) lineAt(l *richLayout, y float32) int {
	if len(l.lines) == 0 {
		return -1
	}
	_, adv := lineMetrics(r.UI, r.Font)
	return clampInt(int((y-r.Bounds.Y)/adv), 0, len(l.lines)-1)
}

// nearest boundary to a point, as a byte offset into the text
func (r *RichText) byteAtPoint(x, y float32) int {
	l := r.layout(r.Bounds.W)
	li := r.lineAt(l, y)
	if li < 0 {
		return 0
	}
	xs, bos := r.lineBoundaries(l, l.lines[li])
	return bos[nearestBoundary(xs, x-r.Bounds.X)]
}

// x of a byte offset on a line, from the line origin. an offset inside a
// rune snaps to the boundary before it
func (r *RichText) xAtByte(l *richLayout, ln richLine, bo int) float32 {
	xs, bos := r.lineBoundaries(l, ln)
	x := xs[0]
	for i, b := range bos {
		if b > bo {
			break
		}
		x = xs[i]
	}
	return x
}

// span under a point, -1 between fragments or outside every line box
func (r *RichText) spanAt(x, y float32) int {
	l := r.layout(r.Bounds.W)
	li := r.lineAt(l, y)
	if li < 0 {
		return -1
	}
	lineH, adv := lineMetrics(r.UI, r.Font)
	top := r.Bounds.Y + float32(li)*adv
	if y < top || y >= top+lineH {
		return -1
	}
	lx := x - r.Bounds.X
	for _, f := range l.lines[li].frags {
		if lx >= f.x && lx < f.x+f.w {
			return f.span
		}
	}
	return -1
}

func (r *RichText) actionSpanAt(x, y float32) int {
	if i := r.spanAt(x, y); i >= 0 && r.spans[i].Action != "" {
		return i
	}
	return -1
}

func (r *RichText) hasActions() bool {
	for _, s := range r.spans {
		if s.Action != "" {
			return true
		}
	}
	return false
}

func (r *RichText) Interactive() bool { return r.Selectable || r.hasActions() }

func (r *RichText) Cursor() string {
	switch {
	case r.hoverSpan >= 0:
		return "pointer"
	case r.Selectable:
		return "text"
	}
	return ""
}

func (r *RichText) Semantics() *Semantic {
	if r.text == "" {
		return nil
	}
	return &Semantic{Role: "text", Label: r.text}
}

const spanBgPad = 3

func (r *RichText) PaintSelf(dl *gfx.DisplayList) {
	l := r.layout(r.Bounds.W)
	lineH, adv := lineMetrics(r.UI, r.Font)
	sel := r.UI.selectionFor(r)
	var s, e int
	if sel != nil {
		s, e = min(sel.Start, sel.End), max(sel.Start, sel.End)
	}
	radius := gfx.CornerRadius(Metrics.RadiusControl / 2)
	for i, ln := range l.lines {
		y := r.Bounds.Y + float32(i)*adv
		for _, f := range ln.frags {
			if bg := r.spans[f.span].Bg; bg != nil {
				dl.Fill(gfx.R(r.Bounds.X+f.x-spanBgPad, y, f.w+2*spanBgPad, lineH), *bg, radius)
			}
		}
		if sel != nil && e > s {
			paintSelectionBand(dl, s, e, ln.Line, func(bo int) float32 { return r.xAtByte(l, ln, bo) }, r.Bounds.X, y, lineH)
		}
		for _, f := range ln.frags {
			sp := r.spans[f.span]
			color := r.Color
			if sp.Color != nil {
				color = sp.Color
			}
			dl.Text(r.text[f.start:f.end], r.Bounds.X+f.x, y+l.shift[f.span], l.fonts[f.span], *color)
		}
	}
}

// selection boundaries are byte offsets, so the copy is the exact source slice
func (r *RichText) GlyphText(start, end int) string {
	s := clampInt(min(start, end), 0, len(r.text))
	e := clampInt(max(start, end), 0, len(r.text))
	return r.text[s:e]
}

func (r *RichText) OnPointerHover(x, y float32) { r.hoverSpan = r.actionSpanAt(x, y) }

func (r *RichText) OnHoverChange(h bool) {
	if !h {
		r.hoverSpan = -1
	}
}

func (r *RichText) OnPointerDown(x, y float32, detail int) {
	if r.UI == nil {
		return
	}
	r.pressSpan = r.actionSpanAt(x, y)
	if r.Selectable {
		pressSelection(r.UI, r, r.text, r.byteAtPoint(x, y), detail)
	}
}

func (r *RichText) OnPointerDrag(x, y float32) {
	if r.Selectable && r.UI != nil {
		r.UI.moveSelectionEnd(r, r.byteAtPoint(x, y))
	}
}

// a release on the pressed span with nothing selected is a click, a drag
// that selected text is not
func (r *RichText) OnPointerUp(x, y float32) {
	pressed := r.pressSpan
	r.pressSpan = -1
	if pressed < 0 || r.UI == nil || r.spanAt(x, y) != pressed {
		return
	}
	if sel := r.UI.selectionFor(r); sel != nil && sel.Start != sel.End {
		return
	}
	r.sound("press")
	if r.OnSpanClick != nil {
		r.OnSpanClick(r.spans[pressed].Action)
	}
}

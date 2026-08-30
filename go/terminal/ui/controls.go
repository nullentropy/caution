package ui

import (
	"github.com/nullentropy/caution/go/terminal/gfx"
)

// Progress, slider, radio group and tabs. Local interaction here, semantic
// events upstream, all styling from theme tokens. Ports of src/ui/controls.ts.

var ctrlFont = gfx.NewFont(13, gfx.FontOpts{})

// -- progress -------------------------------------------------------------------

// Progress is a determinate progress bar; Value is 0..1. Display-only.
type Progress struct {
	Core
	Value float32
}

func NewProgress() *Progress { p := &Progress{}; p.self = p; return p }

func (p *Progress) IntrinsicSize() Size { return Size{220, 6} }

func (p *Progress) PaintSelf(dl *gfx.DisplayList) {
	b := p.Bounds
	r := gfx.CornerRadius(b.H / 2)
	dl.Rect(b, *Theme["panelInset"], gfx.RectOpts{Radius: r, BorderWidth: Metrics.BorderWidth, BorderColor: Theme["edgeSoft"]})
	v := min(1, max(0, p.Value))
	if v > 0 {
		dl.Fill(gfx.R(b.X, b.Y, max(b.H, b.W*v), b.H), *Theme["accent"], r)
	}
}

// -- slider ---------------------------------------------------------------------

// Slider is a horizontal slider over [Min, Max], optionally stepped.
// Dragging is client-authoritative (server value sets are ignored mid-drag);
// moves emit input (debounced upstream), release or a key press emits commit.
type Slider struct {
	Core
	Min, Max, Step, Value float32
	OnInput               func(v float32)
	OnCommit              func(v float32)

	Dragging bool
	hovered  bool
}

func NewSlider() *Slider { s := &Slider{Max: 1}; s.self = s; return s }

func (s *Slider) IntrinsicSize() Size {
	return Size{220, max(24, Metrics.SliderThumb*2+4)}
}
func (s *Slider) Interactive() bool { return true }
func (s *Slider) Focusable() bool   { return true }
func (s *Slider) Cursor() string    { return "pointer" }

func (s *Slider) span() float32 {
	if d := s.Max - s.Min; d != 0 {
		return d
	}
	return 1
}

func (s *Slider) ratio() float32 { return min(1, max(0, (s.Value-s.Min)/s.span())) }

func (s *Slider) valueAt(x float32) float32 {
	b := s.Bounds
	thumbR := Metrics.SliderThumb
	usable := max(1, b.W-thumbR*2)
	v := s.Min + min(1, max(0, (x-b.X-thumbR)/usable))*s.span()
	if s.Step > 0 {
		v = s.Min + float32(int((v-s.Min)/s.Step+0.5))*s.Step
	}
	return min(s.Max, max(s.Min, v))
}

func (s *Slider) setValue(v float32, commit bool) {
	if v != s.Value {
		s.Value = v
		s.Invalidate()
		if s.OnInput != nil {
			s.OnInput(v)
		}
	}
	if commit && s.OnCommit != nil {
		s.OnCommit(s.Value)
	}
}

func (s *Slider) OnPointerDown(x, _ float32, _ int) {
	s.Dragging = true
	s.setValue(s.valueAt(x), false)
}

func (s *Slider) OnPointerDrag(x, _ float32) {
	if s.Dragging {
		s.setValue(s.valueAt(x), false)
	}
}

func (s *Slider) OnPointerUp(_, _ float32) {
	if s.Dragging {
		s.Dragging = false
		s.setValue(s.Value, true)
	}
}

func (s *Slider) OnKey(k Key) bool {
	nudge := s.Step
	if nudge <= 0 {
		nudge = s.span() / 100
	}
	var v float32
	switch k.Name {
	case "Left", "Down":
		v = s.Value - nudge
	case "Right", "Up":
		v = s.Value + nudge
	case "Home":
		v = s.Min
	case "End":
		v = s.Max
	default:
		return false
	}
	s.setValue(min(s.Max, max(s.Min, v)), true)
	return true
}

func (s *Slider) OnHoverChange(h bool) {
	s.hovered = h
	s.Invalidate()
}

func (s *Slider) PaintSelf(dl *gfx.DisplayList) {
	b := s.Bounds
	thumbR, trackH := Metrics.SliderThumb, Metrics.SliderTrack
	cy := b.Y + b.H/2
	track := gfx.R(b.X+thumbR, cy-trackH/2, b.W-thumbR*2, trackH)
	dl.Rect(track, *Theme["panelInset"], gfx.RectOpts{
		Radius: gfx.CornerRadius(trackH / 2), BorderWidth: Metrics.BorderWidth, BorderColor: Theme["edgeSoft"],
	})
	tx := track.X + track.W*s.ratio()
	if tx > track.X {
		dl.Fill(gfx.R(track.X, track.Y, tx-track.X, track.H), *Theme["accent"], gfx.CornerRadius(trackH/2))
	}
	thumb := gfx.R(tx-thumbR, cy-thumbR, thumbR*2, thumbR*2)
	if s.UI.ShowFocusRing(s) {
		PaintFocusRing(dl, thumb, thumbR)
	}
	dl.Shadow(thumb, gfx.WithAlpha(gfx.Black, 0.4), 6, gfx.CornerRadius(thumbR), 0, 2)
	fill := *Theme["accent"]
	if s.Dragging {
		fill = gfx.Mix(fill, gfx.Black, 0.15)
	} else if s.hovered {
		fill = gfx.Mix(fill, gfx.White, 0.12)
	}
	white := gfx.White
	dl.Rect(thumb, fill, gfx.RectOpts{Radius: gfx.CornerRadius(thumbR), BorderWidth: Metrics.BorderWidth * 2, BorderColor: &white})
}

// -- radio group ------------------------------------------------------------------

// radioD is the radio circle's diameter: checkbox.size - 2, because a circle
// reads optically larger than a square of the same box.
func radioD() float32 { return Metrics.CheckboxSize - 2 }

// RadioGroup is a vertical radio list; picking emits select with the index.
type RadioGroup struct {
	Core
	Options  []string
	Selected int
	OnSelect func(i int)
	hoverRow int
}

func NewRadioGroup() *RadioGroup {
	r := &RadioGroup{Selected: -1, hoverRow: -1}
	r.self = r
	return r
}

func (r *RadioGroup) IntrinsicSize() Size {
	var w float32
	for _, o := range r.Options {
		w = max(w, ceil32(r.UI.Measure(ctrlFont, o).Width))
	}
	return Size{radioD() + Metrics.SpaceGap + w, float32(len(r.Options)) * Metrics.RowHeight}
}

func (r *RadioGroup) Interactive() bool { return true }
func (r *RadioGroup) Focusable() bool   { return true }
func (r *RadioGroup) Cursor() string    { return "pointer" }

func (r *RadioGroup) pick(i int) {
	if i < 0 || i >= len(r.Options) || i == r.Selected {
		return
	}
	r.Selected = i // local echo
	r.Invalidate()
	if r.OnSelect != nil {
		r.OnSelect(i)
	}
}

func (r *RadioGroup) OnKey(k Key) bool {
	switch k.Name {
	case "Down", "Right":
		r.pick(min(len(r.Options)-1, r.Selected+1))
		return true
	case "Up", "Left":
		r.pick(max(0, r.Selected-1))
		return true
	}
	return false
}

func (r *RadioGroup) OnPointerUp(x, y float32) {
	if Contains(r.Bounds, x, y) {
		r.pick(r.rowAt(y))
	}
}

func (r *RadioGroup) OnPointerHover(_, y float32) {
	if i := r.rowAt(y); i != r.hoverRow {
		r.hoverRow = i
		r.Invalidate()
	}
}

func (r *RadioGroup) OnHoverChange(h bool) {
	if !h && r.hoverRow != -1 {
		r.hoverRow = -1
		r.Invalidate()
	}
}

func (r *RadioGroup) rowAt(y float32) int {
	i := int((y - r.Bounds.Y) / Metrics.RowHeight)
	if i >= 0 && i < len(r.Options) {
		return i
	}
	return -1
}

func (r *RadioGroup) PaintSelf(dl *gfx.DisplayList) {
	b := r.Bounds
	d, rowH := radioD(), Metrics.RowHeight
	// The selected dot keeps a 5px ring of accent around it, floored so it
	// never vanishes under a small checkbox.size.
	dot := max(4, d-10)
	for i, opt := range r.Options {
		y := b.Y + float32(i)*rowH
		cy := y + rowH/2
		circle := gfx.R(b.X, cy-d/2, d, d)
		if i == r.Selected && r.UI.ShowFocusRing(r) {
			PaintFocusRing(dl, circle, d/2)
		}
		if i == r.Selected {
			dl.Fill(circle, *Theme["accent"], gfx.CornerRadius(d/2))
			dl.Fill(gfx.R(circle.X+(d-dot)/2, circle.Y+(d-dot)/2, dot, dot),
				gfx.White, gfx.CornerRadius(dot/2))
		} else {
			edge := *Theme["controlEdge"]
			if i == r.hoverRow {
				edge = gfx.Mix(edge, gfx.White, 0.25)
			}
			dl.Rect(circle, *Theme["panelInset"], gfx.RectOpts{
				Radius: gfx.CornerRadius(d / 2), BorderWidth: Metrics.BorderWidth * 1.5, BorderColor: &edge,
			})
		}
		m := r.UI.Measure(ctrlFont, opt)
		ink := Theme["inkDim"]
		if i == r.Selected {
			ink = Theme["ink"]
		}
		dl.Text(opt, b.X+d+Metrics.SpaceGap, cy-(m.Ascent+m.Descent)/2, ctrlFont, *ink)
	}
}

// -- tabs ---------------------------------------------------------------------------

var tabFont = gfx.NewFont(13, gfx.FontOpts{Weight: 600})

// tabPad is a tab's horizontal text padding; tabsH is control.height plus the
// 2px selection underline the bar reserves below the text.
func tabPad() float32 { return Metrics.ControlPadX }
func tabsH() float32  { return Metrics.ControlHeight + 2 }

// Tabs is a tab bar; picking emits select. Content switching is app policy.
type Tabs struct {
	Core
	Options  []string
	Selected int
	OnSelect func(i int)
	hoverTab int
}

func NewTabs() *Tabs { t := &Tabs{hoverTab: -1}; t.self = t; return t }

func (t *Tabs) IntrinsicSize() Size {
	var w float32
	for _, o := range t.Options {
		w += ceil32(t.UI.Measure(tabFont, o).Width) + tabPad()*2
	}
	return Size{w, tabsH()}
}

func (t *Tabs) Interactive() bool { return true }
func (t *Tabs) Focusable() bool   { return true }
func (t *Tabs) Cursor() string    { return "pointer" }

func (t *Tabs) pick(i int) {
	if i < 0 || i >= len(t.Options) || i == t.Selected {
		return
	}
	t.Selected = i // local echo
	t.Invalidate()
	if t.OnSelect != nil {
		t.OnSelect(i)
	}
}

func (t *Tabs) OnKey(k Key) bool {
	switch k.Name {
	case "Right":
		t.pick(min(len(t.Options)-1, t.Selected+1))
		return true
	case "Left":
		t.pick(max(0, t.Selected-1))
		return true
	}
	return false
}

func (t *Tabs) OnPointerUp(x, y float32) {
	if Contains(t.Bounds, x, y) {
		t.pick(t.tabAt(x))
	}
}

func (t *Tabs) OnPointerHover(x, _ float32) {
	if i := t.tabAt(x); i != t.hoverTab {
		t.hoverTab = i
		t.Invalidate()
	}
}

func (t *Tabs) OnHoverChange(h bool) {
	if !h && t.hoverTab != -1 {
		t.hoverTab = -1
		t.Invalidate()
	}
}

func (t *Tabs) tabAt(x float32) int {
	tx := t.Bounds.X
	for i, o := range t.Options {
		w := ceil32(t.UI.Measure(tabFont, o).Width) + tabPad()*2
		if x >= tx && x < tx+w {
			return i
		}
		tx += w
	}
	return -1
}

func (t *Tabs) PaintSelf(dl *gfx.DisplayList) {
	b := t.Bounds
	hair := Metrics.BorderWidth
	dl.Fill(gfx.R(b.X, b.Y+b.H-hair, b.W, hair), *Theme["edgeSoft"], gfx.Corners{})
	tx := b.X
	for i, opt := range t.Options {
		m := t.UI.Measure(tabFont, opt)
		w := ceil32(m.Width) + tabPad()*2
		sel := i == t.Selected
		if sel && t.UI.ShowFocusRing(t) {
			PaintFocusRing(dl, gfx.R(tx+4, b.Y+4, w-8, b.H-8), Metrics.RadiusControl)
		}
		ink := Theme["inkDim"]
		if sel || i == t.hoverTab {
			ink = Theme["ink"]
		}
		dl.Text(opt, tx+tabPad(), b.Y+(b.H-(m.Ascent+m.Descent))/2-1, tabFont, *ink)
		if sel {
			dl.Fill(gfx.R(tx+tabPad()-4, b.Y+b.H-2, w-tabPad()*2+8, 2), *Theme["accent"], gfx.CornerRadius(1))
		}
		tx += w
	}
}

package ui

import (
	"math"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

// ShaderPane is a pane painted entirely by an app-supplied fragment shader
// (ShaderToy style): the GLSL defines `vec4 effect(vec2 uv)` and may use
// u_time, u_res, and custom float uniforms. Animate keeps the surface
// repainting while visible. Decorative - no semantics, no interaction.
type ShaderPane struct {
	Core
	Frag     string
	Uniforms map[string]float32
	Animate  bool
}

func NewShaderPane() *ShaderPane { s := &ShaderPane{}; s.self = s; return s }

func (s *ShaderPane) PaintSelf(dl *gfx.DisplayList) {
	if s.Frag != "" {
		dl.ShaderQuad(s.Bounds, s.Frag, s.Uniforms, s.Animate)
	}
}

// ImageView is a bitmap image. Src is a URL or data: URI; the texture loads
// asynchronously (placeholder until ready). Fit follows the native image
// view modes. Radius rounds the corners in the shader, and Alt is what screen
// readers hear (unused natively until a semantics bridge exists).
type ImageView struct {
	Core
	Src    string
	Fit    string // contain | cover | fill
	Radius gfx.Corners
	Alt    string
}

func NewImageView() *ImageView { v := &ImageView{Fit: "contain"}; v.self = v; return v }

func (v *ImageView) PaintSelf(dl *gfx.DisplayList) {
	if v.Src != "" {
		dl.Image(v.Bounds, v.Src, v.Fit, v.Radius)
	}
}

// Select is the dropdown. The closed control is server-driven state; the
// open list is a client-local popover on the Ui overlay layer - opening,
// hovering, and closing never touch the network. Picking an option is
// local-echoed and emits `select`.
type Select struct {
	Core
	Options  []string
	Selected int
	OnSelect func(i int)

	hovered bool
}

var selectFont = gfx.NewFont(13, gfx.FontOpts{})

// selChevronW is the closed control's pull-down zone (clip inset + mark).
const (
	selPad      = 6
	selChevronW = 24
)

func NewSelect() *Select { s := &Select{Selected: -1}; s.self = s; return s }

func (s *Select) value() string {
	if s.Selected >= 0 && s.Selected < len(s.Options) {
		return s.Options[s.Selected]
	}
	return ""
}

func (s *Select) IntrinsicSize() Size {
	var w float32
	for _, o := range s.Options {
		w = max(w, s.UI.Measure(selectFont, o).Width)
	}
	return Size{ceil32(w) + Metrics.SpacePad*2 + selChevronW, Metrics.ControlHeight}
}

func (s *Select) Interactive() bool { return true }
func (s *Select) Focusable() bool   { return true }
func (s *Select) Cursor() string    { return "pointer" }

func (s *Select) PaintSelf(dl *gfx.DisplayList) {
	b := s.Bounds
	if s.UI.ShowFocusRing(s) {
		PaintFocusRing(dl, b, Metrics.RadiusControl)
	}
	bg := *Theme["control"]
	if s.hovered {
		bg = gfx.Mix(bg, gfx.White, 0.06)
	}
	dl.Rect(b, bg, gfx.RectOpts{Radius: gfx.CornerRadius(Metrics.RadiusControl), BorderWidth: Metrics.BorderWidth, BorderColor: Theme["controlEdge"]})
	m := s.UI.Measure(selectFont, s.value())
	dl.PushClip(gfx.R(b.X, b.Y, b.W-selChevronW, b.H))
	dl.Text(s.value(), b.X+Metrics.SpacePad, b.Y+(b.H-(m.Ascent+m.Descent))/2, selectFont, *Theme["ink"])
	dl.PopClip()
	paintChevron(dl, b)
}

// paintChevron draws the select's pull-down mark as geometry, not text: the
// Go fonts lack U+25BE and system fonts each size it differently, so a glyph
// here can never converge across terminals. Five shrinking bars make the same
// stepped triangle everywhere.
func paintChevron(dl *gfx.DisplayList, b gfx.Rect) {
	cx := float32(math.Floor(float64(b.X + b.W - 15)))
	cy := float32(math.Floor(float64(b.Y + b.H/2)))
	for i := 0; i < 5; i++ {
		w := float32(9 - 2*i)
		dl.Fill(gfx.R(cx-w/2, cy-3+float32(i), w, 1), *Theme["inkDim"], gfx.Corners{})
	}
}

func (s *Select) OnPointerDown(_, _ float32, _ int) { s.toggle() }

func (s *Select) OnKey(k Key) bool {
	if k.Name == " " || k.Name == "Enter" {
		s.toggle()
		return true
	}
	return false
}

func (s *Select) Activate() { s.toggle() }

func (s *Select) OnHoverChange(h bool) {
	s.hovered = h
	s.Invalidate()
}

// Pick local-echoes the choice and reports it; the server syncs silently.
func (s *Select) Pick(i int) {
	if i != s.Selected {
		s.Selected = i
		s.sound("select")
		if s.OnSelect != nil {
			s.OnSelect(i)
		}
	}
	s.UI.dismissOverlay()
	s.Invalidate()
}

func (s *Select) toggle() {
	if s.UI == nil || len(s.Options) == 0 {
		return
	}
	if pop, ok := s.UI.Overlay.(*selectPopup); ok && pop.owner == s {
		s.UI.CloseOverlay()
		return
	}
	pop := &selectPopup{owner: s, hoverRow: -1}
	pop.self = pop
	w := s.Bounds.W
	for _, o := range s.Options {
		w = max(w, s.UI.Measure(selectFont, o).Width+40)
	}
	h := float32(len(s.Options))*Metrics.RowHeight + selPad*2
	y := s.Bounds.Y + s.Bounds.H + 4
	if y+h > s.UI.ViewH()-8 {
		y = max(8, s.Bounds.Y-4-h)
	}
	pop.Bounds = gfx.R(s.Bounds.X, y, w, h)
	s.UI.OpenOverlay(pop)
	s.sound("open")
}

// selectPopup is the client-local option list; it lives on the Ui overlay
// layer, above everything.
type selectPopup struct {
	Core
	owner    *Select
	hoverRow int
}

func (p *selectPopup) Interactive() bool { return true }
func (p *selectPopup) Cursor() string    { return "pointer" }

func (p *selectPopup) PaintSelf(dl *gfx.DisplayList) {
	b := p.Bounds
	dl.Shadow(b, gfx.WithAlpha(gfx.Black, 0.45), 24, gfx.CornerRadius(Metrics.RadiusPopover), 0, 10)
	dl.Rect(b, *Theme["panel"], gfx.RectOpts{Radius: gfx.CornerRadius(Metrics.RadiusPopover), BorderWidth: Metrics.BorderWidth, BorderColor: Theme["edge"]})
	for i, opt := range p.owner.Options {
		y := b.Y + selPad + float32(i)*Metrics.RowHeight
		if i == p.hoverRow {
			dl.Fill(gfx.R(b.X+4, y, b.W-8, Metrics.RowHeight), gfx.WithAlpha(*Theme["accent"], 0.25), gfx.CornerRadius(Metrics.RadiusControl))
		}
		m := p.UI.Measure(selectFont, opt)
		dl.Text(opt, b.X+26, y+(Metrics.RowHeight-(m.Ascent+m.Descent))/2, selectFont, *Theme["ink"])
		if i == p.owner.Selected {
			PaintCheck(dl, b.X+14, y+Metrics.RowHeight/2, *Theme["accent"])
		}
	}
}

func (p *selectPopup) OnPointerDown(_, y float32, _ int) {
	if i := p.rowAt(y); i >= 0 {
		p.owner.Pick(i)
	}
}

func (p *selectPopup) OnPointerHover(_, y float32) {
	if i := p.rowAt(y); i != p.hoverRow {
		p.hoverRow = i
		p.Invalidate()
	}
}

func (p *selectPopup) rowAt(y float32) int {
	i := int((y - p.Bounds.Y - selPad) / Metrics.RowHeight)
	if i >= 0 && i < len(p.owner.Options) {
		return i
	}
	return -1
}

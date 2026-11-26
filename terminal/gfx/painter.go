package gfx

import "math"

type CmdKind uint8

const (
	CmdRect CmdKind = iota
	CmdShadow
	CmdText
	CmdLayerBegin
	CmdLayerEnd
	CmdShaderQuad
	CmdImage
)

type Fit string

const (
	FitFill    Fit = "fill"
	FitContain Fit = "contain"
	FitCover   Fit = "cover"
)

type Cmd struct {
	Kind  CmdKind
	Rect  Rect
	Radii Corners
	Clip  Rect

	Animated bool

	Color       Color
	Border      Color
	BorderWidth float32
	Blur        float32

	X, Y float32
	Text string
	Font Font

	Src string
	Fit Fit

	Frag     string
	Uniforms map[string]float32
	UV       [4]float32
}

type DisplayList struct {
	Cmds          []Cmd
	GeomAnimation bool
	Measure       func(f Font, s string) (w, ascent, descent float32)

	clips  []Rect
	layers []Rect
}

func (dl *DisplayList) clip() Rect {
	if n := len(dl.clips); n > 0 {
		return dl.clips[n-1]
	}
	return NoClip
}

func (dl *DisplayList) WantsAnimation() bool {
	if dl.GeomAnimation {
		return true
	}
	for i := range dl.Cmds {
		if dl.Cmds[i].Animated {
			return true
		}
	}
	return false
}

func (dl *DisplayList) Clip() Rect { return dl.clip() }

type RectOpts struct {
	Radius      Corners
	BorderWidth float32
	BorderColor *Color
}

func (dl *DisplayList) Rect(r Rect, color Color, opts RectOpts) {
	border := color
	if opts.BorderColor != nil {
		border = *opts.BorderColor
	}
	dl.Cmds = append(dl.Cmds, Cmd{
		Kind: CmdRect, Rect: r, Radii: opts.Radius, Clip: dl.clip(),
		Color: color, Border: border, BorderWidth: opts.BorderWidth,
	})
}

func (dl *DisplayList) Fill(r Rect, color Color, radius Corners) {
	dl.Rect(r, color, RectOpts{Radius: radius})
}

func (dl *DisplayList) Shadow(r Rect, color Color, blur float32, radius Corners, dx, dy float32) {
	dl.Cmds = append(dl.Cmds, Cmd{
		Kind: CmdShadow, Rect: Rect{r.X + dx, r.Y + dy, r.W, r.H}, Radii: radius,
		Clip: dl.clip(), Color: color, Blur: blur,
	})
}

func (dl *DisplayList) Text(s string, x, y float32, f Font, color Color) {
	var box Rect
	if dl.Measure != nil {
		w, ascent, descent := dl.Measure(f, s)
		box = Rect{x, y, w, ascent + descent}
	}
	dl.Cmds = append(dl.Cmds, Cmd{
		Kind: CmdText, Rect: box, X: x, Y: y, Text: s, Font: f, Color: color, Clip: dl.clip(),
	})
}

func (dl *DisplayList) Image(r Rect, src string, fit Fit, radius Corners) {
	dl.Cmds = append(dl.Cmds, Cmd{
		Kind: CmdImage, Rect: r, Src: src, Fit: fit, Radii: radius, Clip: dl.clip(),
	})
}

func (dl *DisplayList) PushClip(r Rect) {
	dl.clips = append(dl.clips, Intersect(dl.clip(), r))
}

func (dl *DisplayList) PopClip() {
	if n := len(dl.clips); n > 0 {
		dl.clips = dl.clips[:n-1]
	}
}

func (dl *DisplayList) BeginLayer(r Rect) {
	x := float32(math.Floor(float64(r.X)))
	y := float32(math.Floor(float64(r.Y)))
	snapped := Rect{
		x, y,
		float32(math.Ceil(float64(r.X+r.W))) - x,
		float32(math.Ceil(float64(r.Y+r.H))) - y,
	}
	dl.layers = append(dl.layers, snapped)
	dl.Cmds = append(dl.Cmds, Cmd{Kind: CmdLayerBegin, Rect: snapped})
}

func (dl *DisplayList) EndLayer(frag string, uniforms map[string]float32, animate bool) {
	n := len(dl.layers)
	if n == 0 {
		return
	}
	r := dl.layers[n-1]
	dl.layers = dl.layers[:n-1]
	dl.Cmds = append(dl.Cmds, Cmd{Kind: CmdLayerEnd, Rect: r, Frag: frag, Uniforms: uniforms, Animated: animate})
}

func (dl *DisplayList) ShaderQuad(r Rect, frag string, uniforms map[string]float32, animate bool) {
	c := Intersect(dl.clip(), r)
	if c.W <= 0 || c.H <= 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	dl.Cmds = append(dl.Cmds, Cmd{
		Kind: CmdShaderQuad, Rect: c, Frag: frag, Uniforms: uniforms, Animated: animate,
		UV: [4]float32{(c.X - r.X) / r.W, (c.Y - r.Y) / r.H, (c.X + c.W - r.X) / r.W, (c.Y + c.H - r.Y) / r.H},
	})
}

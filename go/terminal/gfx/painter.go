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

// Cmd is one draw command
type Cmd struct {
	Kind  CmdKind
	Rect  Rect
	Radii Corners
	Clip  Rect

	// Animated marks a ShaderQuad or LayerEnd that asked for continuous
	// u_time animation
	Animated bool

	// rect / shadow
	Color       Color
	Border      Color
	BorderWidth float32
	Blur        float32

	// text: X, Y is the top-left of the line box; the renderer derives the
	// baseline from font metrics. Rect is the measured line box when the
	// list was built with a Measure hook - a text command's painted extent,
	// which is what lets a partial frame skip the text it does not touch. A
	// zero-size Rect means "unmeasured": the extent falls back to the clip.
	X, Y float32
	Text string
	Font Font

	// image
	Src string
	Fit string // contain | cover | fill

	// custom shader passes
	Frag     string
	Uniforms map[string]float32
	UV       [4]float32 // uv sub-range so ambient clipping doesn't distort shader space
}

// DisplayList is a retained draw-command list in logical pixels, painter's-
// algorithm ordered. Clips are resolved (intersected) at record time so the
// renderer never replays a stack.
type DisplayList struct {
	Cmds []Cmd
	// GeomAnimation is true while geometric animations (slides/fades) are
	// mid-flight this frame. Those move widget bounds every frame, so a
	// retained list must not be replayed - every frame rebuilds until they
	// settle (set by the Ui's animGeometry pass).
	GeomAnimation bool
	// Measure returns a string's advance width and vertical metrics, so Text
	// can record the line box it will paint into. Without it text commands
	// carry no rect and partial frames fall back to replaying them whenever
	// their clip is in range - correct, just less selective. Ui.BuildFrame
	// wires it to the shell's shaper.
	Measure func(f Font, s string) (w, ascent, descent float32)

	clips  []Rect
	layers []Rect
}

func (dl *DisplayList) clip() Rect {
	if n := len(dl.clips); n > 0 {
		return dl.clips[n-1]
	}
	return NoClip
}

// WantsAnimation reports whether this frame asks for another one: any command
// wanting continuous u_time, or a geometric tween mid-flight.
// Derived from the commands rather than accumulated while recording them. An
// accumulator only sees the commands a frame actually built, and a spliced
// subtree copies its retained commands without calling ShaderQuad or EndLayer,
// so it would lose exactly the animation reuse is best at preserving: an
// animated pane is byte-identical frame to frame, which makes it the first
// thing reuse splices.
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

// Clip is the ambient clip commands recorded right now will carry. A retained
// subtree's commands are only reusable under the clip they were recorded with,
// so the paint layer compares against this.
func (dl *DisplayList) Clip() Rect { return dl.clip() }

type RectOpts struct {
	Radius      Corners
	BorderWidth float32
	BorderColor *Color // nil = same as fill
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

// Fill is the common no-border case.
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

func (dl *DisplayList) Image(r Rect, src, fit string, radius Corners) {
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

// BeginLayer renders everything until the matching EndLayer into an offscreen
// texture. The rect is snapped to whole logical pixels so glyphs stay crisp.
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

// EndLayer composites the layer back through a custom fragment shader.
func (dl *DisplayList) EndLayer(frag string, uniforms map[string]float32, animate bool) {
	n := len(dl.layers)
	if n == 0 {
		return
	}
	r := dl.layers[n-1]
	dl.layers = dl.layers[:n-1]
	dl.Cmds = append(dl.Cmds, Cmd{Kind: CmdLayerEnd, Rect: r, Frag: frag, Uniforms: uniforms, Animated: animate})
}

// ShaderQuad paints a quad entirely with a custom fragment shader.
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

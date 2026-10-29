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

// which fields matter depends on Kind
type Cmd struct {
	Kind  CmdKind
	Rect  Rect
	Radii Corners
	Clip  Rect

	// ShaderQuad or LayerEnd wants continuous u_time
	Animated bool

	// rect / shadow
	Color       Color
	Border      Color
	BorderWidth float32
	Blur        float32

	// text: X, Y is the top-left of the line box, baseline comes from font
	// metrics. Rect is the measured line box when built with Measure, which
	// lets a partial frame skip text it doesn't touch. zero-size Rect means
	// unmeasured, the extent falls back to the clip
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

// logical pixels, painter's order. clips are intersected at record time so
// the renderer never replays a stack
type DisplayList struct {
	Cmds []Cmd
	// slides/fades mid-flight move widget bounds every frame, so the retained
	// list can't be replayed until they settle. set by the UI's animGeometry pass
	GeomAnimation bool
	// lets Text record its line box. without it text has no rect and partial
	// frames replay it whenever its clip is in range, correct but less selective
	// UI.BuildFrame wires it to the shell's shaper
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

// any command wanting continuous u_time, or a geometric tween mid-flight
// derived from the commands, not accumulated while recording. a spliced
// subtree copies retained commands without calling ShaderQuad or EndLayer,
// and an animated pane is byte-identical frame to frame so it's the first
// thing reuse splices
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

// ambient clip for commands recorded now. a retained subtree is only
// reusable under the clip it was recorded with
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

// no border
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

// renders until the matching EndLayer into an offscreen texture
// rect snaps to whole logical pixels so glyphs stay crisp
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

// composites through a custom fragment shader
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

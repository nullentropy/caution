package gfx

import "strconv"

// Color is straight (non-premultiplied) RGBA, all channels 0..1.
// Premultiplication happens in the shader.
type Color struct {
	R, G, B, A float32
}

func RGBA(r, g, b, a float32) Color { return Color{r, g, b, a} }

// Hex parses '#rgb', '#rrggbb', or '#rrggbbaa'.
func Hex(s string) Color {
	h := s
	if len(h) > 0 && h[0] == '#' {
		h = h[1:]
	}
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) < 6 {
		return Color{}
	}
	n, err := strconv.ParseUint(h[:6], 16, 32)
	if err != nil {
		return Color{}
	}
	a := float32(1)
	if len(h) == 8 {
		if an, err := strconv.ParseUint(h[6:8], 16, 32); err == nil {
			a = float32(an) / 255
		}
	}
	return Color{
		float32((n>>16)&0xff) / 255,
		float32((n>>8)&0xff) / 255,
		float32(n&0xff) / 255,
		a,
	}
}

func WithAlpha(c Color, a float32) Color { return Color{c.R, c.G, c.B, a} }

// Mix linearly blends a->b by t (0..1), all channels including alpha.
func Mix(a, b Color, t float32) Color {
	return Color{
		a.R + (b.R-a.R)*t,
		a.G + (b.G-a.G)*t,
		a.B + (b.B-a.B)*t,
		a.A + (b.A-a.A)*t,
	}
}

var (
	White       = Color{1, 1, 1, 1}
	Black       = Color{0, 0, 0, 1}
	Transparent = Color{0, 0, 0, 0}
)

package main

import (
	"github.com/nullentropy/caution/go/terminal/gfx"
)

func scene(w, h, time float32) *gfx.DisplayList {
	dl := &gfx.DisplayList{}

	ink := gfx.Hex("#e8eaf0")
	inkDim := gfx.Hex("#9aa3b2")
	inkFaint := gfx.Hex("#6b7280")
	panel := gfx.Hex("#262b33")
	panelAlt := gfx.Hex("#20252c")
	titlebar := gfx.Hex("#2e343e")
	edge := gfx.Hex("#3a4150")
	accent := gfx.Hex("#4f8cff")

	title := gfx.NewFont(22, gfx.FontOpts{Weight: 600})
	body := gfx.NewFont(13, gfx.FontOpts{})
	small := gfx.NewFont(12, gfx.FontOpts{})
	big := gfx.NewFont(28, gfx.FontOpts{Weight: 600})

	dl.Text("caution - native terminal", 32, 22, title, ink)
	dl.Text("the same display list, drawn by OpenGL instead of WebGL - no browser in this process",
		32, 54, body, inkDim)

	// A demo-style window: shadow, bordered panel, titlebar, traffic lights.
	win := gfx.R(32, 92, 430, 300)
	dl.Shadow(win, gfx.Hex("#00000080"), 32, gfx.CornerRadius(12), 0, 14)
	dl.Rect(win, panel, gfx.RectOpts{
		Radius: gfx.CornerRadius(12), BorderWidth: 1, BorderColor: &edge,
	})
	dl.Fill(gfx.R(win.X, win.Y, win.W, 40), titlebar,
		gfx.Corners{TL: 12, TR: 12})
	dl.Fill(gfx.R(win.X+16, win.Y+14, 12, 12), gfx.Hex("#ff5f57"), gfx.CornerRadius(6))
	dl.Fill(gfx.R(win.X+36, win.Y+14, 12, 12), gfx.Hex("#febc2e"), gfx.CornerRadius(6))
	dl.Fill(gfx.R(win.X+56, win.Y+14, 12, 12), gfx.Hex("#28c840"), gfx.CornerRadius(6))
	dl.Text("Server Counter", win.X+win.W/2-46, win.Y+12, gfx.NewFont(13, gfx.FontOpts{Weight: 600}), inkDim)

	dl.Text("3", win.X+24, win.Y+60, big, ink)
	dl.Text("this number would live on the server - the terminal only paints",
		win.X+24, win.Y+104, body, inkDim)
	dl.Text("kerning check: AV To Ta We watch - a patch comes back.",
		win.X+24, win.Y+130, body, ink)
	dl.Text("hamburgefonstiv 0123456789 - the quick brown fox",
		win.X+24, win.Y+152, body, inkDim)

	// A "button": accent fill, rounded, label.
	btn := gfx.R(win.X+24, win.Y+win.H-56, 96, 32)
	dl.Fill(btn, accent, gfx.CornerRadius(8))
	dl.Text("Primary", btn.X+22, btn.Y+8, gfx.NewFont(13, gfx.FontOpts{Weight: 600}), gfx.White)
	btn2 := gfx.R(btn.X+108, btn.Y, 96, 32)
	dl.Rect(btn2, titlebar, gfx.RectOpts{Radius: gfx.CornerRadius(8), BorderWidth: 1, BorderColor: &edge})
	dl.Text("Quiet", btn2.X+30, btn2.Y+8, body, ink)

	// Clipping: a panel whose text overflows and must cut off at the edge
	// (with the ~1px AA the clip shader gives).
	clipPanel := gfx.R(494, 92, 220, 120)
	dl.Fill(clipPanel, panelAlt, gfx.CornerRadius(12))
	dl.PushClip(gfx.Inset(clipPanel, 1))
	dl.Text("this line is far too long to fit inside this panel and must clip cleanly at the boundary",
		clipPanel.X+16, clipPanel.Y+14, body, ink)
	dl.Text("clipped: yes", clipPanel.X+16, clipPanel.Y+40, small, inkFaint)
	dl.Fill(gfx.R(clipPanel.X+140, clipPanel.Y+70, 200, 80), accent, gfx.CornerRadius(10))
	dl.PopClip()

	// Effect layer: content rendered offscreen, composited back through
	// user GLSL (scanlines) - the FBO round trip.
	fxPanel := gfx.R(494, 232, 220, 160)
	dl.BeginLayer(fxPanel)
	dl.Fill(fxPanel, gfx.Hex("#101913"), gfx.CornerRadius(12))
	dl.Text("EFFECT LAYER", fxPanel.X+16, fxPanel.Y+16, gfx.NewFont(14, gfx.FontOpts{Weight: 600, Mono: true}), gfx.Hex("#6fe08a"))
	dl.Text("rendered to a texture,", fxPanel.X+16, fxPanel.Y+44, gfx.NewFont(12, gfx.FontOpts{Mono: true}), gfx.Hex("#4fae66"))
	dl.Text("composited via GLSL", fxPanel.X+16, fxPanel.Y+62, gfx.NewFont(12, gfx.FontOpts{Mono: true}), gfx.Hex("#4fae66"))
	dl.Fill(gfx.R(fxPanel.X+16, fxPanel.Y+96, 120, 30), gfx.Hex("#1e3325"), gfx.CornerRadius(6))
	dl.EndLayer(`
vec4 effect(vec2 uv) {
  vec4 c = src(uv);
  float scan = 0.82 + 0.18 * sin(uv.y * u_res.y * 3.14159);
  c.rgb *= scan;
  return c;
}`, nil, false)

	// Shader quad: a surface painted entirely by app GLSL.
	sq := gfx.R(742, 92, 186, 300)
	dl.Fill(gfx.Inflate(sq, 8), panelAlt, gfx.CornerRadius(12))
	dl.ShaderQuad(sq, `
vec4 effect(vec2 uv) {
  vec3 col = 0.5 + 0.5 * cos(u_time + uv.xyx * 3.0 + vec3(0.0, 2.0, 4.0));
  return vec4(col * 0.8, 1.0);
}`, nil, true)

	// Shadow-only row: blur ramp.
	for i := 0; i < 4; i++ {
		x := 32 + float32(i)*110
		r := gfx.R(x, 452, 80, 56)
		dl.Shadow(r, gfx.Hex("#000000a0"), float32(4+i*8), gfx.CornerRadius(10), 0, 6)
		dl.Fill(r, panel, gfx.CornerRadius(10))
		dl.Text("blur", r.X+26, r.Y+18, small, inkFaint)
	}

	// Radius / border sampler.
	rb := gfx.R(494, 452, 80, 56)
	dl.Rect(rb, panelAlt, gfx.RectOpts{Radius: gfx.Corners{TL: 24, BR: 24}, BorderWidth: 2, BorderColor: &accent})
	rb2 := gfx.R(590, 452, 80, 56)
	dl.Rect(rb2, gfx.Transparent, gfx.RectOpts{Radius: gfx.CornerRadius(28), BorderWidth: 1, BorderColor: &edge})

	dl.Text("weights: ", 32, 540, body, inkDim)
	dl.Text("regular 400", 96, 540, gfx.NewFont(13, gfx.FontOpts{}), ink)
	dl.Text("medium 500", 186, 540, gfx.NewFont(13, gfx.FontOpts{Weight: 500}), ink)
	dl.Text("bold 700", 280, 540, gfx.NewFont(13, gfx.FontOpts{Weight: 700}), ink)
	dl.Text("italic", 350, 540, gfx.NewFont(13, gfx.FontOpts{Italic: true}), ink)
	dl.Text("mono 13", 400, 540, gfx.NewFont(13, gfx.FontOpts{Mono: true}), ink)
	dl.Text("sizes: 11 / 13 / 17 / 22", 32, 566, gfx.NewFont(11, gfx.FontOpts{}), inkFaint)
	dl.Text("the quick brown fox jumps over the lazy dog", 180, 562, gfx.NewFont(17, gfx.FontOpts{}), ink)

	return dl
}

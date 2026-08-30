package main

import (
	"fmt"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/glx"
	"github.com/nullentropy/caution/go/terminal/render"
	"github.com/nullentropy/caution/go/terminal/text"
	"github.com/nullentropy/caution/go/terminal/ui"
)

const (
	W, H = 400, 300
	dpr  = 2
)

func runNative() error {
	if err := glfw.Init(); err != nil {
		return err
	}
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Visible, glfw.False)
	win, err := glfw.CreateWindow(W, H, "loopprobe", nil, nil)
	if err != nil {
		return err
	}
	win.MakeContextCurrent()
	if err := glx.Init(); err != nil {
		return err
	}

	devW, devH := int32(W*dpr), int32(H*dpr)
	var tex, fbo uint32
	glx.GenTextures(1, &tex)
	glx.ActiveTexture(glx.TEXTURE0)
	glx.BindTexture(glx.TEXTURE_2D, tex)
	glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, devW, devH, 0, glx.RGBA, glx.UNSIGNED_BYTE, nil)
	glx.GenFramebuffers(1, &fbo)
	glx.BindFramebuffer(glx.FRAMEBUFFER, fbo)
	glx.FramebufferTexture2D(glx.FRAMEBUFFER, glx.COLOR_ATTACHMENT0, glx.TEXTURE_2D, tex, 0)
	sample := func(lx, ly float32) byte { // red channel at a logical point
		px, py := int32(lx*dpr), int32(ly*dpr)
		pix := make([]byte, 4)
		glx.BindFramebuffer(glx.FRAMEBUFFER, fbo)
		glx.ReadPixels(px, devH-1-py, 1, 1, glx.RGBA, glx.UNSIGNED_BYTE, glx.Ptr(pix))
		return pix[0]
	}

	r, err := render.New()
	if err != nil {
		return err
	}
	u := ui.New()
	u.Measure = func(f gfx.Font, s string) *text.Run { return r.Shaper.Shape(f, dpr, s) }

	root := ui.NewPanel()
	root.Bg = ui.Theme["bg"]
	pane := ui.NewShaderPane()
	pane.Frame = &gfx.Rect{X: 20, Y: 20, W: 160, H: 120}
	pane.Frag = "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }"
	pane.Animate = true
	root.Add(pane)
	u.Root = root

	start := time.Now()
	frame := func() render.Frame {
		return render.Frame{
			ViewW: W, ViewH: H, DevW: devW, DevH: devH,
			FBO: fbo, Time: float32(time.Since(start).Seconds()),
		}
	}

	dirty := true
	animating := false
	fullDraw := func() {
		dl := &gfx.DisplayList{}
		u.BuildFrame(dl, W, H)
		animating = dl.WantsAnimation()
		r.Render(dl, *ui.Tok("bg"), frame())
	}
	step := func() {
		paint := animating && !r.AnimationIsInvisible()
		if dirty {
			dirty = false
			fullDraw()
		} else if paint {
			if !r.RenderPartial(*ui.Tok("bg"), frame()) {
				fullDraw()
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Settle: the shader alone must ride the continuation path.
	for i := 0; i < 12; i++ {
		step()
	}
	if r.Frames.Partial < 8 {
		return fmt.Errorf("settle: expected continuation frames, got %+v", r.Frames)
	}

	// The structural insert, both scene shapes, then NO further ops: the
	// loop alone must finish the fades.
	red := gfx.Hex("#ff2030")
	plain := ui.NewPanel()
	plain.Frame = &gfx.Rect{X: 280, Y: 200, W: 80, H: 60}
	plain.Bg = &red
	root.Add(plain)
	gib := ui.NewPanel()
	gib.Frame = &gfx.Rect{X: 200, Y: 40, W: 120, H: 100}
	gib.Bg = &red
	gib.Effect = &ui.Effect{
		Frag:    "vec4 effect(vec2 uv) { return src(uv) * (0.9 + 0.1*sin(u_time*20.0 + uv.y*80.0)); }",
		Animate: true,
	}
	inner := ui.NewShaderPane()
	inner.Frame = &gfx.Rect{X: 10, Y: 10, W: 40, H: 30}
	inner.Frag = "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }"
	inner.Animate = true
	gib.Add(inner)
	root.Add(gib)
	u.NoteStructural()
	dirty = true

	for i := 0; i < 90; i++ { // 450ms of wall clock; the fade is 200ms
		step()
	}
	a, b := sample(320, 230), sample(260, 120)
	fmt.Printf("native: plain=%d overlay=%d frames=%+v\n", a, b, r.Frames)
	// Solid #ff2030 red = 255; the overlay's CRT modulates 0.8..1.0 of it.
	// A starved fade holds a blend toward bg (red ~ 22 at alpha 0).
	if a < 240 {
		return fmt.Errorf("plain insert never finished fading (red=%d)", a)
	}
	if b < 180 {
		return fmt.Errorf("effect-layer insert never finished fading (red=%d)", b)
	}
	fmt.Println("native: ok - fades complete under an animated composite")
	return nil
}

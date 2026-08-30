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

// runLeaks drives the renderer through two shapes that can grow resources
// without bound and asserts both stay bounded:
//
//	resize storm - a live-resize drag hands Render hundreds of distinct
//	logical sizes; the layer pool is keyed by pixel size
//
//	shader mint - an app that bakes per-state data into generated frag
//	sources compiles a distinct program per state
func runLeaks() error {
	if err := glfw.Init(); err != nil {
		return err
	}
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Visible, glfw.False)
	win, err := glfw.CreateWindow(640, 480, "loopprobe-leaks", nil, nil)
	if err != nil {
		return err
	}
	win.MakeContextCurrent()
	if err := glx.Init(); err != nil {
		return err
	}
	// One offscreen target big enough for every storm size.
	devW, devH := int32(640*2), int32(480*2)
	var tex, fbo uint32
	glx.GenTextures(1, &tex)
	glx.ActiveTexture(glx.TEXTURE0)
	glx.BindTexture(glx.TEXTURE_2D, tex)
	glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, devW, devH, 0, glx.RGBA, glx.UNSIGNED_BYTE, nil)
	glx.GenFramebuffers(1, &fbo)
	glx.BindFramebuffer(glx.FRAMEBUFFER, fbo)
	glx.FramebufferTexture2D(glx.FRAMEBUFFER, glx.COLOR_ATTACHMENT0, glx.TEXTURE_2D, tex, 0)

	r, err := render.New()
	if err != nil {
		return err
	}
	u := ui.New()
	u.Measure = func(f gfx.Font, s string) *text.Run { return r.Shaper.Shape(f, 2, s) }

	root := ui.NewPanel()
	root.Bg = ui.Theme["bg"]
	// A FULL-BLEED effect layer. The reported shape: its pooled texture is
	// window-sized, so its pool key changes with every resize step. A
	// fixed-size layer would reuse one key forever and never leak.
	fx := ui.NewPanel()
	fx.Frame = &gfx.Rect{X: 0, Y: 0, W: 640, H: 480}
	fx.Bg = ui.Theme["panel"]
	fx.Effect = &ui.Effect{Frag: "vec4 effect(vec2 uv) { return src(uv) * vec4(1.0, 0.95, 0.9, 1.0); }"}
	root.Add(fx)
	pane := ui.NewShaderPane()
	pane.Frame = &gfx.Rect{X: 230, Y: 10, W: 120, H: 90}
	pane.Frag = "vec4 effect(vec2 uv) { return vec4(uv, 0.5, 1.0); }"
	root.Add(pane)
	u.Root = root

	start := time.Now()
	draw := func(w, h float32) {
		fx.Frame.W, fx.Frame.H = w, h // track the window, like a full-bleed pane
		dl := &gfx.DisplayList{}
		u.BuildFrame(dl, w, h)
		r.Render(dl, *ui.Tok("bg"), render.Frame{
			ViewW: w, ViewH: h, DevW: int32(w * 2), DevH: int32(h * 2),
			FBO: fbo, Time: float32(time.Since(start).Seconds()),
		})
	}

	// The resize storm: 150 distinct sizes, like a corner drag.
	for i := 0; i < 150; i++ {
		draw(float32(320+i*2), float32(240+i))
	}
	layers, progs := r.PoolSizes()
	fmt.Printf("after resize storm: pooledLayers=%d userPrograms=%d\n", layers, progs)
	if layers > 8 {
		return fmt.Errorf("layer pool retained %d textures across a resize storm - the per-size leak is back", layers)
	}

	// The shader mint storm: 150 distinct frag sources (and every 10th one
	// broken, so failure markers are exercised too).
	for i := 0; i < 150; i++ {
		frag := fmt.Sprintf("vec4 effect(vec2 uv) { const float k = %d.0; return vec4(uv * (k / 150.0), 0.5, 1.0); }", i)
		if i%10 == 9 {
			frag = fmt.Sprintf("vec4 effect(vec2 uv) { syntax error %d }", i)
		}
		pane.Frag = frag
		pane.Invalidate()
		draw(620, 389)
	}
	layers, progs = r.PoolSizes()
	fmt.Printf("after shader mint:  pooledLayers=%d userPrograms=%d\n", layers, progs)
	if progs > 48 {
		return fmt.Errorf("user-program cache holds %d entries - the cap is not holding", progs)
	}
	fmt.Println("leaks: ok - layer pool and program cache stay bounded")
	return nil
}

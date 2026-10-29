package main

import (
	"fmt"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/glx"
	"github.com/nullentropy/caution/terminal/render"
	"github.com/nullentropy/caution/terminal/text"
	"github.com/nullentropy/caution/terminal/ui"
)

// two shapes that could grow resources without bound:
//
//	resize storm  a live-resize drag hands Render hundreds of logical
//	              sizes, the layer pool is keyed by pixel size
//
//	shader mint   an app baking per-state data into generated frag
//	              sources compiles a program per state
//
//	layer burst   hundreds of layered rows in assorted heights in one
//	              frame, each a pool key of its own that never comes back
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
	// FULL-BLEED effect layer, the reported shape. its pooled texture is
	// window-sized so the pool key changes every resize step. a fixed-size
	// layer reuses one key and never leaks
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

	// resize storm, 150 sizes like a corner drag
	for i := 0; i < 150; i++ {
		draw(float32(320+i*2), float32(240+i))
	}
	layers, _, progs := r.PoolSizes()
	fmt.Printf("after resize storm: pooledLayers=%d userPrograms=%d\n", layers, progs)
	if layers > 8 {
		return fmt.Errorf("layer pool retained %d textures across a resize storm - the per-size leak is back", layers)
	}

	// shader mint, 150 frag sources. every 10th is broken to exercise
	// failure markers
	for i := 0; i < 150; i++ {
		frag := fmt.Sprintf("vec4 effect(vec2 uv) { const float k = %d.0; return vec4(uv * (k / 150.0), 0.5, 1.0); }", i)
		if i%10 == 9 {
			frag = fmt.Sprintf("vec4 effect(vec2 uv) { syntax error %d }", i)
		}
		pane.Frag = frag
		pane.Invalidate()
		draw(620, 389)
	}
	layers, _, progs = r.PoolSizes()
	fmt.Printf("after shader mint:  pooledLayers=%d userPrograms=%d\n", layers, progs)
	if progs > 48 {
		return fmt.Errorf("user-program cache holds %d entries - the cap is not holding", progs)
	}

	// layer burst, 300 rows outside any clip so culling can't hide them,
	// about 200 MB of textures at 2x
	burst := ui.NewPanel()
	burst.Frame = &gfx.Rect{X: 0, Y: 0, W: 600, H: 389}
	y := float32(0)
	for i := 0; i < 300; i++ {
		row := ui.NewPanel()
		row.Frame = &gfx.Rect{X: 0, Y: y, W: 600, H: float32(40 + i)}
		row.Bg = ui.Theme["panel"]
		row.Effect = &ui.Effect{Frag: "vec4 effect(vec2 uv) { return src(uv) * 0.9; }"}
		burst.Add(row)
		y += float32(40 + i)
	}
	root.Add(burst)
	u.Invalidate()
	draw(620, 389)
	root.Kids = root.Kids[:len(root.Kids)-1]
	u.Invalidate()
	draw(620, 389) // the burst's cached layers go back to the pool here
	_, bytes, _ := r.PoolSizes()
	fmt.Printf("after layer burst:  pooledMB=%.1f\n", float64(bytes)/(1<<20))
	if bytes > 64<<20 {
		return fmt.Errorf("layer pool holds %d MB after a burst - the byte cap is not holding", bytes>>20)
	}
	for i := 0; i < 70; i++ {
		u.Invalidate()
		draw(620, 389)
	}
	_, idle, _ := r.PoolSizes()
	fmt.Printf("after idle frames:  pooledMB=%.1f\n", float64(idle)/(1<<20))
	if idle >= bytes {
		return fmt.Errorf("layer pool still holds %d MB after 70 frames without the burst", idle>>20)
	}
	fmt.Println("leaks: ok - layer pool and program cache stay bounded")
	return nil
}

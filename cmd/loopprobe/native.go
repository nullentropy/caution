package main

import (
	"fmt"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/nullentropy/caution/terminal"
	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/render"
	"github.com/nullentropy/caution/terminal/text"
	"github.com/nullentropy/caution/terminal/ui"
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
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	glfw.WindowHint(glfw.Visible, glfw.False)
	win, err := glfw.CreateWindow(W, H, "loopprobe", nil, nil)
	if err != nil {
		return err
	}
	dev, err := terminal.OpenDevice(win)
	if err != nil {
		return err
	}

	devW, devH := int32(W*dpr), int32(H*dpr)
	target, err := dev.NewTexture(int(devW), int(devH), gpu.Nearest)
	if err != nil {
		return err
	}
	sample := func(lx, ly float32) byte {
		px, py := int32(lx*dpr), int32(ly*dpr)
		pix, err := dev.ReadPixels(target)
		if err != nil {
			panic(err)
		}
		return pix[(int(py)*int(devW)+int(px))*4]
	}

	r, err := render.New(dev)
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
			Target: target, Time: float32(time.Since(start).Seconds()),
		}
	}

	dirty := true
	animating := false
	fullDraw := func() {
		dl := &gfx.DisplayList{}
		u.BuildFrame(dl, W, H)
		animating = dl.WantsAnimation()
		r.Render(dl, *ui.Tok("bg"), frame())
		dev.Present()
	}
	step := func() {
		paint := animating && !r.AnimationIsInvisible()
		if dirty {
			dirty = false
			fullDraw()
		} else if paint {
			ok := r.RenderPartial(*ui.Tok("bg"), frame())
			dev.Present()
			if !ok {
				fullDraw()
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	for i := 0; i < 12; i++ {
		step()
	}
	if r.Frames.Partial < 8 {
		return fmt.Errorf("settle: expected continuation frames, got %+v", r.Frames)
	}

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

	for i := 0; i < 90; i++ {
		step()
	}
	a, b := sample(320, 230), sample(260, 120)
	fmt.Printf("native: plain=%d overlay=%d frames=%+v\n", a, b, r.Frames)
	if a < 240 {
		return fmt.Errorf("plain insert never finished fading (red=%d)", a)
	}
	if b < 180 {
		return fmt.Errorf("effect-layer insert never finished fading (red=%d)", b)
	}
	fmt.Println("native: ok - fades complete under an animated composite")
	return nil
}

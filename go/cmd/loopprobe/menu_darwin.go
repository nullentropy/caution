//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Cocoa
void caution_probe_menu(void *nswindow, double seconds);
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/glx"
	"github.com/nullentropy/caution/go/terminal/pacer"
	"github.com/nullentropy/caution/go/terminal/render"
	"github.com/nullentropy/caution/go/terminal/text"
	"github.com/nullentropy/caution/go/terminal/ui"
)

// runMenu proves animation frames keep flowing while an NSMenu tracking
// session parks the main thread in a modal loop that would otherwise freeze every
// animation until the menu closed. Falsification is built in: the same
// tracking window is entered twice, pacer armed then idled, and the probe
// demands frames flow in the first and starve in the second
func runMenu() error {
	if err := glfw.Init(); err != nil {
		return err
	}
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Visible, glfw.False)
	win, err := glfw.CreateWindow(400, 300, "loopprobe-menu", nil, nil)
	if err != nil {
		return err
	}
	win.MakeContextCurrent()
	if err := glx.Init(); err != nil {
		return err
	}
	devW, devH := int32(800), int32(600)
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
	pane := ui.NewShaderPane()
	pane.Frame = &gfx.Rect{X: 20, Y: 20, W: 160, H: 120}
	pane.Frag = "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }"
	pane.Animate = true
	root.Add(pane)
	u.Root = root

	start := time.Now()
	var lastDraw time.Time
	step := func() {
		dl := &gfx.DisplayList{}
		u.BuildFrame(dl, 400, 300)
		r.Render(dl, *ui.Tok("bg"), render.Frame{
			ViewW: 400, ViewH: 300, DevW: devW, DevH: devH,
			FBO: fbo, Time: float32(time.Since(start).Seconds()),
		})
		lastDraw = time.Now()
	}
	pacer.Install(func() {
		if time.Since(lastDraw) < 25*time.Millisecond {
			return
		}
		step()
	})

	// Settle a few frames the ordinary way.
	for i := 0; i < 10; i++ {
		step()
		glfw.PollEvents()
		time.Sleep(8 * time.Millisecond)
	}
	frames := func() int { return r.Frames.Full + r.Frames.Damage + r.Frames.Partial }

	pacer.Arm(1.0 / 60.0)
	before := frames()
	C.caution_probe_menu(unsafe.Pointer(win.GetCocoaWindow()), 0.8) // blocks: tracking
	armed := frames() - before
	fmt.Printf("frames during 0.8s of menu tracking, pacer armed: %d\n", armed)

	pacer.Idle()
	before = frames()
	C.caution_probe_menu(unsafe.Pointer(win.GetCocoaWindow()), 0.8)
	idled := frames() - before
	fmt.Printf("frames during 0.8s of menu tracking, pacer idled: %d\n", idled)

	if idled > 3 {
		return fmt.Errorf("tracking loop did not park the thread (%d frames with pacer idle) - the probe can't falsify", idled)
	}
	if armed < 20 {
		return fmt.Errorf("only %d frames flowed under menu tracking - the modal-loop pacer is not holding", armed)
	}
	fmt.Println("menu: ok - animation frames flow through menu tracking")
	return nil
}

//go:build angle || windows

package terminal

import (
	"fmt"

	"github.com/nullentropy/caution/terminal/egl"
	"github.com/nullentropy/caution/terminal/glx"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// ANGLE dialect: the window has no GL context (NoAPI), ANGLE supplies
// GLES 3 on the native backend attached to the window handle
// shot mode uses an offscreen pbuffer, every real target is our own FBO anyway

func glHints() {
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
}

func glSetup(win *glfw.Window, shot bool, w, h int) error {
	if err := egl.Load(); err != nil {
		return err
	}
	if err := egl.InitDisplay(); err != nil {
		return err
	}
	if shot {
		if err := egl.MakeCurrentPbuffer(w*2, h*2); err != nil {
			return err
		}
	} else {
		sx, _ := win.GetContentScale()
		if err := egl.MakeCurrentWindow(nativeWindowHandle(win), float64(sx)); err != nil {
			return err
		}
	}
	if err := glx.Init(); err != nil {
		return fmt.Errorf("gles (angle): %w", err)
	}
	egl.SwapInterval(1)
	return nil
}

func glSwap(_ *glfw.Window) { egl.SwapBuffers() }

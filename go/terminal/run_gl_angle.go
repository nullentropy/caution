//go:build angle || windows

package terminal

import (
	"fmt"

	"github.com/nullentropy/caution/go/terminal/egl"
	"github.com/nullentropy/caution/go/terminal/glx"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// The GL dialect seam for ANGLE: the window carries no GL context of its own
// (NoAPI) and ANGLE supplies an OpenGL ES 3 context on the platform's native
// backend, attached to the window handle nativeWindowHandle returns, or to
// an offscreen pbuffer in shot mode, where every real target is the
// terminal's own FBO anyway.

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

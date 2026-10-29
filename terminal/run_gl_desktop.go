//go:build !angle && !windows

package terminal

import (
	"fmt"

	"github.com/nullentropy/caution/terminal/glx"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// default GL dialect: desktop OpenGL 4.1 core through GLFW (see terminal/glx)

func glHints() {
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
}

func glSetup(win *glfw.Window, _ bool, _, _ int) error {
	win.MakeContextCurrent()
	glfw.SwapInterval(1)
	if err := glx.Init(); err != nil {
		return fmt.Errorf("gl: %w", err)
	}
	return nil
}

func glSwap(win *glfw.Window) { win.SwapBuffers() }

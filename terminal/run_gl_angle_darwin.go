//go:build angle && darwin

package terminal

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// egl.MakeCurrentWindow asks the NSWindow for a layer
func nativeWindowHandle(win *glfw.Window) unsafe.Pointer {
	return unsafe.Pointer(win.GetCocoaWindow())
}

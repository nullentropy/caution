//go:build angle && darwin

package terminal

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// EGL's native window on macOS is derived from the NSWindow (see
// egl.MakeCurrentWindow, which asks it for a layer).
func nativeWindowHandle(win *glfw.Window) unsafe.Pointer {
	return unsafe.Pointer(win.GetCocoaWindow())
}

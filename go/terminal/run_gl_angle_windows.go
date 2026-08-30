//go:build windows

package terminal

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// EGL's native window on Windows is the HWND itself.
func nativeWindowHandle(win *glfw.Window) unsafe.Pointer {
	return unsafe.Pointer(win.GetWin32Window())
}

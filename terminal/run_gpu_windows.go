package terminal

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/gpu/d3d11"
)

func OpenDevice(win *glfw.Window) (gpu.Device, error) {
	return d3d11.New(unsafe.Pointer(win.GetWin32Window()))
}

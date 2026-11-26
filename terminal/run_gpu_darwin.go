package terminal

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/gpu/metal"
)

func OpenDevice(win *glfw.Window) (gpu.Device, error) {
	return metal.New(unsafe.Pointer(win.GetCocoaWindow()))
}

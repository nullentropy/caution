//go:build linux && !wayland

package terminal

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/gpu/vulkan"
)

func OpenDevice(win *glfw.Window) (gpu.Device, error) {
	return vulkan.NewXlib(unsafe.Pointer(glfw.GetX11Display()), uint64(win.GetX11Window()))
}

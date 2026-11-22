//go:build darwin

package terminal

/*
#cgo LDFLAGS: -framework Cocoa
int caution_watch_occlusion(void *nswindow);
*/
import "C"

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

var onOcclusion func(visible bool)

//export cautionOcclusionChanged
func cautionOcclusionChanged(visible C.int) {
	if onOcclusion != nil {
		onOcclusion(visible != 0)
	}
}

func watchOcclusion(win *glfw.Window, fn func(visible bool)) bool {
	onOcclusion = fn
	return C.caution_watch_occlusion(unsafe.Pointer(win.GetCocoaWindow())) != 0
}

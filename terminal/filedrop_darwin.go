//go:build darwin

package terminal

/*
#cgo LDFLAGS: -framework Cocoa
void caution_watch_file_drag(void *nswindow);
*/
import "C"

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

var onFileDrag func(bool)

//export cautionFileDrag
func cautionFileDrag(over C.int) {
	if onFileDrag != nil {
		onFileDrag(over != 0)
	}
}

// main thread
func watchFileDrag(win *glfw.Window, fn func(over bool)) {
	onFileDrag = fn
	C.caution_watch_file_drag(unsafe.Pointer(win.GetCocoaWindow()))
}

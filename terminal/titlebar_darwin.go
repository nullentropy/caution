//go:build darwin

package terminal

/*
#cgo LDFLAGS: -framework Cocoa
void caution_custom_titlebar(void *nswindow, int style);
void caution_watch_fullscreen(void *nswindow);
*/
import "C"

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// full-bleed content with the traffic lights floating over it
// style picks where they sit
func applyCustomTitlebar(win *glfw.Window, style string) {
	s := 0
	switch style {
	case "compact":
		s = 1
	case "tall":
		s = 2
	}
	C.caution_custom_titlebar(unsafe.Pointer(win.GetCocoaWindow()), C.int(s))
}

var onFullscreen func(bool)

//export cautionFullscreenChanged
func cautionFullscreenChanged(on C.int) {
	if onFullscreen != nil {
		onFullscreen(on != 0)
	}
}

// system fullscreen (green button, Cmd+Ctrl+F), not Options.Fullscreen
// which takes over a monitor at a video mode
func watchFullscreen(win *glfw.Window, fn func(on bool)) {
	onFullscreen = fn
	C.caution_watch_fullscreen(unsafe.Pointer(win.GetCocoaWindow()))
}
